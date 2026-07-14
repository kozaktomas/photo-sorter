package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
)

// hnswEfSearch is the pgvector ef_search parameter used for every cosine
// nearest-neighbour query in this package. Higher values trade query
// latency for recall. The value is intentionally hard-coded (no env-var
// knob) — the schema-level HNSW build parameters live in the migration
// that creates the index.
const hnswEfSearch = 100

// EmbeddingRepository provides pgvector-backed image embedding storage.
type EmbeddingRepository struct {
	pool *Pool
}

// NewEmbeddingRepository creates a new PostgreSQL embedding repository.
func NewEmbeddingRepository(pool *Pool) *EmbeddingRepository {
	return &EmbeddingRepository{pool: pool}
}

// Get retrieves an embedding by photo UID, returns nil if not found.
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

// Has checks if an embedding exists for the given photo UID.
func (r *EmbeddingRepository) Has(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM embeddings WHERE photo_uid = $1)", photoUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check embedding exists: %w", err)
	}
	return exists, nil
}

// Count returns the total number of embeddings stored.
func (r *EmbeddingRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return count, nil
}

// CountByUIDs returns the number of embeddings whose photo_uid is in the given list.
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
// The query runs in a read-only transaction so SET LOCAL hnsw.ef_search
// applies to the SELECT.
func (r *EmbeddingRepository) FindSimilar(
	ctx context.Context, embedding []float32, limit int,
) ([]database.StoredEmbedding, error) {
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch)); err != nil {
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

// FindSimilarWithDistance returns the closest embeddings whose cosine
// distance to the query vector is strictly less than maxDistance, ordered
// by distance ascending, capped at limit.
func (r *EmbeddingRepository) FindSimilarWithDistance(
	ctx context.Context, embedding []float32, limit int, maxDistance float64,
) ([]database.StoredEmbedding, []float64, error) {
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch)); err != nil {
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

// GetCentroid returns the L2-normalized AVG(embedding) over the given
// photo UIDs computed in pgvector. Returns nil when no rows match.
// Used by the album-suggestion handler to replace a N-photo Go-side
// averaging loop with a single round trip.
func (r *EmbeddingRepository) GetCentroid(
	ctx context.Context, photoUIDs []string,
) ([]float32, error) {
	if len(photoUIDs) == 0 {
		return nil, nil
	}

	var centroid pgvector.Vector
	err := r.pool.QueryRow(ctx, `
		SELECT AVG(embedding)::vector
		FROM embeddings
		WHERE photo_uid = ANY($1)
	`, pq.Array(photoUIDs)).Scan(&centroid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compute centroid: %w", err)
	}

	return centroid.Slice(), nil
}

// Save stores an embedding (upsert).
func (r *EmbeddingRepository) Save(
	ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int,
) error {
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

// SaveBatch saves multiple embeddings in a single transaction.
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

// GetAllEmbeddings retrieves all embeddings from the database.
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

// ListEmbeddingsAfter returns the embeddings whose photo_uid sorts strictly
// after afterPhotoUID, ordered by photo_uid, capped at the export limit. It
// backs the keyset-paginated migration feed (GET /api/v1/embeddings).
//
// The empty afterPhotoUID starts the walk: no photo_uid is the empty string
// (it is a NOT NULL primary key generated by NewPhotoUID), and the empty
// string sorts below every non-empty one in any collation, so `photo_uid > ”`
// selects the whole table while still driving an ordered index scan on the
// primary key — no special-cased first-page query needed.
//
// GetAllEmbeddings exists next door and returns the same rows in the same
// order, but materialises all 20k of them (≈60 MB of float32) at once. This
// method is what an HTTP feed can afford.
func (r *EmbeddingRepository) ListEmbeddingsAfter(
	ctx context.Context, afterPhotoUID string, limit int,
) ([]database.StoredEmbedding, error) {
	query := `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		WHERE photo_uid > $1
		ORDER BY photo_uid
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, afterPhotoUID, database.ClampEmbeddingExportLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query embeddings after %q: %w", afterPhotoUID, err)
	}
	defer rows.Close()

	return scanEmbeddings(rows)
}

// DeleteEmbedding removes the embedding for a photo. pgvector keeps the
// index in sync automatically.
func (r *EmbeddingRepository) DeleteEmbedding(ctx context.Context, photoUID string) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM embeddings WHERE photo_uid = $1", photoUID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	return nil
}

// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings.
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

// Verify interface compliance.
var _ database.EmbeddingReader = (*EmbeddingRepository)(nil)
var _ database.EmbeddingWriter = (*EmbeddingRepository)(nil)
var _ database.EmbeddingExportReader = (*EmbeddingRepository)(nil)
