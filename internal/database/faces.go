package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// StoredFace represents a face embedding stored in the database
type StoredFace struct {
	ID        int64
	PhotoUID  string
	FaceIndex int
	Embedding []float32
	BBox      []float64 // [x1, y1, x2, y2]
	DetScore  float64
	Model     string
	Dim       int
	CreatedAt time.Time
}

// FaceRepository handles database operations for face embeddings
type FaceRepository struct {
	pool *pgxpool.Pool
}

// NewFaceRepository creates a new face repository
func NewFaceRepository(pool *pgxpool.Pool) *FaceRepository {
	return &FaceRepository{pool: pool}
}

// SaveFaces stores multiple faces for a photo (replaces existing faces for that photo)
func (r *FaceRepository) SaveFaces(ctx context.Context, photoUID string, faces []StoredFace) error {
	// Start transaction
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Delete existing faces for this photo
	_, err = tx.Exec(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID)
	if err != nil {
		return err
	}

	// Insert new faces
	for _, face := range faces {
		vec := pgvector.NewVector(face.Embedding)
		_, err = tx.Exec(ctx, `
			INSERT INTO faces (photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		`, photoUID, face.FaceIndex, vec, face.BBox, face.DetScore, face.Model, face.Dim)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetFaces retrieves all faces for a photo
func (r *FaceRepository) GetFaces(ctx context.Context, photoUID string) ([]StoredFace, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at
		FROM faces
		WHERE photo_uid = $1
		ORDER BY face_index
	`, photoUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var faces []StoredFace
	for rows.Next() {
		var face StoredFace
		var vec pgvector.Vector
		if err := rows.Scan(&face.ID, &face.PhotoUID, &face.FaceIndex, &vec, &face.BBox, &face.DetScore, &face.Model, &face.Dim, &face.CreatedAt); err != nil {
			return nil, err
		}
		face.Embedding = vec.Slice()
		faces = append(faces, face)
	}

	return faces, rows.Err()
}

// HasFaces checks if faces have been computed for a photo
func (r *FaceRepository) HasFaces(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM faces WHERE photo_uid = $1)
	`, photoUID).Scan(&exists)
	return exists, err
}

// Count returns the total number of faces stored
func (r *FaceRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM faces").Scan(&count)
	return count, err
}

// CountPhotos returns the number of distinct photos with faces
func (r *FaceRepository) CountPhotos(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(DISTINCT photo_uid) FROM faces").Scan(&count)
	return count, err
}

// FindSimilar finds faces with similar embeddings using cosine distance
func (r *FaceRepository) FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredFace, error) {
	vec := pgvector.NewVector(embedding)

	rows, err := r.pool.Query(ctx, `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at
		FROM faces
		ORDER BY embedding <=> $1
		LIMIT $2
	`, vec, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var faces []StoredFace
	for rows.Next() {
		var face StoredFace
		var v pgvector.Vector
		if err := rows.Scan(&face.ID, &face.PhotoUID, &face.FaceIndex, &v, &face.BBox, &face.DetScore, &face.Model, &face.Dim, &face.CreatedAt); err != nil {
			return nil, err
		}
		face.Embedding = v.Slice()
		faces = append(faces, face)
	}

	return faces, rows.Err()
}

// FindSimilarWithDistance finds faces with similar embeddings and returns distances
func (r *FaceRepository) FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredFace, []float64, error) {
	vec := pgvector.NewVector(embedding)

	rows, err := r.pool.Query(ctx, `
		SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at, embedding <=> $1 as distance
		FROM faces
		WHERE embedding <=> $1 < $3
		ORDER BY embedding <=> $1
		LIMIT $2
	`, vec, limit, maxDistance)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var faces []StoredFace
	var distances []float64
	for rows.Next() {
		var face StoredFace
		var v pgvector.Vector
		var distance float64
		if err := rows.Scan(&face.ID, &face.PhotoUID, &face.FaceIndex, &v, &face.BBox, &face.DetScore, &face.Model, &face.Dim, &face.CreatedAt, &distance); err != nil {
			return nil, nil, err
		}
		face.Embedding = v.Slice()
		faces = append(faces, face)
		distances = append(distances, distance)
	}

	return faces, distances, rows.Err()
}
