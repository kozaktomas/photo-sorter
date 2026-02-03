package database

import (
	"context"
)

// EmbeddingReader provides read-only access to image embeddings
type EmbeddingReader interface {
	// Get retrieves an embedding by photo UID, returns nil if not found
	Get(ctx context.Context, photoUID string) (*StoredEmbedding, error)
	// Has checks if an embedding exists for the given photo UID
	Has(ctx context.Context, photoUID string) (bool, error)
	// Count returns the total number of embeddings stored
	Count(ctx context.Context) (int, error)
	// FindSimilar finds the most similar embeddings using cosine distance
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredEmbedding, error)
	// FindSimilarWithDistance finds similar embeddings and returns distances
	FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredEmbedding, []float64, error)
}

// FaceReader provides read-only access to face embeddings
type FaceReader interface {
	// GetFaces retrieves all faces for a photo
	GetFaces(ctx context.Context, photoUID string) ([]StoredFace, error)
	// GetFacesBySubjectName retrieves all faces for a specific subject/person by name.
	// This is an optimized query that uses the cached subject_name field, eliminating
	// the need to query PhotoPrism for photos and then fetch faces individually.
	// Names are normalized before comparison (lowercase, no diacritics, dashes to spaces)
	// to handle format differences between slugs and display names (e.g., "jan-novak" matches "Jan Novák").
	GetFacesBySubjectName(ctx context.Context, subjectName string) ([]StoredFace, error)
	// HasFaces checks if faces have been computed for a photo
	HasFaces(ctx context.Context, photoUID string) (bool, error)
	// IsFacesProcessed checks if face detection has been run for a photo (regardless of whether faces were found)
	IsFacesProcessed(ctx context.Context, photoUID string) (bool, error)
	// Count returns the total number of faces stored
	Count(ctx context.Context) (int, error)
	// CountPhotos returns the number of distinct photos with faces
	CountPhotos(ctx context.Context) (int, error)
	// FindSimilar finds faces with similar embeddings using cosine distance
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredFace, error)
	// FindSimilarWithDistance finds similar faces and returns distances
	FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredFace, []float64, error)
	// GetUniquePhotoUIDs returns all unique photo UIDs that have faces
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
}

// FaceWriter provides write access to face data
type FaceWriter interface {
	FaceReader

	// SaveFaces stores multiple faces for a photo (replaces existing faces for that photo)
	SaveFaces(ctx context.Context, photoUID string, faces []StoredFace) error

	// MarkFacesProcessed marks a photo as having been processed for face detection
	MarkFacesProcessed(ctx context.Context, photoUID string, faceCount int) error

	// UpdateFaceMarker updates the cached marker data for a specific face.
	// Used to keep cache in sync when faces are assigned/unassigned via the UI.
	UpdateFaceMarker(ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) error

	// UpdateFacePhotoInfo updates the cached photo dimensions and file info for all faces of a photo.
	// Used during processing or backfill to populate cached PhotoPrism data.
	UpdateFacePhotoInfo(ctx context.Context, photoUID string, width, height, orientation int, fileUID string) error

	// DeleteFacesByPhoto removes all faces and faces_processed records for a photo.
	// Returns the deleted face IDs for HNSW cleanup.
	DeleteFacesByPhoto(ctx context.Context, photoUID string) ([]int64, error)
}

// EmbeddingWriter provides write access to image embeddings
type EmbeddingWriter interface {
	EmbeddingReader

	// DeleteEmbedding removes the embedding for a photo
	DeleteEmbedding(ctx context.Context, photoUID string) error

	// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
}

