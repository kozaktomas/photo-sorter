package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// PHashRepository implements database.PHashReader and database.PHashWriter
// against the photo_phashes table (migration 034). PHash/DHash are stored
// as BIGINT in PostgreSQL — uint64 ↔ int64 conversion is a no-op cast
// preserving the bit pattern (two's complement). Hamming distance is
// computed in Go after a full-table fetch; the table is one row per photo
// and 24 bytes wide so even a million-photo library is < 25 MB.
type PHashRepository struct {
	pool *Pool
}

// NewPHashRepository returns a PHashRepository bound to the given pool.
func NewPHashRepository(pool *Pool) *PHashRepository {
	return &PHashRepository{pool: pool}
}

// GetPHash returns the stored pHash + dHash for the given photo. Returns
// database.ErrNotFound when no row exists yet.
func (r *PHashRepository) GetPHash(ctx context.Context, photoUID string) (*database.PhotoPHash, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT photo_uid, phash, dhash, created_at
		 FROM photo_phashes WHERE photo_uid = $1`, photoUID)

	var (
		out         database.PhotoPHash
		phashI64    int64
		dhashI64    int64
		uid         string
		createdScan sql.NullTime
	)
	if err := row.Scan(&uid, &phashI64, &dhashI64, &createdScan); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("get phash: %w", err)
	}
	out.PhotoUID = uid
	out.PHash = uint64(phashI64) //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
	out.DHash = uint64(dhashI64) //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
	if createdScan.Valid {
		out.CreatedAt = createdScan.Time
	}
	return &out, nil
}

// ListAllPHashes returns every row in photo_phashes ordered by photo_uid
// (deterministic for tests). Callers should treat the result set as the
// scan target for hamming-distance comparison against a candidate hash.
func (r *PHashRepository) ListAllPHashes(ctx context.Context) ([]database.PhotoPHash, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT photo_uid, phash, dhash, created_at
		 FROM photo_phashes ORDER BY photo_uid`)
	if err != nil {
		return nil, fmt.Errorf("list phashes: %w", err)
	}
	defer rows.Close()

	var out []database.PhotoPHash
	for rows.Next() {
		var (
			ph       database.PhotoPHash
			phashI64 int64
			dhashI64 int64
		)
		if err := rows.Scan(&ph.PhotoUID, &phashI64, &dhashI64, &ph.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan phash: %w", err)
		}
		ph.PHash = uint64(phashI64) //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
		ph.DHash = uint64(dhashI64) //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
		out = append(out, ph)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate phashes: %w", err)
	}
	return out, nil
}

// CountPHashes returns the number of rows in photo_phashes.
func (r *PHashRepository) CountPHashes(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM photo_phashes`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count phashes: %w", err)
	}
	return n, nil
}

// ListPhotosWithoutPHash returns photo UIDs that have no row in
// photo_phashes yet, up to limit rows (0 = no limit). Drives the backfill
// command. Archived photos are included — the backfill should hash them
// too so the duplicate detector still works after a restore.
func (r *PHashRepository) ListPhotosWithoutPHash(ctx context.Context, limit int) ([]string, error) {
	query := `SELECT p.uid FROM photos p
		LEFT JOIN photo_phashes h ON h.photo_uid = p.uid
		WHERE h.photo_uid IS NULL
		ORDER BY p.uid`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list missing phashes: %w", err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate uids: %w", err)
	}
	return uids, nil
}

// SavePHash upserts the pHash + dHash for a photo. Called by the upload
// pipeline after a successful insert and by the backfill CLI when filling
// the row for a pre-existing photo.
func (r *PHashRepository) SavePHash(ctx context.Context, photoUID string, phash, dhash uint64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO photo_phashes (photo_uid, phash, dhash)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (photo_uid) DO UPDATE SET
		     phash = EXCLUDED.phash,
		     dhash = EXCLUDED.dhash,
		     created_at = NOW()`,
		photoUID,
		int64(phash), //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
		int64(dhash), //nolint:gosec // bit-pattern reinterpret cast: BIGINT round-trip
	)
	if err != nil {
		return fmt.Errorf("save phash: %w", err)
	}
	return nil
}

// DeletePHash removes a photo's pHash row. In production this is rarely
// needed (the FK cascades on photo delete); the method exists for tests
// and the future "rehash this photo" admin endpoint.
func (r *PHashRepository) DeletePHash(ctx context.Context, photoUID string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM photo_phashes WHERE photo_uid = $1`, photoUID); err != nil {
		return fmt.Errorf("delete phash: %w", err)
	}
	return nil
}

// Verify interface compliance.
var (
	_ database.PHashReader = (*PHashRepository)(nil)
	_ database.PHashWriter = (*PHashRepository)(nil)
)
