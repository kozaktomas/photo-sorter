package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// EmbeddingRepository provides PostgreSQL-backed embedding storage with optional in-memory HNSW index
type EmbeddingRepository struct {
	pool          *Pool
	hnswIndex     *database.HNSWEmbeddingIndex
	hnswEnabled   bool
	hnswIndexPath string // Path to persist HNSW index (optional)
	hnswMu        sync.RWMutex
}

// NewEmbeddingRepository creates a new PostgreSQL embedding repository
func NewEmbeddingRepository(pool *Pool) *EmbeddingRepository {
	return &EmbeddingRepository{pool: pool}
}

// Get retrieves an embedding by photo UID, returns nil if not found
func (r *EmbeddingRepository) Get(ctx context.Context, photoUID string) (*database.StoredEmbedding, error) {
	query := `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		WHERE photo_uid = $1
	`

	var emb database.StoredEmbedding
	var vec pgvector.Vector

	err := r.pool.QueryRow(ctx, query, photoUID).Scan(
		&emb.PhotoUID,
		&vec,
		&emb.Model,
		&emb.Pretrained,
		&emb.Dim,
		&emb.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}

	emb.Embedding = vec.Slice()
	return &emb, nil
}

// Has checks if an embedding exists for the given photo UID
func (r *EmbeddingRepository) Has(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM embeddings WHERE photo_uid = $1)", photoUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check embedding exists: %w", err)
	}
	return exists, nil
}

// Count returns the total number of embeddings stored
func (r *EmbeddingRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return count, nil
}

// CountByUIDs returns the number of embeddings whose photo_uid is in the given list
func (r *EmbeddingRepository) CountByUIDs(ctx context.Context, uids []string) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings WHERE photo_uid = ANY($1)", pq.Array(uids)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count embeddings by UIDs: %w", err)
	}
	return count, nil
}

// FindSimilar finds the most similar embeddings using cosine distance.
// Uses in-memory HNSW index if enabled, otherwise falls back to PostgreSQL.
func (r *EmbeddingRepository) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]database.StoredEmbedding, error) {
	// Use HNSW if enabled
	r.hnswMu.RLock()
	hnswEnabled := r.hnswEnabled && r.hnswIndex != nil
	r.hnswMu.RUnlock()

	if hnswEnabled {
		return r.findSimilarHNSW(embedding, limit)
	}

	// Fallback to PostgreSQL with ef_search optimization
	return r.findSimilarPostgres(ctx, embedding, limit)
}

// findSimilarHNSW uses the in-memory HNSW index for similarity search
func (r *EmbeddingRepository) findSimilarHNSW(embedding []float32, limit int) ([]database.StoredEmbedding, error) {
	r.hnswMu.RLock()
	defer r.hnswMu.RUnlock()

	if r.hnswIndex == nil {
		return nil, errors.New("HNSW index not initialized")
	}

	ids, _, err := r.hnswIndex.Search(embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("HNSW search: %w", err)
	}

	results := make([]database.StoredEmbedding, 0, len(ids))
	for _, id := range ids {
		emb := r.hnswIndex.GetEmbedding(id)
		if emb != nil {
			results = append(results, *emb)
		}
	}

	return results, nil
}

// findSimilarPostgres uses PostgreSQL for similarity search with ef_search optimization
func (r *EmbeddingRepository) findSimilarPostgres(ctx context.Context, embedding []float32, limit int) ([]database.StoredEmbedding, error) {
	// Use transaction to set ef_search for better recall (matching GOB HNSW config)
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Set ef_search to match GOB HNSW configuration
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", database.HNSWEfSearch)); err != nil {
		return nil, fmt.Errorf("set ef_search: %w", err)
	}

	query := `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`

	vec := pgvector.NewVector(embedding)
	rows, err := tx.QueryContext(ctx, query, vec, limit)
	if err != nil {
		return nil, fmt.Errorf("query similar embeddings: %w", err)
	}
	defer rows.Close()

	return scanEmbeddings(rows)
}

// FindSimilarWithDistance finds similar embeddings and returns distances.
// Uses in-memory HNSW index if enabled, otherwise falls back to PostgreSQL.
func (r *EmbeddingRepository) FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]database.StoredEmbedding, []float64, error) {
	// Use HNSW if enabled
	r.hnswMu.RLock()
	hnswEnabled := r.hnswEnabled && r.hnswIndex != nil
	r.hnswMu.RUnlock()

	if hnswEnabled {
		return r.findSimilarWithDistanceHNSW(embedding, limit, maxDistance)
	}

	// Fallback to PostgreSQL with ef_search optimization
	return r.findSimilarWithDistancePostgres(ctx, embedding, limit, maxDistance)
}

