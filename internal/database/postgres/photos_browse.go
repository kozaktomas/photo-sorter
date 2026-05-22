package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// histogramBucketMonth / histogramBucketYear are the two date_trunc units
// the Histogram method accepts. Defined as constants because each value
// appears in switch cases, error messages, and SQL fragments.
const (
	histogramBucketMonth = "month"
	histogramBucketYear  = "year"
)

// bucketTrunc maps the public bucket name onto the SQL date_trunc unit.
// Returns ok=false when the caller passes an unsupported bucket.
func bucketTrunc(bucket string) (string, bool) {
	switch bucket {
	case histogramBucketMonth, histogramBucketYear:
		return bucket, true
	default:
		return "", false
	}
}

// bucketEnd returns the exclusive upper bound of a bucket starting at start
// for the given bucket unit. Year buckets span Jan..Jan, month buckets span
// 1st..1st-of-next-month — both done via AddDate so leap years and Dec→Jan
// rollover happen for free.
func bucketEnd(start time.Time, bucket string) time.Time {
	if bucket == histogramBucketYear {
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0)
}

// Histogram returns the photo-count histogram across the filter's matching
// photos.
//
// The same WHERE clause used by ListPhotos is reused via buildPhotoFilter so
// the histogram, the geo-points list, and the photo list all see exactly
// the same population — switching one filter on the page must update all
// three views in lockstep. Three aggregate counts come back in a single
// trip: the histogram itself, the total matching rows, and the breakdown
// of rows missing taken_at vs missing lat/lng. We deliberately do NOT fill
// empty months in the middle of the range; the frontend can do that step
// when it lays out a continuous chart axis, and skipping it keeps the
// payload small for sparse libraries.
func (r *PhotoRepository) Histogram(
	ctx context.Context, filter database.PhotoFilter, bucket string,
) (database.HistogramResult, error) {
	trunc, ok := bucketTrunc(bucket)
	if !ok {
		return database.HistogramResult{}, fmt.Errorf("invalid bucket %q (want month or year)", bucket)
	}
	where, _, args := buildPhotoFilter(filter)

	res := database.HistogramResult{}
	if err := r.countAggregates(ctx, where, args, &res); err != nil {
		return database.HistogramResult{}, err
	}
	buckets, err := r.histogramBuckets(ctx, where, args, trunc)
	if err != nil {
		return database.HistogramResult{}, err
	}
	res.Buckets = buckets
	return res, nil
}

// countAggregates fills Total, NoDateCount, NoGPSCount via a single
// SELECT. We use FILTER (WHERE ...) so the planner only walks the rows
// once.
func (r *PhotoRepository) countAggregates(
	ctx context.Context, where string, args []any, out *database.HistogramResult,
) error {
	sql := `SELECT
		COUNT(*),
		COUNT(*) FILTER (WHERE p.taken_at IS NULL),
		COUNT(*) FILTER (WHERE p.lat IS NULL OR p.lng IS NULL)
	FROM photos p` + where
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(
		&out.Total, &out.NoDateCount, &out.NoGPSCount,
	); err != nil {
		return fmt.Errorf("histogram counts: %w", err)
	}
	return nil
}

// histogramBuckets returns the per-bucket counts for photos with a non-NULL
// taken_at. Photos with NULL taken_at are reported via NoDateCount instead
// of being lumped into a bogus bucket.
func (r *PhotoRepository) histogramBuckets(
	ctx context.Context, where string, args []any, trunc string,
) ([]database.HistogramBucket, error) {
	clause := where
	if clause == "" {
		clause = " WHERE p.taken_at IS NOT NULL"
	} else {
		clause += " AND p.taken_at IS NOT NULL"
	}
	sql := fmt.Sprintf(`SELECT date_trunc('%s', p.taken_at) AS bucket, COUNT(*)
		FROM photos p%s
		GROUP BY bucket
		ORDER BY bucket ASC`, trunc, clause)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("histogram buckets: %w", err)
	}
	defer rows.Close()

	var buckets []database.HistogramBucket
	for rows.Next() {
		var start time.Time
		var count int
		if err := rows.Scan(&start, &count); err != nil {
			return nil, fmt.Errorf("scan histogram bucket: %w", err)
		}
		buckets = append(buckets, database.HistogramBucket{
			Start: start.UTC(),
			End:   bucketEnd(start.UTC(), trunc),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate histogram buckets: %w", err)
	}
	return buckets, nil
}

// ListGeoPoints returns one row per photo with non-NULL lat/lng that passes
// the filter. The frontend uses these for client-side clustering on the map.
// When maxPoints > 0 the query asks the database for maxPoints+1 rows and
// trims the extra row in Go — that's enough to flag truncation without
// running a second COUNT(*) query.
func (r *PhotoRepository) ListGeoPoints(
	ctx context.Context, filter database.PhotoFilter, maxPoints int,
) ([]database.GeoPoint, bool, error) {
	where, _, args := buildPhotoFilter(filter)
	clause := where
	if clause == "" {
		clause = " WHERE p.lat IS NOT NULL AND p.lng IS NOT NULL"
	} else {
		clause += " AND p.lat IS NOT NULL AND p.lng IS NOT NULL"
	}

	sql := "SELECT p.uid, p.lat, p.lng FROM photos p" + clause + " ORDER BY p.uid"
	if maxPoints > 0 {
		sql += fmt.Sprintf(" LIMIT %d", maxPoints+1)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, fmt.Errorf("list geo points: %w", err)
	}
	defer rows.Close()

	var points []database.GeoPoint
	for rows.Next() {
		var p database.GeoPoint
		if err := rows.Scan(&p.PhotoUID, &p.Lat, &p.Lng); err != nil {
			return nil, false, fmt.Errorf("scan geo point: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate geo points: %w", err)
	}

	truncated := false
	if maxPoints > 0 && len(points) > maxPoints {
		points = points[:maxPoints]
		truncated = true
	}
	return points, truncated, nil
}
