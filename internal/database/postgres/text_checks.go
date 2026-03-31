package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TextCheckRepository provides PostgreSQL-backed text check result storage.
type TextCheckRepository struct {
	pool *Pool
}

// NewTextCheckRepository creates a new text check repository.
func NewTextCheckRepository(pool *Pool) *TextCheckRepository {
	return &TextCheckRepository{pool: pool}
}

const upsertTextCheckSQL = `
INSERT INTO text_check_results
  (source_type, source_id, field, content_hash,
   status, readability_score, corrected_text, changes, cost_czk)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (source_type, source_id, field)
DO UPDATE SET
  content_hash = EXCLUDED.content_hash,
  status = EXCLUDED.status,
  readability_score = EXCLUDED.readability_score,
  corrected_text = EXCLUDED.corrected_text,
  changes = EXCLUDED.changes,
  cost_czk = EXCLUDED.cost_czk,
  checked_at = NOW()
RETURNING id, checked_at`

// SaveTextCheckResult upserts a text check result.
func (r *TextCheckRepository) SaveTextCheckResult(
	ctx context.Context, result *database.TextCheckResult,
) error {
	changesJSON, err := json.Marshal(result.Changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}

	err = r.pool.QueryRow(ctx, upsertTextCheckSQL,
		result.SourceType, result.SourceID, result.Field,
		result.ContentHash, result.Status, result.ReadabilityScore,
		result.CorrectedText, changesJSON, result.CostCZK,
	).Scan(&result.ID, &result.CheckedAt)
	if err != nil {
		return fmt.Errorf("save text check result: %w", err)
	}
	return nil
}

const selectTextCheckSQL = `
SELECT id, source_type, source_id, field, content_hash,
       status, readability_score, corrected_text,
       changes, cost_czk, checked_at
FROM text_check_results
WHERE (source_type, source_id, field) IN (VALUES %s)`

// GetTextCheckResults returns check results for the given keys,
// keyed by "sourceType:sourceID:field".
func (r *TextCheckRepository) GetTextCheckResults(
	ctx context.Context, keys []database.TextCheckKey,
) (map[string]database.TextCheckResult, error) {
	if len(keys) == 0 {
		return map[string]database.TextCheckResult{}, nil
	}

	args := make([]any, 0, len(keys)*3)
	valueParts := make([]string, 0, len(keys))
	for i, k := range keys {
		base := i * 3
		valueParts = append(valueParts,
			fmt.Sprintf("($%d, $%d, $%d)", base+1, base+2, base+3))
		args = append(args, k.SourceType, k.SourceID, k.Field)
	}

	query := fmt.Sprintf(selectTextCheckSQL,
		strings.Join(valueParts, ", "))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get text check results: %w", err)
	}
	defer rows.Close()

	results := make(map[string]database.TextCheckResult, len(keys))
	for rows.Next() {
		var res database.TextCheckResult
		var changesJSON []byte
		if err := rows.Scan(
			&res.ID, &res.SourceType, &res.SourceID, &res.Field,
			&res.ContentHash, &res.Status, &res.ReadabilityScore,
			&res.CorrectedText, &changesJSON, &res.CostCZK,
			&res.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan text check result: %w", err)
		}
		if changesJSON != nil {
			if unmErr := json.Unmarshal(changesJSON, &res.Changes); unmErr != nil {
				return nil, fmt.Errorf("unmarshal changes: %w", unmErr)
			}
		}
		key := res.SourceType + ":" + res.SourceID + ":" + res.Field
		results[key] = res
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate text check results: %w", err)
	}
	return results, nil
}