// findSimilarWithDistanceHNSW uses the in-memory HNSW index for similarity search
func (r *EmbeddingRepository) findSimilarWithDistanceHNSW(embedding []float32, limit int, maxDistance float64) ([]database.StoredEmbedding, []float64, error) {
	r.hnswMu.RLock()
	defer r.hnswMu.RUnlock()

	if r.hnswIndex == nil {
		return nil, nil, errors.New("HNSW index not initialized")
	}

	// Request more candidates to ensure we have enough after distance filtering
	searchK := limit * database.HNSWSearchMultiplier
	searchK = max(searchK, 100) // Minimum search size for better recall

	ids, distances, err := r.hnswIndex.SearchWithDistance(embedding, searchK, maxDistance)
	if err != nil {
		return nil, nil, fmt.Errorf("HNSW search: %w", err)
	}

	// Collect results up to limit
	results := make([]database.StoredEmbedding, 0, limit)
	distancesOut := make([]float64, 0, limit)

	for i, id := range ids {
		emb := r.hnswIndex.GetEmbedding(id)
		if emb == nil {
			continue
		}
		results = append(results, *emb)
		distancesOut = append(distancesOut, distances[i])
		if len(results) >= limit {
			break
		}
	}

	return results, distancesOut, nil
}

// findSimilarWithDistancePostgres uses PostgreSQL for similarity search with ef_search optimization
func (r *EmbeddingRepository) findSimilarWithDistancePostgres(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]database.StoredEmbedding, []float64, error) {
	// Use transaction to set ef_search for better recall (matching GOB HNSW config)
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Set ef_search to match GOB HNSW configuration
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", database.HNSWEfSearch)); err != nil {
		return nil, nil, fmt.Errorf("set ef_search: %w", err)
	}

	query := `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at,
		       embedding <=> $1::vector AS distance
		FROM embeddings
		WHERE embedding <=> $1::vector < $2
		ORDER BY distance
		LIMIT $3
	`

	vec := pgvector.NewVector(embedding)
	rows, err := tx.QueryContext(ctx, query, vec, maxDistance, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("query similar embeddings: %w", err)
	}
	defer rows.Close()

	var embeddings []database.StoredEmbedding
	var distances []float64

	for rows.Next() {
		var emb database.StoredEmbedding
		var vec pgvector.Vector
		var dist float64

		if err := rows.Scan(
			&emb.PhotoUID,
			&vec,
			&emb.Model,
			&emb.Pretrained,
			&emb.Dim,
			&emb.CreatedAt,
			&dist,
		); err != nil {
			return nil, nil, fmt.Errorf("scan embedding: %w", err)
		}

		emb.Embedding = vec.Slice()
		embeddings = append(embeddings, emb)
		distances = append(distances, dist)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate embeddings: %w", err)
	}

	return embeddings, distances, nil
}

// Save stores an embedding (upsert)
func (r *EmbeddingRepository) Save(ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int) error {
	query := `
		INSERT INTO embeddings (photo_uid, embedding, model, pretrained, dim)
		VALUES ($1, $2::vector, $3, $4, $5)
		ON CONFLICT (photo_uid) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			pretrained = EXCLUDED.pretrained,
			dim = EXCLUDED.dim,
			created_at = NOW()
	`

	vec := pgvector.NewVector(embedding)
	_, err := r.pool.Exec(ctx, query, photoUID, vec, model, pretrained, dim)
	if err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}
	return nil
}

