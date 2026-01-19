package fingerprint

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestHammingDistance(t *testing.T) {
	tests := []struct {
		name     string
		hash1    uint64
		hash2    uint64
		expected int
	}{
		{"identical", 0x0, 0x0, 0},
		{"completely different", 0xFFFFFFFFFFFFFFFF, 0x0, 64},
		{"one bit different", 0x1, 0x0, 1},
		{"four bits different", 0xF, 0x0, 4},
		{"half different", 0xFFFFFFFF00000000, 0x0, 32},
		{"alternating", 0xAAAAAAAAAAAAAAAA, 0x5555555555555555, 64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := HammingDistance(tc.hash1, tc.hash2)
			if result != tc.expected {
				t.Errorf("HammingDistance(%x, %x) = %d; want %d",
					tc.hash1, tc.hash2, result, tc.expected)
			}
		})
	}
}

func TestSimilar(t *testing.T) {
	tests := []struct {
		name      string
		hash1     uint64
		hash2     uint64
		threshold int
		expected  bool
	}{
		{"identical with threshold 0", 0x0, 0x0, 0, true},
		{"identical with threshold 10", 0x0, 0x0, 10, true},
		{"9 bits different, threshold 10", 0x0, 0x1FF, 10, true},
		{"10 bits different, threshold 10", 0x0, 0x3FF, 10, true},
		{"11 bits different, threshold 10", 0x0, 0x7FF, 10, false},
		{"completely different, threshold 10", 0xFFFFFFFFFFFFFFFF, 0x0, 10, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Similar(tc.hash1, tc.hash2, tc.threshold)
			if result != tc.expected {
				t.Errorf("Similar(%x, %x, %d) = %v; want %v",
					tc.hash1, tc.hash2, tc.threshold, result, tc.expected)
			}
		})
	}
}

func TestComputeHashes(t *testing.T) {
	// Create a simple test image
	img := createTestImage(100, 100, color.White)
	imgData := encodeJPEG(img)

	result, err := ComputeHashes(imgData)
	if err != nil {
		t.Fatalf("ComputeHashes failed: %v", err)
	}

	// Check that hashes are not empty
	if result.PHash == "" {
		t.Error("PHash should not be empty")
	}
	if result.DHash == "" {
		t.Error("DHash should not be empty")
	}

	// Check hex format (16 characters for 64-bit hash)
	if len(result.PHash) != 16 {
		t.Errorf("PHash should be 16 hex characters, got %d: %s", len(result.PHash), result.PHash)
	}
	if len(result.DHash) != 16 {
		t.Errorf("DHash should be 16 hex characters, got %d: %s", len(result.DHash), result.DHash)
	}
}

func TestComputeHashesConsistency(t *testing.T) {
	// Same image should produce same hashes
	img := createTestImage(100, 100, color.RGBA{128, 128, 128, 255})
	imgData := encodeJPEG(img)

	result1, err := ComputeHashes(imgData)
	if err != nil {
		t.Fatalf("First ComputeHashes failed: %v", err)
	}

	result2, err := ComputeHashes(imgData)
	if err != nil {
		t.Fatalf("Second ComputeHashes failed: %v", err)
	}

	if result1.PHash != result2.PHash {
		t.Errorf("PHash should be consistent: %s vs %s", result1.PHash, result2.PHash)
	}
	if result1.DHash != result2.DHash {
		t.Errorf("DHash should be consistent: %s vs %s", result1.DHash, result2.DHash)
	}
}

func TestComputeHashesDifferentImages(t *testing.T) {
	// Very different images should have different hashes
	whiteImg := createTestImage(100, 100, color.White)
	blackImg := createTestImage(100, 100, color.Black)

	whiteData := encodeJPEG(whiteImg)
	blackData := encodeJPEG(blackImg)

	whiteResult, err := ComputeHashes(whiteData)
	if err != nil {
		t.Fatalf("ComputeHashes for white image failed: %v", err)
	}

	blackResult, err := ComputeHashes(blackData)
	if err != nil {
		t.Fatalf("ComputeHashes for black image failed: %v", err)
	}

	// Solid color images may have similar hashes due to lack of features
	// but they should still be computed without error
	t.Logf("White pHash: %s, Black pHash: %s", whiteResult.PHash, blackResult.PHash)
	t.Logf("White dHash: %s, Black dHash: %s", whiteResult.DHash, blackResult.DHash)
}

