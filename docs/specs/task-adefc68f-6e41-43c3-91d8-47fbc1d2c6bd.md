# Migration export API: bulk embedding and face-vector feeds

The embeddings and face vectors are the most valuable thing in this library and the **only** part of it that is completely invisible over HTTP. Kukátko migrates over the API, so without these endpoints it must re-run inference on all 20k photos — hours of GPU on a box that is often offline, to recreate vectors that already exist here.

Production holds **20,092 embeddings (768-dim CLIP)** and **112,806 faces (512-dim)** across 14,567 photos.

## What is missing today (audited against this repo)

- The `embeddings` table is used server-side only (`/photos/similar`, `/photos/search-by-text`, duplicates). No handler ever serialises a vector — a grep for `json:"embedding"` hits only the sidecar client in `internal/fingerprint/`.
- `GET /photos/{uid}/faces` returns `embeddings_count` (`handlers/face_photos.go:20`) and never the 512-dim vectors.

## Requirements

- **`GET /api/v1/embeddings?after=<uid>&limit=N`** — a keyset-paginated feed of every photo embedding:
  `{photo_uid, model, pretrained, dim, embedding: [float32]}`. Stable order by `photo_uid`, so an interrupted export resumes with `after=`.
- **`GET /api/v1/faces?after=<id>&limit=N`** — the `faces` rows verbatim:
  `{id, photo_uid, face_index, embedding: [float32] (512), bbox, det_score, model, marker_uid, subject_uid}`.
- Per-photo variants for spot checks: `GET /photos/{uid}/embedding` and `GET /photos/{uid}/faces?include_embeddings=true`.
- Both feeds are **read-only** and gated by the read-only migration token from the companion task (never anonymous — this is the whole library's fingerprint).
- Payload size is the design constraint: 112k × 512 floats is not something to hold in RAM or send in one response. Stream/paginate, cap `limit` server-side, and document the caps. Offer a compact encoding (base64 float32 array) alongside the JSON number array if the plain form is too fat.
- Include `model`/`pretrained`/`dim` on every vector — Kukátko must be able to prove the vectors it imports came from the same model it will query with, and reject them otherwise.
- Tests: pagination covers every row exactly once; a vector round-trips byte-identically (float32 precision is not lost through the encoding).

## Notes

- Schema: `embeddings` (768-dim `VECTOR`, `model`, `pretrained`, `dim`) and `faces` (512-dim `VECTOR`, `bbox`, `det_score`, `marker_uid`, `subject_uid`), both pgvector.
- Kukátko stores these in Postgres as `halfvec` with HNSW cosine indexes and runs the **same models** (its inference sidecar is the same service this app uses), so this is a 1:1 transfer, not a conversion.
- Do not expose the vectors on any existing public/share route.
