package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
)

// safeIntToInt32 converts int to int32 with clamping to prevent overflow.
func safeIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// FaceRepository provides pgvector-backed face storage.
type FaceRepository struct {
	pool *Pool
}

// NewFaceRepository creates a new PostgreSQL face repository.
func NewFaceRepository(pool *Pool) *FaceRepository {
	return &FaceRepository{pool: pool}
}

// GetFaces retrieves all faces for a photo.
func (r *FaceRepository) GetFaces(ctx context.Context, photoUID string) ([]database.StoredFace, error) {
	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		WHERE photo_uid = $1
		ORDER BY face_index
	`

	rows, err := r.pool.Query(ctx, query, photoUID)
	if err != nil {
		return nil, fmt.Errorf("query faces: %w", err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// GetFacesBySubjectName retrieves all faces for a specific subject/person by name.
// This queries the cached subject_name field directly, avoiding N individual photo queries.
// Names are normalized before comparison (lowercase, no diacritics, dashes to spaces).
// to handle format differences (e.g., "jan-novak" matches "Jan Novák").
func (r *FaceRepository) GetFacesBySubjectName(ctx context.Context, subjectName string) ([]database.StoredFace, error) {
	// Normalize input in Go (matches facematch.NormalizePersonName behavior).
	normalizedInput := facematch.NormalizePersonName(subjectName)

	// Use PostgreSQL LOWER + unaccent + REPLACE for comparison.
	// This matches the Go normalization: lowercase, remove diacritics, replace dashes with spaces.
	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		WHERE LOWER(REPLACE(unaccent(subject_name), '-', ' ')) = $1
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query, normalizedInput)
	if err != nil {
		return nil, fmt.Errorf("query faces by subject: %w", err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// HasFaces checks if faces have been computed for a photo.
func (r *FaceRepository) HasFaces(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM faces WHERE photo_uid = $1)", photoUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check faces exists: %w", err)
	}
	return exists, nil
}

// IsFacesProcessed checks if face detection has been run for a photo.
func (r *FaceRepository) IsFacesProcessed(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(
		ctx, "SELECT EXISTS(SELECT 1 FROM faces_processed WHERE photo_uid = $1)", photoUID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check faces processed: %w", err)
	}
	return exists, nil
}

// Count returns the total number of faces stored.
func (r *FaceRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM faces").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count faces: %w", err)
	}
	return count, nil
}

// CountByUIDs returns the number of faces whose photo_uid is in the given list.
func (r *FaceRepository) CountByUIDs(ctx context.Context, uids []string) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM faces WHERE photo_uid = ANY($1)", pq.Array(uids)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count faces by UIDs: %w", err)
	}
	return count, nil
}

// CountPhotos returns the number of distinct photos with faces.
func (r *FaceRepository) CountPhotos(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(DISTINCT photo_uid) FROM faces").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count photos: %w", err)
	}
	return count, nil
}

// CountPhotosByUIDs returns the number of distinct photos with faces whose photo_uid is in the given list.
func (r *FaceRepository) CountPhotosByUIDs(ctx context.Context, uids []string) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	var count int
	err := r.pool.QueryRow(
		ctx, "SELECT COUNT(DISTINCT photo_uid) FROM faces WHERE photo_uid = ANY($1)", pq.Array(uids),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count photos by UIDs: %w", err)
	}
	return count, nil
}

// GetUniquePhotoUIDs returns all unique photo UIDs that have faces.
func (r *FaceRepository) GetUniquePhotoUIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT DISTINCT photo_uid FROM faces ORDER BY photo_uid")
	if err != nil {
		return nil, fmt.Errorf("query unique photo UIDs: %w", err)
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

// GetFacesWithMarkerUID returns all faces that have a non-empty marker_uid.
func (r *FaceRepository) GetFacesWithMarkerUID(ctx context.Context) ([]database.StoredFace, error) {
	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		WHERE marker_uid IS NOT NULL AND marker_uid != ''
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query faces with marker UID: %w", err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// CountProcessed returns the number of photos that have been processed for face detection.
func (r *FaceRepository) CountProcessed(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM faces_processed").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count processed: %w", err)
	}
	return count, nil
}

// FindSimilar finds faces with similar embeddings using cosine distance.
func (r *FaceRepository) FindSimilar(
	ctx context.Context, embedding []float32, limit int,
) ([]database.StoredFace, error) {
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch)); err != nil {
		return nil, fmt.Errorf("set ef_search: %w", err)
	}

	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`

	vec := pgvector.NewVector(embedding)
	rows, err := tx.QueryContext(ctx, query, vec, limit)
	if err != nil {
		return nil, fmt.Errorf("query similar faces: %w", err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// FindSimilarWithDistance returns the closest faces whose cosine
// distance to the query vector is strictly less than maxDistance, ordered
// by distance ascending, capped at limit.
func (r *FaceRepository) FindSimilarWithDistance(
	ctx context.Context, embedding []float32, limit int, maxDistance float64,
) ([]database.StoredFace, []float64, error) {
	tx, err := r.pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", hnswEfSearch)); err != nil {
		return nil, nil, fmt.Errorf("set ef_search: %w", err)
	}

	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid,
		       embedding <=> $1::vector AS distance
		FROM faces
		WHERE embedding <=> $1::vector < $2
		ORDER BY distance
		LIMIT $3
	`

	vec := pgvector.NewVector(embedding)
	rows, err := tx.QueryContext(ctx, query, vec, maxDistance, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("query similar faces: %w", err)
	}
	defer rows.Close()

	var faces []database.StoredFace
	var distances []float64

	for rows.Next() {
		face, dist, err := scanFaceWithDistance(rows)
		if err != nil {
			return nil, nil, err
		}
		faces = append(faces, face)
		distances = append(distances, dist)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate faces: %w", err)
	}

	return faces, distances, nil
}

// faceNullableFields holds nullable SQL parameters extracted from a StoredFace.
type faceNullableFields struct {
	markerUID   sql.NullString
	subjectUID  sql.NullString
	subjectName sql.NullString
	fileUID     sql.NullString
	photoWidth  sql.NullInt32
	photoHeight sql.NullInt32
	orientation sql.NullInt32
}

// extractNullableFields converts optional face fields to SQL nullable types.
func extractNullableFields(face *database.StoredFace) faceNullableFields {
	var f faceNullableFields
	if face.MarkerUID != "" {
		f.markerUID = sql.NullString{String: face.MarkerUID, Valid: true}
	}
	if face.SubjectUID != "" {
		f.subjectUID = sql.NullString{String: face.SubjectUID, Valid: true}
	}
	if face.SubjectName != "" {
		f.subjectName = sql.NullString{String: face.SubjectName, Valid: true}
	}
	if face.FileUID != "" {
		f.fileUID = sql.NullString{String: face.FileUID, Valid: true}
	}
	if face.PhotoWidth > 0 {
		f.photoWidth = sql.NullInt32{Int32: safeIntToInt32(face.PhotoWidth), Valid: true}
	}
	if face.PhotoHeight > 0 {
		f.photoHeight = sql.NullInt32{Int32: safeIntToInt32(face.PhotoHeight), Valid: true}
	}
	if face.Orientation > 0 {
		f.orientation = sql.NullInt32{Int32: safeIntToInt32(face.Orientation), Valid: true}
	}
	return f
}

// SaveFaces stores multiple faces for a photo, replacing any existing faces for that photo.
func (r *FaceRepository) SaveFaces(ctx context.Context, photoUID string, faces []database.StoredFace) error {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID); err != nil {
		return fmt.Errorf("delete existing faces: %w", err)
	}

	if len(faces) == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
		return nil
	}

	if _, err := insertFacesReturningIDs(ctx, tx, photoUID, faces); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// insertFacesReturningIDs inserts faces into the database and returns them with assigned IDs.
func insertFacesReturningIDs(
	ctx context.Context, tx *sql.Tx, photoUID string, faces []database.StoredFace,
) ([]database.StoredFace, error) {
	insertedFaces := make([]database.StoredFace, 0, len(faces))

	for i := range faces {
		face := &faces[i]
		vec := pgvector.NewVector(face.Embedding)
		bbox := pq.Array(face.BBox)
		nf := extractNullableFields(face)

		var newID int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO faces (photo_uid, face_index, embedding, bbox, det_score, model, dim,
			                   marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid)
			VALUES ($1, $2, $3::vector, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING id
		`,
			photoUID,
			face.FaceIndex,
			vec,
			bbox,
			face.DetScore,
			face.Model,
			face.Dim,
			nf.markerUID,
			nf.subjectUID,
			nf.subjectName,
			nf.photoWidth,
			nf.photoHeight,
			nf.orientation,
			nf.fileUID,
		).Scan(&newID)
		if err != nil {
			return nil, fmt.Errorf("insert face %d: %w", face.FaceIndex, err)
		}

		newFace := *face
		newFace.ID = newID
		newFace.PhotoUID = photoUID
		insertedFaces = append(insertedFaces, newFace)
	}

	return insertedFaces, nil
}

