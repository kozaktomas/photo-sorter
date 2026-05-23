# Similarity Search

All vector similarity search in photo-sorter runs through PostgreSQL +
[pgvector](https://github.com/pgvector/pgvector). There is no in-process
HNSW index, no on-disk index file, no manual rebuild button. A
`pg_dump` of the database is a complete metadata backup.

## Where the embeddings live

| Table | Column | Dimensions | Source |
|-------|--------|-----------:|--------|
| `embeddings` | `embedding` | 768 | CLIP image embeddings |
| `faces` | `embedding` | 512 | InsightFace ResNet100 |
| `era_embeddings` | `embedding` | 768 | CLIP text centroids for each era |

The `embeddings` and `faces` tables each carry an `hnsw` index with
operator class `vector_cosine_ops`, created by migration
`038_pgvector_hnsw_indexes.sql`. The `era_embeddings` table is a small
fixed set and does a plain sequential scan — no index needed.

## Query shape

Every cosine query in the app — both image (`EmbeddingRepository.FindSimilar`
/ `FindSimilarWithDistance`) and face (`FaceRepository.FindSimilar` /
`FindSimilarWithDistance`) — opens a small read-only transaction,
sets `ef_search`, and issues an `ORDER BY embedding <=> $vec` SELECT:

```sql
BEGIN READ ONLY;
SET LOCAL hnsw.ef_search = 100;
SELECT photo_uid, embedding, …
  FROM embeddings
 WHERE embedding <=> $1::vector < $maxDistance
 ORDER BY embedding <=> $1::vector
 LIMIT $limit;
COMMIT;
```

`100` is the hard-coded value of the package-level `hnswEfSearch`
constant in `internal/database/postgres/embeddings.go`; the face
repository imports the same constant. Increasing it trades query
latency for recall. The schema-level build parameters
(`m=16, ef_construction=64`) are the pgvector defaults and live in the
migration. The application never sets these per-query — there is no
env-var knob for `ef_search` either, on purpose.

The "no distance cap" form (`FindSimilar`) is identical except it drops
the `WHERE embedding <=> $1::vector < $maxDistance` predicate and
returns the top-`$limit` neighbours regardless of distance.

## Maintenance

pgvector keeps both indexes in sync automatically on every
INSERT / UPDATE / DELETE; the application does no rebuild work. There is
no `POST /api/v1/process/rebuild-index` endpoint and no in-process index
to flush on shutdown.

If recall ever feels off — for instance after a bulk re-embedding — an
operator can rebuild the index manually with:

```sql
REINDEX INDEX embeddings_embedding_hnsw_cosine_idx;
REINDEX INDEX faces_embedding_hnsw_cosine_idx;
```

This is intentionally not exposed as an app-level command. The rebuild
holds an `ACCESS EXCLUSIVE` lock and should be scheduled in a quiet
window.

## Centroid queries

The album-suggestion endpoint
(`POST /api/v1/photos/suggest-albums`) computes the album centroid in
SQL via `EmbeddingRepository.GetCentroid`, which issues
`SELECT AVG(embedding)::vector FROM embeddings WHERE photo_uid = ANY($1)`
and lets pgvector do the element-wise average in one round trip. The
ranking query is then a standard `FindSimilar*` call against the HNSW
index. See `processAlbumForSuggestions` for the wire-up.

Face-outlier detection (`POST /api/v1/faces/outliers`) does **not** use
`GetCentroid` — it pages a person's face rows into Go and averages
embeddings element-wise on the application side
(`computeFaceCentroid` in `internal/web/handlers/face_outliers.go`).
That's because outlier ranking also needs each face's individual
distance to the centroid, which the SQL aggregate would throw away.

## What was removed

Earlier versions of photo-sorter maintained a parallel in-process
graph index (via `github.com/coder/hnsw`) on top of pgvector and
persisted it to disk. That indirection is gone — pgvector alone is
now the system of record for similarity search. Operators upgrading
from an earlier release can `rm` any leftover `.hnsw`, `.meta`,
`.faces`, and `.embeddings` files in the old index location once
migration `038` has applied successfully.
