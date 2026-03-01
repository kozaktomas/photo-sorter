# HNSW Similarity Search Architecture

## Overview

The system uses a **dual-layer architecture** for vector similarity search: an in-memory HNSW index (via `coder/hnsw` library) on top of PostgreSQL with pgvector. Both layers use the same HNSW algorithm with matching parameters (`M=16`, `ef_construction=200`, `ef_search=100`).

## Why both layers?

pgvector provides HNSW indexes natively (created in migrations 001/002), and the in-memory layer provides the same O(log N) approximate nearest neighbor search. For single queries the difference is negligible (~1ms vs ~15ms). However, **batch-heavy interactive features** make hundreds to thousands of sequential queries where the latency compounds:

| Feature | HNSW Queries | In-memory (~1ms each) | pgvector (~15ms each) |
|---------|-------------|----------------------|----------------------|
| Duplicate Detection | 1 per photo (e.g., 1000) | ~1s | ~15s |
| Recognition Scan | subjects × faces (e.g., 100×10) | ~2-3s | ~15-20s |
| Album Suggestions | 1 per album (e.g., 50) | <1s | ~1s |
| Similar Collection | 1 per source photo | <1s | ~1s |

The in-memory index provides a ~15x speedup for Duplicate Detection and Recognition Scan — both user-facing features where the difference between 2s and 15s is noticeable.

## Two indexes

| Index | File | Key Type | Dimensions | Data |
|-------|------|----------|-----------|------|
| Face | `hnsw_index.go` | `int64` (DB row ID) | 512 (ResNet100) | Face embeddings |
| Embedding | `hnsw_embeddings.go` | `string` (photo UID) | 768 (CLIP) | Image embeddings |

## Fallback pattern

Both `EmbeddingRepository` and `FaceRepository` in `postgres/` implement the same pattern:

```go
func (r *Repository) FindSimilar(ctx, embedding, limit) {
    if r.hnswEnabled {
        return r.findSimilarHNSW(embedding, limit)   // in-memory, <1ms
    }
    return r.findSimilarPostgres(ctx, embedding, limit)  // pgvector, ~15ms
}
```

The pgvector path sets `SET LOCAL hnsw.ef_search = 100` to match the in-memory index's recall quality.

## Lifecycle

1. **Startup** (`cmd/serve.go`): Tries to load persisted index from disk (`HNSW_INDEX_PATH` / `HNSW_EMBEDDING_INDEX_PATH`). If stale or missing, rebuilds from full table scan.
2. **Runtime**: Incremental updates on insert/delete (face index auto-updates when faces are saved) and on marker metadata changes (subject assignment via `UpdateFaceMarker`).
3. **Shutdown**: Saves to disk for faster next startup.
4. **Rebuild**: Admin endpoint `POST /api/v1/process/rebuild-index` rebuilds from PostgreSQL.

## Persistence files

When `HNSW_INDEX_PATH` or `HNSW_EMBEDDING_INDEX_PATH` is configured, the index is persisted as three files:

- `.graph` — Binary HNSW graph structure
- `.meta` — JSON metadata (count, max ID) for staleness detection
- `.faces` / `.embeddings` — Gob-encoded data for the `idToFace`/`idToEmb` lookup maps

## Configuration

| Env Var | Description |
|---------|-------------|
| `HNSW_INDEX_PATH` | Path to persist face HNSW index (e.g., `/data/faces.pg.hnsw`) |
| `HNSW_EMBEDDING_INDEX_PATH` | Path to persist embedding HNSW index (e.g., `/data/embeddings.pg.hnsw`) |

If not set, indexes are built in-memory at startup and lost on shutdown. The pgvector fallback is always available.

## Key files

```
internal/database/
├── hnsw_index.go          # Face HNSW index (int64 keys, 512-dim)
├── hnsw_embeddings.go     # Embedding HNSW index (string keys, 768-dim)
├── constants.go           # Shared HNSW parameters (M, ef_search, ef_construction)
├── cosine.go              # Cosine distance computation
└── postgres/
    ├── embeddings.go      # FindSimilar with HNSW/pgvector fallback
    ├── faces.go           # FindSimilar with HNSW/pgvector fallback
    └── migrations/
        ├── 001_create_embeddings.sql  # pgvector HNSW index on embeddings
        └── 002_create_faces.sql       # pgvector HNSW index on faces
```