// SaveBatch saves multiple embeddings in a single transaction
func (r *EmbeddingRepository) SaveBatch(ctx context.Context, embeddings []database.StoredEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO embeddings (photo_uid, embedding, model, pretrained, dim)
		VALUES ($1, $2::vector, $3, $4, $5)
		ON CONFLICT (photo_uid) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			pretrained = EXCLUDED.pretrained,
			dim = EXCLUDED.dim,
			created_at = NOW()
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, emb := range embeddings {
		vec := pgvector.NewVector(emb.Embedding)
		if _, err := stmt.ExecContext(ctx, emb.PhotoUID, vec, emb.Model, emb.Pretrained, emb.Dim); err != nil {
			return fmt.Errorf("insert embedding %s: %w", emb.PhotoUID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func scanEmbeddings(rows *sql.Rows) ([]database.StoredEmbedding, error) {
	var embeddings []database.StoredEmbedding

	for rows.Next() {
		var emb database.StoredEmbedding
		var vec pgvector.Vector

		if err := rows.Scan(
			&emb.PhotoUID,
			&vec,
			&emb.Model,
			&emb.Pretrained,
			&emb.Dim,
			&emb.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan embedding: %w", err)
		}

		emb.Embedding = vec.Slice()
		embeddings = append(embeddings, emb)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embeddings: %w", err)
	}

	return embeddings, nil
}

// GetAllEmbeddings retrieves all embeddings from the database
func (r *EmbeddingRepository) GetAllEmbeddings(ctx context.Context) ([]database.StoredEmbedding, error) {
	query := `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		ORDER BY photo_uid
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query all embeddings: %w", err)
	}
	defer rows.Close()

	return scanEmbeddings(rows)
}

// tryLoadEmbeddingIndex attempts to load the HNSW index from disk.
// Returns true if the index was loaded successfully.
func (r *EmbeddingRepository) tryLoadEmbeddingIndex(ctx context.Context, indexPath string, dbEmbCount int64) bool {
	metadata, metaErr := database.LoadHNSWEmbeddingMetadata(indexPath)
	if metaErr != nil {
		fmt.Printf("Embedding index: metadata file error: %v (will rebuild)\n", metaErr)
		return false
	}
	if metadata.EmbeddingCount != dbEmbCount {
		fmt.Printf("Embedding index: stale (db: count=%d, cached: count=%d) (will rebuild)\n",
			dbEmbCount, metadata.EmbeddingCount)
		return false
	}

	return r.tryLoadFreshEmbeddingIndex(ctx, indexPath)
}

// tryLoadFreshEmbeddingIndex attempts to load a fresh index, with fallback to legacy format.
func (r *EmbeddingRepository) tryLoadFreshEmbeddingIndex(ctx context.Context, indexPath string) bool {
	r.hnswIndex = database.NewHNSWEmbeddingIndex()
	if err := r.hnswIndex.LoadWithEmbeddingMetadata(indexPath); err != nil {
		fmt.Printf("Embedding index: failed to load with metadata: %v (trying fallback)\n", err)
		return r.tryLoadFallbackEmbeddingIndex(ctx, indexPath)
	}
	if r.hnswIndex.IsEmpty() {
		fmt.Printf("Embedding index: loaded graph is empty (will rebuild)\n")
		return false
	}
	fmt.Printf("Embedding index: loaded from disk (fresh)\n")
	return true
}

// tryLoadFallbackEmbeddingIndex attempts the legacy load path without metadata.
func (r *EmbeddingRepository) tryLoadFallbackEmbeddingIndex(ctx context.Context, indexPath string) bool {
	r.hnswIndex = database.NewHNSWEmbeddingIndex()
	if err := r.hnswIndex.Load(indexPath); err != nil {
		fmt.Printf("Embedding index: fallback load failed: %v (will rebuild)\n", err)
		return false
	}
	if r.hnswIndex.IsEmpty() {
		fmt.Printf("Embedding index: fallback loaded graph is empty (will rebuild)\n")
		return false
	}
	fmt.Println("Loading embeddings from database (consider running 'Rebuild Index' to create .embeddings file for faster startup)...")
	embeddings, err := r.GetAllEmbeddings(ctx)
	if err != nil {
		fmt.Printf("Embedding index: failed to load embeddings for fallback: %v (will rebuild)\n", err)
		return false
	}
	r.hnswIndex.RebuildFromEmbeddings(embeddings)
	fmt.Printf("Embedding index: loaded from disk (fallback path)\n")
	return true
}

// EnableHNSW loads or builds an in-memory HNSW index for O(log N) similarity search.
// If indexPath is provided, it will try to load from disk first and save after building.
// This should be called once at startup.
func (r *EmbeddingRepository) EnableHNSW(ctx context.Context, indexPath string) error {
	r.hnswMu.Lock()
	defer r.hnswMu.Unlock()

	r.hnswIndexPath = indexPath

	var dbEmbCount int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&dbEmbCount)
	if err != nil {
		return fmt.Errorf("failed to get embedding count: %w", err)
	}

	if indexPath != "" && r.tryLoadEmbeddingIndex(ctx, indexPath, dbEmbCount) {
		r.hnswEnabled = true
		return nil
	}

	embeddings, err := r.GetAllEmbeddings(ctx)
	if err != nil {
		return fmt.Errorf("failed to load embeddings: %w", err)
	}

	r.hnswIndex = database.NewHNSWEmbeddingIndex()
	if err := r.hnswIndex.BuildFromEmbeddings(embeddings); err != nil {
		return fmt.Errorf("failed to build HNSW embedding index: %w", err)
	}

	if indexPath != "" && len(embeddings) > 0 {
		metadata := database.HNSWEmbeddingIndexMetadata{EmbeddingCount: dbEmbCount}
		if err := r.hnswIndex.SaveWithEmbeddingMetadata(indexPath, metadata); err != nil {
			fmt.Printf("Warning: failed to save HNSW embedding index to disk: %v\n", err)
		}
	}

	r.hnswEnabled = true
	return nil
}

// DisableHNSW disables the in-memory HNSW index, falling back to PostgreSQL queries
func (r *EmbeddingRepository) DisableHNSW() {
	r.hnswMu.Lock()
	defer r.hnswMu.Unlock()
	r.hnswEnabled = false
	r.hnswIndex = nil
}

// IsHNSWEnabled returns whether the in-memory HNSW index is enabled
func (r *EmbeddingRepository) IsHNSWEnabled() bool {
	r.hnswMu.RLock()
	defer r.hnswMu.RUnlock()
	return r.hnswEnabled && r.hnswIndex != nil
}

// HNSWCount returns the number of embeddings in the HNSW index
func (r *EmbeddingRepository) HNSWCount() int {
	r.hnswMu.RLock()
	defer r.hnswMu.RUnlock()
	if r.hnswIndex == nil {
		return 0
	}
	return r.hnswIndex.Count()
}

// RebuildHNSW rebuilds the HNSW index from PostgreSQL data
func (r *EmbeddingRepository) RebuildHNSW(ctx context.Context) error {
	r.hnswMu.RLock()
	indexPath := r.hnswIndexPath
	r.hnswMu.RUnlock()
	return r.EnableHNSW(ctx, indexPath)
}

// SaveHNSWIndex saves the current HNSW index to disk (if path configured)
func (r *EmbeddingRepository) SaveHNSWIndex() error {
	r.hnswMu.RLock()
	defer r.hnswMu.RUnlock()

	if r.hnswIndexPath == "" {
		fmt.Println("Embedding index save: no path configured, skipping")
		return nil // No path configured, nothing to save
	}

	if r.hnswIndex == nil {
		fmt.Println("Embedding index save: no index in memory, skipping")
		return nil // No index to save
	}

	fmt.Printf("Embedding index save: saving to %s\n", r.hnswIndexPath)

	// Get current database stats for metadata
	ctx := context.Background()
	var embCount int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&embCount)
	if err != nil {
		return fmt.Errorf("failed to get embedding count: %w", err)
	}

	metadata := database.HNSWEmbeddingIndexMetadata{
		EmbeddingCount: embCount,
	}

	if err := r.hnswIndex.SaveWithEmbeddingMetadata(r.hnswIndexPath, metadata); err != nil {
		return fmt.Errorf("saving HNSW embedding index: %w", err)
	}

	fmt.Printf("Embedding index save: saved successfully (count=%d)\n", embCount)
	return nil
}

// DeleteEmbedding removes the embedding for a photo and cleans up the HNSW index
func (r *EmbeddingRepository) DeleteEmbedding(ctx context.Context, photoUID string) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM embeddings WHERE photo_uid = $1", photoUID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}

	// Remove from HNSW index
	r.hnswMu.RLock()
	hnswEnabled := r.hnswEnabled && r.hnswIndex != nil
	r.hnswMu.RUnlock()

	if hnswEnabled {
		r.hnswIndex.Delete(photoUID)
	}

	return nil
}

// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings
func (r *EmbeddingRepository) GetUniquePhotoUIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT photo_uid FROM embeddings ORDER BY photo_uid")
	if err != nil {
		return nil, fmt.Errorf("query embedding photo UIDs: %w", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan photo UID: %w", err)
		}
		uids = append(uids, uid)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo UIDs: %w", err)
	}

	return uids, nil
}

// Verify interface compliance
var _ database.EmbeddingReader = (*EmbeddingRepository)(nil)
var _ database.EmbeddingWriter = (*EmbeddingRepository)(nil)
