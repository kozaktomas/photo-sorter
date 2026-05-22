package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// PhotoEditsRepository implements database.PhotoEditsReader and
// database.PhotoEditsWriter against the photo_edits table (migration 041).
//
// A row exists only when at least one of crop / rotation / brightness /
// contrast is at a non-default value. The four crop columns are all-NULL
// or all-NON-NULL per the CHECK constraint on the table, so the Go side
// can safely round-trip via sql.NullFloat64 without re-validating.
type PhotoEditsRepository struct {
	pool *Pool
}

// NewPhotoEditsRepository returns a PhotoEditsRepository bound to the given pool.
func NewPhotoEditsRepository(pool *Pool) *PhotoEditsRepository {
	return &PhotoEditsRepository{pool: pool}
}

// GetPhotoEdits returns the stored edit parameters for the given photo.
// Returns database.ErrNotFound when no row exists (i.e. the photo has not
// been edited yet).
func (r *PhotoEditsRepository) GetPhotoEdits(
	ctx context.Context, photoUID string,
) (*database.PhotoEdits, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT photo_uid, crop_x, crop_y, crop_w, crop_h,
		        rotation, brightness, contrast, updated_at
		   FROM photo_edits WHERE photo_uid = $1`, photoUID)

	var (
		out                  database.PhotoEdits
		cx, cy, cw, ch       sql.NullFloat64
		brightness, contrast float64
		rotation             int
	)
	if err := row.Scan(
		&out.PhotoUID, &cx, &cy, &cw, &ch,
		&rotation, &brightness, &contrast, &out.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("get photo edits: %w", err)
	}
	out.Rotation = rotation
	out.Brightness = brightness
	out.Contrast = contrast
	if cx.Valid && cy.Valid && cw.Valid && ch.Valid {
		out.Crop = &database.PhotoEditsCrop{
			X: cx.Float64, Y: cy.Float64, W: cw.Float64, H: ch.Float64,
		}
	}
	return &out, nil
}

// SavePhotoEdits upserts the edit row for the given photo. The caller is
// responsible for clamping values to the documented ranges — the database
// only enforces the rotation CHECK and the crop-all-or-nothing CHECK.
func (r *PhotoEditsRepository) SavePhotoEdits(
	ctx context.Context, edits *database.PhotoEdits,
) error {
	var cx, cy, cw, ch sql.NullFloat64
	if edits.Crop != nil {
		cx = sql.NullFloat64{Float64: edits.Crop.X, Valid: true}
		cy = sql.NullFloat64{Float64: edits.Crop.Y, Valid: true}
		cw = sql.NullFloat64{Float64: edits.Crop.W, Valid: true}
		ch = sql.NullFloat64{Float64: edits.Crop.H, Valid: true}
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO photo_edits
		     (photo_uid, crop_x, crop_y, crop_w, crop_h,
		      rotation, brightness, contrast, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		 ON CONFLICT (photo_uid) DO UPDATE SET
		     crop_x     = EXCLUDED.crop_x,
		     crop_y     = EXCLUDED.crop_y,
		     crop_w     = EXCLUDED.crop_w,
		     crop_h     = EXCLUDED.crop_h,
		     rotation   = EXCLUDED.rotation,
		     brightness = EXCLUDED.brightness,
		     contrast   = EXCLUDED.contrast,
		     updated_at = NOW()
		 RETURNING updated_at`,
		edits.PhotoUID, cx, cy, cw, ch,
		edits.Rotation, edits.Brightness, edits.Contrast,
	)
	if err := row.Scan(&edits.UpdatedAt); err != nil {
		return fmt.Errorf("save photo edits: %w", err)
	}
	return nil
}

// DeletePhotoEdits removes the edit row for the given photo. Returns nil
// when no row was present so the revert-to-original flow is idempotent.
func (r *PhotoEditsRepository) DeletePhotoEdits(
	ctx context.Context, photoUID string,
) error {
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM photo_edits WHERE photo_uid = $1`, photoUID); err != nil {
		return fmt.Errorf("delete photo edits: %w", err)
	}
	return nil
}

// Verify interface compliance.
var (
	_ database.PhotoEditsReader = (*PhotoEditsRepository)(nil)
	_ database.PhotoEditsWriter = (*PhotoEditsRepository)(nil)
)