// MarkFacesProcessed marks a photo as having been processed for face detection.
func (r *FaceRepository) MarkFacesProcessed(ctx context.Context, photoUID string, faceCount int) error {
	query := `
		INSERT INTO faces_processed (photo_uid, face_count)
		VALUES ($1, $2)
		ON CONFLICT (photo_uid) DO UPDATE SET
			face_count = EXCLUDED.face_count,
			created_at = NOW()
	`

	if _, err := r.pool.Exec(ctx, query, photoUID, faceCount); err != nil {
		return fmt.Errorf("mark faces processed: %w", err)
	}
	return nil
}

// UpdateFaceMarker updates the cached marker data for a specific face.
func (r *FaceRepository) UpdateFaceMarker(
	ctx context.Context, photoUID string, faceIndex int,
	markerUID, subjectUID, subjectName string,
) error {
	query := `
		UPDATE faces SET
			marker_uid = $1,
			subject_uid = $2,
			subject_name = $3
		WHERE photo_uid = $4 AND face_index = $5
	`

	var mUID, sUID, sName sql.NullString
	if markerUID != "" {
		mUID = sql.NullString{String: markerUID, Valid: true}
	}
	if subjectUID != "" {
		sUID = sql.NullString{String: subjectUID, Valid: true}
	}
	if subjectName != "" {
		sName = sql.NullString{String: subjectName, Valid: true}
	}

	if _, err := r.pool.Exec(ctx, query, mUID, sUID, sName, photoUID, faceIndex); err != nil {
		return fmt.Errorf("update face marker: %w", err)
	}
	return nil
}

