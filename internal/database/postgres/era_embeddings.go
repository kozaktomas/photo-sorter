package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/pgvector/pgvector-go"
)

// EraEmbeddingRepository provides PostgreSQL-backed era embedding storage.
type EraEmbeddingRepository struct {
	pool *Pool
}

// NewEraEmbeddingRepository creates a new PostgreSQL era embedding repository.
func NewEraEmbeddingRepository(pool *Pool) *EraEmbeddingRepository {
	return &EraEmbeddingRepository{pool: pool}
}

// GetEra retrieves an era embedding by slug, returns nil if not found.
func (r *EraEmbeddingRepository) GetEra(ctx context.Context, eraSlug string) (*database.StoredEraEmbedding, error) {
	query := `
		SELECT era_slug, era_name, representative_date, prompt_count, embedding, model, pretrained, dim, created_at
		FROM era_embeddings
		WHERE era_slug = $1
	`

	var era database.StoredEraEmbedding
	var vec pgvector.Vector
	var repDate string

	err := r.pool.QueryRow(ctx, query, eraSlug).Scan(
		&era.EraSlug,
		&era.EraName,
		&repDate,
		&era.PromptCount,
		&vec,
		&era.Model,
		&era.Pretrained,
		&era.Dim,
		&era.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query era embedding: %w", err)
	}

	era.Embedding = vec.Slice()
	era.RepresentativeDate = repDate
	return &era, nil
}

// GetAllEras retrieves all era embeddings ordered by representative date.
func (r *EraEmbeddingRepository) GetAllEras(ctx context.Context) ([]database.StoredEraEmbedding, error) {
	query := `
		SELECT era_slug, era_name, representative_date, prompt_count, embedding, model, pretrained, dim, created_at
		FROM era_embeddings
		ORDER BY representative_date
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query all era embeddings: %w", err)
	}
	defer rows.Close()

	var eras []database.StoredEraEmbedding
	for rows.Next() {
		var era database.StoredEraEmbedding
		var vec pgvector.Vector
		var repDate string

		if err := rows.Scan(
			&era.EraSlug,
			&era.EraName,
			&repDate,
			&era.PromptCount,
			&vec,
			&era.Model,
			&era.Pretrained,
			&era.Dim,
			&era.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan era embedding: %w", err)
		}

		era.Embedding = vec.Slice()
		era.RepresentativeDate = repDate
		eras = append(eras, era)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate era embeddings: %w", err)
	}

	return eras, nil
}

// CountEras returns the total number of era embeddings stored.
func (r *EraEmbeddingRepository) CountEras(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM era_embeddings").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count era embeddings: %w", err)
	}
	return count, nil
}

// SaveEra stores an era embedding centroid (upsert).
func (r *EraEmbeddingRepository) SaveEra(ctx context.Context, era database.StoredEraEmbedding) error {
	query := `
		INSERT INTO era_embeddings (era_slug, era_name, representative_date, prompt_count, embedding, model, pretrained, dim)
		VALUES ($1, $2, $3::date, $4, $5::vector, $6, $7, $8)
		ON CONFLICT (era_slug) DO UPDATE SET
			era_name = EXCLUDED.era_name,
			representative_date = EXCLUDED.representative_date,
			prompt_count = EXCLUDED.prompt_count,
			embedding = EXCLUDED.embedding,
			model = EXCLUDED.model,
			pretrained = EXCLUDED.pretrained,
			dim = EXCLUDED.dim,
			created_at = NOW()
	`

	vec := pgvector.NewVector(era.Embedding)
	_, err := r.pool.Exec(ctx, query,
		era.EraSlug,
		era.EraName,
		era.RepresentativeDate,
		era.PromptCount,
		vec,
		era.Model,
		era.Pretrained,
		era.Dim,
	)
	if err != nil {
		return fmt.Errorf("save era embedding: %w", err)
	}
	return nil
}

// DeleteEra removes an era embedding by slug.
func (r *EraEmbeddingRepository) DeleteEra(ctx context.Context, eraSlug string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM era_embeddings WHERE era_slug = $1", eraSlug)
	if err != nil {
		return fmt.Errorf("delete era embedding: %w", err)
	}
	return nil
}

// Verify interface compliance.
var _ database.EraEmbeddingReader = (*EraEmbeddingRepository)(nil)
var _ database.EraEmbeddingWriter = (*EraEmbeddingRepository)(nil)
