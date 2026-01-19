package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// StoredEmbedding represents an embedding stored in the database
type StoredEmbedding struct {
	PhotoUID   string
	Embedding  []float32
	Model      string
	Pretrained string
	Dim        int
	CreatedAt  time.Time
}

// EmbeddingRepository handles database operations for embeddings
type EmbeddingRepository struct {
	pool *pgxpool.Pool
}

// NewEmbeddingRepository creates a new repository
func NewEmbeddingRepository(pool *pgxpool.Pool) *EmbeddingRepository {
	return &EmbeddingRepository{pool: pool}
}

// Save stores an embedding in the database (upsert)
func (r *EmbeddingRepository) Save(ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int) error {
	vec := pgvector.NewVector(embedding)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO embeddings (photo_uid, embedding, model, pretrained, dim, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (photo_uid)
		DO UPDATE SET embedding = $2, model = $3, pretrained = $4, dim = $5, created_at = NOW()
	`, photoUID, vec, model, pretrained, dim)

	return err
}

// Get retrieves an embedding by photo UID, returns nil if not found
func (r *EmbeddingRepository) Get(ctx context.Context, photoUID string) (*StoredEmbedding, error) {
	var stored StoredEmbedding
	var vec pgvector.Vector

	err := r.pool.QueryRow(ctx, `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		WHERE photo_uid = $1
	`, photoUID).Scan(&stored.PhotoUID, &vec, &stored.Model, &stored.Pretrained, &stored.Dim, &stored.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	stored.Embedding = vec.Slice()
	return &stored, nil
}

// Has checks if an embedding exists for the given photo UID
func (r *EmbeddingRepository) Has(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM embeddings WHERE photo_uid = $1)
	`, photoUID).Scan(&exists)
	return exists, err
}

// Count returns the total number of embeddings stored
func (r *EmbeddingRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM embeddings").Scan(&count)
	return count, err
}

// FindSimilar finds the most similar embeddings using cosine distance
func (r *EmbeddingRepository) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredEmbedding, error) {
	vec := pgvector.NewVector(embedding)

	rows, err := r.pool.Query(ctx, `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at
		FROM embeddings
		ORDER BY embedding <=> $1
		LIMIT $2
	`, vec, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StoredEmbedding
	for rows.Next() {
		var stored StoredEmbedding
		var v pgvector.Vector
		if err := rows.Scan(&stored.PhotoUID, &v, &stored.Model, &stored.Pretrained, &stored.Dim, &stored.CreatedAt); err != nil {
			return nil, err
		}
		stored.Embedding = v.Slice()
		results = append(results, stored)
	}

	return results, rows.Err()
}

// FindSimilarWithDistance finds the most similar embeddings using cosine distance and returns distances
func (r *EmbeddingRepository) FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredEmbedding, []float64, error) {
	vec := pgvector.NewVector(embedding)

	rows, err := r.pool.Query(ctx, `
		SELECT photo_uid, embedding, model, pretrained, dim, created_at, embedding <=> $1 as distance
		FROM embeddings
		WHERE embedding <=> $1 < $3
		ORDER BY embedding <=> $1
		LIMIT $2
	`, vec, limit, maxDistance)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var results []StoredEmbedding
	var distances []float64
	for rows.Next() {
		var stored StoredEmbedding
		var v pgvector.Vector
		var distance float64
		if err := rows.Scan(&stored.PhotoUID, &v, &stored.Model, &stored.Pretrained, &stored.Dim, &stored.CreatedAt, &distance); err != nil {
			return nil, nil, err
		}
		stored.Embedding = v.Slice()
		results = append(results, stored)
		distances = append(distances, distance)
	}

	return results, distances, rows.Err()
}
