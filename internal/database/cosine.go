package database

import "math"

// CosineDistance computes the cosine distance between two vectors
// Returns a value between 0 (identical) and 2 (opposite)
// Cosine distance = 1 - cosine similarity
func CosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 2.0 // Maximum distance for invalid input
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 2.0 // Maximum distance for zero vectors
	}

	similarity := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	// Clamp to [-1, 1] to handle floating point errors
	if similarity > 1 {
		similarity = 1
	}
	if similarity < -1 {
		similarity = -1
	}

	return 1 - similarity
}
