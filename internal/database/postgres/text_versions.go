package postgres

import (
	"context"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TextVersionRepository provides PostgreSQL-backed text version storage.
type TextVersionRepository struct {
	pool *Pool
}

// NewTextVersionRepository creates a new text version repository.
func NewTextVersionRepository(pool *Pool) *TextVersionRepository {
	return &TextVersionRepository{pool: pool}
}

// SaveTextVersion inserts a new text version record.
func (r *TextVersionRepository) SaveTextVersion(ctx context.Context, version *database.TextVersion) error {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO text_versions (source_type, source_id, field, content, changed_by)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		version.SourceType, version.SourceID, version.Field, version.Content, version.ChangedBy,
	).Scan(&version.ID, &version.CreatedAt)
	if err != nil {
		return fmt.Errorf("save text version: %w", err)
	}
	return nil
}

// ListTextVersions returns recent versions for a specific text field, newest first.
func (r *TextVersionRepository) ListTextVersions(
	ctx context.Context, sourceType, sourceID, field string, limit int,
) ([]database.TextVersion, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, source_type, source_id, field, content, changed_by, created_at
		 FROM text_versions
		 WHERE source_type = $1 AND source_id = $2 AND field = $3
		 ORDER BY created_at DESC LIMIT $4`,
		sourceType, sourceID, field, limit)
	if err != nil {
		return nil, fmt.Errorf("list text versions: %w", err)
	}
	defer rows.Close()
	var versions []database.TextVersion
	for rows.Next() {
		var v database.TextVersion
		if err := rows.Scan(&v.ID, &v.SourceType, &v.SourceID, &v.Field, &v.Content, &v.ChangedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan text version: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate text versions: %w", err)
	}
	return versions, nil
}

// GetTextVersion retrieves a single text version by ID.
func (r *TextVersionRepository) GetTextVersion(ctx context.Context, id int) (*database.TextVersion, error) {
	var v database.TextVersion
	err := r.pool.QueryRow(ctx,
		`SELECT id, source_type, source_id, field, content, changed_by, created_at
		 FROM text_versions WHERE id = $1`, id,
	).Scan(&v.ID, &v.SourceType, &v.SourceID, &v.Field, &v.Content, &v.ChangedBy, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get text version: %w", err)
	}
	return &v, nil
}
