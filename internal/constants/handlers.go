// Package constants provides shared constants used across the codebase.
package constants

// Handler pagination constants.
const (
	// DefaultPhotoQuality is the minimum quality score for photo queries in the web UI.
	// Matches PhotoPrism's default (quality >= 3 hides review/low-quality photos).
	DefaultPhotoQuality = 3

	// DefaultHandlerPageSize is the page size for paginated handler endpoints.
	DefaultHandlerPageSize = 100

	// MaxPhotosPerFetch is the maximum number of photos to fetch in a single operation.
	MaxPhotosPerFetch = 10000

	// DefaultLabelCount is the default number of labels to fetch.
	DefaultLabelCount = 1000

	// DefaultSubjectCount is the default number of subjects to fetch.
	DefaultSubjectCount = 1000

	// DefaultSimilarLimit is the default limit for similarity search results.
	DefaultSimilarLimit = 50

	// DefaultConcurrency is the default number of parallel workers.
	DefaultConcurrency = 5
)

// Event channel constants.
const (
	// EventChannelBuffer is the buffer size for event channels.
	EventChannelBuffer = 100
)

// Face suggestion constants.
const (
	// DefaultFaceSuggestionLimit is the default number of face suggestions to return.
	DefaultFaceSuggestionLimit = 5

	// DefaultFaceSimilarSearchLimit is the limit for finding similar faces.
	DefaultFaceSimilarSearchLimit = 500
)

// File upload constants.
const (
	// MaxUploadSize is the maximum file upload size in bytes (100MB).
	MaxUploadSize = 100 << 20
)