// UpdateFacePhotoInfo updates the cached photo dimensions and file info for all faces of a photo.
func (r *FaceRepository) UpdateFacePhotoInfo(
	ctx context.Context, photoUID string,
	width, height, orientation int, fileUID string,
) error {
	query := `
		UPDATE faces SET
			photo_width = $1,
			photo_height = $2,
			orientation = $3,
			file_uid = $4
		WHERE photo_uid = $5
	`

	if _, err := r.pool.Exec(ctx, query, width, height, orientation, fileUID, photoUID); err != nil {
		return fmt.Errorf("update face photo info: %w", err)
	}
	return nil
}

// SaveFacesBatch saves faces for multiple photos in a single transaction.
//
//nolint:funlen // Batch operation with transaction management.
func (r *FaceRepository) SaveFacesBatch(ctx context.Context, facesByPhoto map[string][]database.StoredFace) error {
	if len(facesByPhoto) == 0 {
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Collect all photo UIDs.
	photoUIDs := make([]string, 0, len(facesByPhoto))
	for uid := range facesByPhoto {
		photoUIDs = append(photoUIDs, uid)
	}

	// Delete existing faces for all photos.
	if _, err := tx.ExecContext(ctx, "DELETE FROM faces WHERE photo_uid = ANY($1)", pq.Array(photoUIDs)); err != nil {
		return fmt.Errorf("delete existing faces: %w", err)
	}

	// Prepare insert statement.
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO faces (photo_uid, face_index, embedding, bbox, det_score, model, dim,
		                   marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid)
		VALUES ($1, $2, $3::vector, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for photoUID, faces := range facesByPhoto {
		for j := range faces {
			face := &faces[j]
			vec := pgvector.NewVector(face.Embedding)
			bbox := pq.Array(face.BBox)
			nf := extractNullableFields(face)

			if _, err := stmt.ExecContext(ctx,
				photoUID,
				face.FaceIndex,
				vec,
				bbox,
				face.DetScore,
				face.Model,
				face.Dim,
				nf.markerUID,
				nf.subjectUID,
				nf.subjectName,
				nf.photoWidth,
				nf.photoHeight,
				nf.orientation,
				nf.fileUID,
			); err != nil {
				return fmt.Errorf("insert face %s/%d: %w", photoUID, face.FaceIndex, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// MarkFacesProcessedBatch marks multiple photos as processed in a single transaction.
func (r *FaceRepository) MarkFacesProcessedBatch(ctx context.Context, records []database.FaceProcessedRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO faces_processed (photo_uid, face_count)
		VALUES ($1, $2)
		ON CONFLICT (photo_uid) DO UPDATE SET
			face_count = EXCLUDED.face_count,
			created_at = NOW()
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, rec := range records {
		if _, err := stmt.ExecContext(ctx, rec.PhotoUID, rec.FaceCount); err != nil {
			return fmt.Errorf("insert faces_processed %s: %w", rec.PhotoUID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// scanFaceRow scans a single row into a StoredFace, with optional extra scan destinations.
// appended after the standard 16 face columns (e.g., a distance column).
func scanFaceRow(scanner interface{ Scan(...any) error }, extraDest ...any) (database.StoredFace, error) {
	var face database.StoredFace
	var vec pgvector.Vector
	var bbox pq.Float64Array
	var markerUID, subjectUID, subjectName, fileUID sql.NullString
	var photoWidth, photoHeight, orientation sql.NullInt32
	var model sql.NullString

	dest := make([]any, 0, 16+len(extraDest))
	dest = append(dest,
		&face.ID,
		&face.PhotoUID,
		&face.FaceIndex,
		&vec,
		&bbox,
		&face.DetScore,
		&model,
		&face.Dim,
		&face.CreatedAt,
		&markerUID,
		&subjectUID,
		&subjectName,
		&photoWidth,
		&photoHeight,
		&orientation,
		&fileUID,
	)
	dest = append(dest, extraDest...)

	if err := scanner.Scan(dest...); err != nil {
		return face, fmt.Errorf("scan face: %w", err)
	}

	face.Embedding = vec.Slice()
	face.BBox = []float64(bbox)
	if model.Valid {
		face.Model = model.String
	}
	if markerUID.Valid {
		face.MarkerUID = markerUID.String
	}
	if subjectUID.Valid {
		face.SubjectUID = subjectUID.String
	}
	if subjectName.Valid {
		face.SubjectName = subjectName.String
	}
	if fileUID.Valid {
		face.FileUID = fileUID.String
	}
	if photoWidth.Valid {
		face.PhotoWidth = int(photoWidth.Int32)
	}
	if photoHeight.Valid {
		face.PhotoHeight = int(photoHeight.Int32)
	}
	if orientation.Valid {
		face.Orientation = int(orientation.Int32)
	}

	return face, nil
}

func scanFaces(rows *sql.Rows) ([]database.StoredFace, error) {
	var faces []database.StoredFace
	for rows.Next() {
		face, err := scanFaceRow(rows)
		if err != nil {
			return nil, err
		}
		faces = append(faces, face)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate faces: %w", err)
	}
	return faces, nil
}

func scanFaceWithDistance(rows *sql.Rows) (database.StoredFace, float64, error) {
	var dist float64
	face, err := scanFaceRow(rows, &dist)
	return face, dist, err
}

// GetAllFaces retrieves all faces from the database.
func (r *FaceRepository) GetAllFaces(ctx context.Context) ([]database.StoredFace, error) {
	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query all faces: %w", err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// ListFacesAfter returns the faces whose id is strictly greater than afterID,
// ordered by id, capped at the export limit. It backs the keyset-paginated
// migration feed (GET /api/v1/faces).
//
// id is the table's BIGSERIAL primary key, so the ordering is total and the
// walk is exact — pass afterID = 0 to start, then the id of the last row of
// each page to continue. GetAllFaces returns the same rows in the same order
// but loads all 112k of them (≈230 MB of float32) into RAM; this is the form
// an HTTP feed can serve.
func (r *FaceRepository) ListFacesAfter(
	ctx context.Context, afterID int64, limit int,
) ([]database.StoredFace, error) {
	query := `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
		       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation, file_uid
		FROM faces
		WHERE id > $1
		ORDER BY id
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, afterID, database.ClampFaceExportLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query faces after %d: %w", afterID, err)
	}
	defer rows.Close()

	return scanFaces(rows)
}

// DeleteFacesByPhoto removes all faces and faces_processed records for a
// photo. pgvector keeps the embedding index in sync automatically.
// The returned slice is the list of deleted face IDs, retained on the
// interface signature for callers that want to log or audit the removed
// rows.
func (r *FaceRepository) DeleteFacesByPhoto(ctx context.Context, photoUID string) ([]int64, error) {
	tx, err := r.pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	faceIDs, err := scanFaceIDs(tx, ctx, photoUID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID); err != nil {
		return nil, fmt.Errorf("delete faces: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM faces_processed WHERE photo_uid = $1", photoUID); err != nil {
		return nil, fmt.Errorf("delete faces_processed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return faceIDs, nil
}

// scanFaceIDs reads face IDs from a query and properly closes the rows.
func scanFaceIDs(tx *sql.Tx, ctx context.Context, photoUID string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM faces WHERE photo_uid = $1", photoUID)
	if err != nil {
		return nil, fmt.Errorf("query face IDs: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan face ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate face IDs: %w", err)
	}
	return ids, nil
}

// GetPhotoUIDsWithSubjectName returns photo UIDs (from the given list) that have at least one.
// face assigned to the given subject name. Names are normalized for comparison.
func (r *FaceRepository) GetPhotoUIDsWithSubjectName(
	ctx context.Context, photoUIDs []string, subjectName string,
) (map[string]bool, error) {
	if len(photoUIDs) == 0 {
		return make(map[string]bool), nil
	}

	normalizedInput := facematch.NormalizePersonName(subjectName)

	query := `
		SELECT DISTINCT photo_uid FROM faces
		WHERE photo_uid = ANY($1)
		AND LOWER(REPLACE(unaccent(subject_name), '-', ' ')) = $2
	`

	rows, err := r.pool.Query(ctx, query, pq.Array(photoUIDs), normalizedInput)
	if err != nil {
		return nil, fmt.Errorf("query photo UIDs with subject: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan photo UID: %w", err)
		}
		result[uid] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo UIDs: %w", err)
	}
	return result, nil
}

// Compile-time interface checks.
var (
	_ database.FaceReader       = (*FaceRepository)(nil)
	_ database.FaceWriter       = (*FaceRepository)(nil)
	_ database.FaceExportReader = (*FaceRepository)(nil)
)
