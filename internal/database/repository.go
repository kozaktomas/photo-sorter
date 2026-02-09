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
	// CountByUIDs returns the number of embeddings whose photo_uid is in the given list
	CountByUIDs(ctx context.Context, uids []string) (int, error)
	// FindSimilar finds the most similar embeddings using cosine distance
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredEmbedding, error)
	// FindSimilarWithDistance finds similar embeddings and returns distances
	FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredEmbedding, []float64, error)
	// GetUniquePhotoUIDs returns all unique photo UIDs that have embeddings
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
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
	// CountByUIDs returns the number of faces whose photo_uid is in the given list
	CountByUIDs(ctx context.Context, uids []string) (int, error)
	// CountPhotos returns the number of distinct photos with faces
	CountPhotos(ctx context.Context) (int, error)
	// CountPhotosByUIDs returns the number of distinct photos with faces whose photo_uid is in the given list
	CountPhotosByUIDs(ctx context.Context, uids []string) (int, error)
	// FindSimilar finds faces with similar embeddings using cosine distance
	FindSimilar(ctx context.Context, embedding []float32, limit int) ([]StoredFace, error)
	// FindSimilarWithDistance finds similar faces and returns distances
	FindSimilarWithDistance(ctx context.Context, embedding []float32, limit int, maxDistance float64) ([]StoredFace, []float64, error)
	// GetUniquePhotoUIDs returns all unique photo UIDs that have faces
	GetUniquePhotoUIDs(ctx context.Context) ([]string, error)
	// GetFacesWithMarkerUID returns all faces that have a non-empty marker_uid
	GetFacesWithMarkerUID(ctx context.Context) ([]StoredFace, error)
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
}

// EraEmbeddingReader provides read-only access to era embedding centroids
type EraEmbeddingReader interface {
	// GetEra retrieves an era embedding by slug, returns nil if not found
	GetEra(ctx context.Context, eraSlug string) (*StoredEraEmbedding, error)
	// GetAllEras retrieves all era embeddings
	GetAllEras(ctx context.Context) ([]StoredEraEmbedding, error)
	// CountEras returns the total number of era embeddings stored
	CountEras(ctx context.Context) (int, error)
}

// EraEmbeddingWriter provides write access to era embedding centroids
type EraEmbeddingWriter interface {
	EraEmbeddingReader
	// SaveEra stores an era embedding centroid (upsert)
	SaveEra(ctx context.Context, era StoredEraEmbedding) error
	// DeleteEra removes an era embedding by slug
	DeleteEra(ctx context.Context, eraSlug string) error
}

// BookReader provides read-only access to photo book data
type BookReader interface {
	GetBook(ctx context.Context, id string) (*PhotoBook, error)
	ListBooks(ctx context.Context) ([]PhotoBook, error)
	GetSections(ctx context.Context, bookID string) ([]BookSection, error)
	GetSectionPhotos(ctx context.Context, sectionID string) ([]SectionPhoto, error)
	CountSectionPhotos(ctx context.Context, sectionID string) (int, error)
	GetPages(ctx context.Context, bookID string) ([]BookPage, error)
	GetPage(ctx context.Context, pageID string) (*BookPage, error)
	GetPageSlots(ctx context.Context, pageID string) ([]PageSlot, error)
	GetAllPageSlots(ctx context.Context, bookID string) ([]PageSlot, error)
	GetPhotoBookMemberships(ctx context.Context, photoUID string) ([]PhotoBookMembership, error)
}

// BookWriter provides write access to photo book data
type BookWriter interface {
	BookReader
	CreateBook(ctx context.Context, book *PhotoBook) error
	UpdateBook(ctx context.Context, book *PhotoBook) error
	DeleteBook(ctx context.Context, id string) error
	CreateSection(ctx context.Context, section *BookSection) error
	UpdateSection(ctx context.Context, section *BookSection) error
	DeleteSection(ctx context.Context, id string) error
	ReorderSections(ctx context.Context, bookID string, sectionIDs []string) error
	AddSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error
	RemoveSectionPhotos(ctx context.Context, sectionID string, photoUIDs []string) error
	UpdateSectionPhoto(ctx context.Context, sectionID string, photoUID string, description string, note string) error
	CreatePage(ctx context.Context, page *BookPage) error
	UpdatePage(ctx context.Context, page *BookPage) error
	DeletePage(ctx context.Context, id string) error
	ReorderPages(ctx context.Context, bookID string, pageIDs []string) error
	AssignSlot(ctx context.Context, pageID string, slotIndex int, photoUID string) error
	ClearSlot(ctx context.Context, pageID string, slotIndex int) error
	SwapSlots(ctx context.Context, pageID string, slotA int, slotB int) error
}

