package database

import "testing"

// TestClampExportLimits pins the caps the vector feeds advertise. The handler
// mints its "was this page full?" cursor decision from these numbers, so a
// change here that the handler does not see would truncate an export — hence
// the single shared clamp, and hence this test.
func TestClampExportLimits(t *testing.T) {
	cases := []struct {
		name  string
		clamp func(int) int
		limit int
		want  int
	}{
		{"embeddings: zero means default", ClampEmbeddingExportLimit, 0, DefaultEmbeddingExportLimit},
		{"embeddings: negative means default", ClampEmbeddingExportLimit, -5, DefaultEmbeddingExportLimit},
		{"embeddings: in range is honoured", ClampEmbeddingExportLimit, 250, 250},
		{"embeddings: at the cap", ClampEmbeddingExportLimit, MaxEmbeddingExportLimit, MaxEmbeddingExportLimit},
		{"embeddings: over the cap is clamped", ClampEmbeddingExportLimit, 100_000, MaxEmbeddingExportLimit},
		{"faces: zero means default", ClampFaceExportLimit, 0, DefaultFaceExportLimit},
		{"faces: in range is honoured", ClampFaceExportLimit, 750, 750},
		{"faces: over the cap is clamped", ClampFaceExportLimit, 100_000, MaxFaceExportLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.clamp(tc.limit); got != tc.want {
				t.Errorf("clamp(%d) = %d, want %d", tc.limit, got, tc.want)
			}
		})
	}
}