func TestComputeHashesGradient(t *testing.T) {
	// Create gradient image
	img := createGradientImage(100, 100)
	imgData := encodeJPEG(img)

	result, err := ComputeHashes(imgData)
	if err != nil {
		t.Fatalf("ComputeHashes failed: %v", err)
	}

	// Gradient should produce non-trivial hashes
	if result.PHashBits == 0 && result.DHashBits == 0 {
		t.Error("Gradient image should produce non-zero hashes")
	}

	t.Logf("Gradient pHash: %s (bits: %064b)", result.PHash, result.PHashBits)
	t.Logf("Gradient dHash: %s (bits: %064b)", result.DHash, result.DHashBits)
}

func TestComputeHashesInvalidImage(t *testing.T) {
	invalidData := []byte("not an image")

	_, err := ComputeHashes(invalidData)
	if err == nil {
		t.Error("ComputeHashes should fail for invalid image data")
	}
}

func TestResizeImage(t *testing.T) {
	// Create a 100x100 image
	img := createTestImage(100, 100, color.White)

	// Resize to 32x32
	resized := resizeImage(img, 32, 32)

	bounds := resized.Bounds()
	if bounds.Dx() != 32 || bounds.Dy() != 32 {
		t.Errorf("Resized image should be 32x32, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestToGrayscale(t *testing.T) {
	// Create a simple colored image
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255}) // Red
		}
	}

	gray := toGrayscale(img)

	// Check dimensions
	if len(gray) != 10 {
		t.Errorf("Grayscale width should be 10, got %d", len(gray))
	}
	if len(gray[0]) != 10 {
		t.Errorf("Grayscale height should be 10, got %d", len(gray[0]))
	}

	// Red should convert to approximately 0.299 * 255 = 76.245
	expectedLuma := 0.299 * 255
	tolerance := 1.0
	if gray[0][0] < expectedLuma-tolerance || gray[0][0] > expectedLuma+tolerance {
		t.Errorf("Red pixel luma should be ~%.2f, got %.2f", expectedLuma, gray[0][0])
	}
}

func TestComputeMedian(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"odd count", []float64{1, 2, 3, 4, 5}, 3},
		{"even count", []float64{1, 2, 3, 4}, 2.5},
		{"single value", []float64{42}, 42},
		{"unsorted", []float64{5, 1, 3, 2, 4}, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := computeMedian(tc.values)
			if result != tc.expected {
				t.Errorf("computeMedian(%v) = %f; want %f", tc.values, result, tc.expected)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
		delta    float64
	}{
		{"identical vectors", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0, 0.001},
		{"opposite vectors", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0, 0.001},
		{"orthogonal vectors", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0, 0.001},
		{"similar vectors", []float32{1, 1, 0}, []float32{1, 0, 0}, 0.707, 0.01},
		{"empty vectors", []float32{}, []float32{}, 0.0, 0.001},
		{"different lengths", []float32{1, 0}, []float32{1, 0, 0}, 0.0, 0.001},
		{"zero vector", []float32{0, 0, 0}, []float32{1, 0, 0}, 0.0, 0.001},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CosineSimilarity(tc.a, tc.b)
			if result < tc.expected-tc.delta || result > tc.expected+tc.delta {
				t.Errorf("CosineSimilarity(%v, %v) = %f; want %f (±%f)",
					tc.a, tc.b, result, tc.expected, tc.delta)
			}
		})
	}
}

func TestEmbeddingSimilar(t *testing.T) {
	tests := []struct {
		name      string
		a         []float32
		b         []float32
		threshold float64
		expected  bool
	}{
		{"identical at 0.9", []float32{1, 0, 0}, []float32{1, 0, 0}, 0.9, true},
		{"similar at 0.5", []float32{1, 1, 0}, []float32{1, 0, 0}, 0.5, true},
		{"not similar at 0.9", []float32{1, 1, 0}, []float32{1, 0, 0}, 0.9, false},
		{"orthogonal at 0.0", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := EmbeddingSimilar(tc.a, tc.b, tc.threshold)
			if result != tc.expected {
				t.Errorf("EmbeddingSimilar(%v, %v, %f) = %v; want %v",
					tc.a, tc.b, tc.threshold, result, tc.expected)
			}
		})
	}
}

// Helper functions

func createTestImage(width, height int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func createGradientImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			gray := uint8((x + y) * 255 / (width + height))
			img.Set(x, y, color.RGBA{gray, gray, gray, 255})
		}
	}
	return img
}

func encodeJPEG(img image.Image) []byte {
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}
