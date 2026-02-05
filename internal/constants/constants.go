// Package constants provides shared constants used across the codebase.
// Centralizing these values ensures consistency and makes them easier to modify.
package constants

// Pagination constants
const (
	// DefaultPageSize is the default number of items to fetch per API page
	DefaultPageSize = 1000

	// DefaultSearchLimit is the default limit for similarity search results per embedding
	DefaultSearchLimit = 1000
)

// Face matching constants
const (
	// IoUThreshold is the minimum Intersection over Union required to consider
	// a marker as matching a detected face
	IoUThreshold = 0.1

	// DefaultDistanceThreshold is the default maximum cosine distance for face matching
	// Lower values = stricter matching
	DefaultDistanceThreshold = 0.5

	// DefaultSimilarityThreshold is the default threshold for photo similarity search
	DefaultSimilarityThreshold = 0.3
)

// Processing constants
const (
	// WorkerPoolSize is the default number of parallel workers for face processing
	WorkerPoolSize = 20

	// GobSaveInterval is the number of photos processed before saving the GOB file
	GobSaveInterval = 50

	// MaxImageSize is the maximum dimension (width or height) for image processing
	MaxImageSize = 1920
)

// Label constants
const (
	// DefaultLabelConfidence is the minimum confidence score for AI-suggested labels
	DefaultLabelConfidence = 0.8
)

// Duplicate detection constants
const (
	// DefaultDuplicateThreshold is the default max cosine distance for duplicate detection
	DefaultDuplicateThreshold = 0.10
	// DefaultDuplicateLimit is the default max number of duplicate groups to return
	DefaultDuplicateLimit = 100
)

// Album suggestion constants
const (
	// DefaultSuggestAlbumThreshold is the default min cosine similarity for album suggestions
	DefaultSuggestAlbumThreshold = 0.30
	// DefaultSuggestAlbumTopK is the default number of top album suggestions per photo
	DefaultSuggestAlbumTopK = 3
	// MinAlbumPhotosForCentroid is the minimum number of photos in an album to compute a centroid
	MinAlbumPhotosForCentroid = 2
)
