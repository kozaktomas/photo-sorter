-- pgvector HNSW indexes for similarity search.
--
-- Replaces the previous file-backed in-memory HNSW indexes
-- (HNSW_INDEX_PATH / HNSW_EMBEDDING_INDEX_PATH) with native pgvector
-- indexes that the database maintains automatically on INSERT/UPDATE/DELETE.
-- After this migration the application holds no in-memory vector data
-- structures and a pg_dump captures all metadata, including similarity
-- search state.
--
-- Default HNSW build params (m=16, ef_construction=64) are used; we set
-- ef_search=100 per-query at runtime. The migration runner wraps each
-- migration in a transaction, so CREATE INDEX CONCURRENTLY is not
-- available — the first server start after deploy will block while these
-- indexes build (expected: minutes on a 50k-row table on the Pi).
-- Operators can `rm` any leftover .hnsw / .meta / .faces / .embeddings
-- files in STORAGE_CACHE_PATH (or wherever HNSW_INDEX_PATH pointed at)
-- once this migration has applied successfully.

CREATE INDEX IF NOT EXISTS embeddings_embedding_hnsw_cosine_idx
    ON embeddings
    USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS faces_embedding_hnsw_cosine_idx
    ON faces
    USING hnsw (embedding vector_cosine_ops);
