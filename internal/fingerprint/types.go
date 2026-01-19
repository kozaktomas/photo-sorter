package fingerprint

import "math"

// PhotoInfo contains comprehensive photo information including hashes
type PhotoInfo struct {
	// Identification
	UID          string `json:"uid"`
	OriginalName string `json:"original_name,omitempty"`
	FileName     string `json:"file_name,omitempty"`

	// Dimensions
	Width  int `json:"width"`
	Height int `json:"height"`

	// Dates
	TakenAt string `json:"taken_at,omitempty"`
	Year    int    `json:"year,omitempty"`
	Month   int    `json:"month,omitempty"`
	Day     int    `json:"day,omitempty"`

	// Location
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
	Country string  `json:"country,omitempty"`

	// Camera
	CameraModel string `json:"camera_model,omitempty"`

	// PhotoPrism metadata
	Hash        string `json:"photoprism_hash,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`

	// Perceptual hashes (computed)
	PHash string `json:"phash"`
	DHash string `json:"dhash"`

	// For internal use (comparison)
	PHashBits uint64 `json:"-"`
	DHashBits uint64 `json:"-"`

	// Image embeddings (from vision model)
	Embedding     []float32 `json:"embedding,omitempty"`
	EmbeddingTime float64   `json:"embedding_time_sec,omitempty"` // seconds

	// Timestamp
	ComputedAt string `json:"computed_at"`
}

// PhotoInfoBatch represents multiple photos for batch output
type PhotoInfoBatch struct {
	Photos []PhotoInfo `json:"photos"`
	Count  int         `json:"count"`
}

// CosineSimilarity computes the cosine similarity between two embedding vectors
// Returns a value between -1 and 1, where 1 means identical
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// EmbeddingSimilar returns true if two embeddings have cosine similarity above threshold
// A threshold of 0.9 typically indicates very similar images
func EmbeddingSimilar(a, b []float32, threshold float64) bool {
	return CosineSimilarity(a, b) >= threshold
}
