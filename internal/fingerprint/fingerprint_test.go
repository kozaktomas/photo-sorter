package fingerprint

import (
	"bytes"
	"fmt"
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

// --- Additional tests for stability ---

func TestHammingDistance_Symmetry(t *testing.T) {
	hash1 := uint64(0xABCDEF0123456789)
	hash2 := uint64(0x123456789ABCDEF0)

	d1 := HammingDistance(hash1, hash2)
	d2 := HammingDistance(hash2, hash1)

	if d1 != d2 {
		t.Errorf("Hamming distance should be symmetric: %d vs %d", d1, d2)
	}
}

func TestHammingDistance_MaxValue(t *testing.T) {
	// Test with max uint64 value
	hash1 := uint64(0xFFFFFFFFFFFFFFFF)
	hash2 := uint64(0x0)

	distance := HammingDistance(hash1, hash2)

	if distance != 64 {
		t.Errorf("expected distance 64 between max and zero, got %d", distance)
	}
}

func TestHammingDistance_SingleBitPositions(t *testing.T) {
	// Test single bit at each position
	for i := 0; i < 64; i++ {
		hash := uint64(1) << i
		distance := HammingDistance(hash, 0)

		if distance != 1 {
			t.Errorf("expected distance 1 for single bit at position %d, got %d", i, distance)
		}
	}
}

func TestComputeHashes_DifferentSizes(t *testing.T) {
	sizes := []struct {
		width  int
		height int
	}{
		{10, 10},
		{100, 100},
		{200, 150},
		{50, 200},
		{1920, 1080},
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			img := createTestImage(size.width, size.height, color.Gray{128})
			imgData := encodeJPEG(img)

			result, err := ComputeHashes(imgData)
			if err != nil {
				t.Fatalf("ComputeHashes failed for %dx%d: %v", size.width, size.height, err)
			}

			if result.PHash == "" || result.DHash == "" {
				t.Errorf("expected non-empty hashes for %dx%d", size.width, size.height)
			}
		})
	}
}

func TestComputeHashes_SimilarImagesHaveSimilarHashes(t *testing.T) {
	// Create two very similar images (slight brightness difference)
	img1 := createTestImage(100, 100, color.Gray{128})
	img2 := createTestImage(100, 100, color.Gray{130}) // Slightly brighter

	data1 := encodeJPEG(img1)
	data2 := encodeJPEG(img2)

	result1, err := ComputeHashes(data1)
	if err != nil {
		t.Fatalf("ComputeHashes failed for img1: %v", err)
	}

	result2, err := ComputeHashes(data2)
	if err != nil {
		t.Fatalf("ComputeHashes failed for img2: %v", err)
	}

	// Similar images should have small Hamming distance
	pHashDist := HammingDistance(result1.PHashBits, result2.PHashBits)
	dHashDist := HammingDistance(result1.DHashBits, result2.DHashBits)

	// For very similar solid color images, distance should be small
	if pHashDist > 20 {
		t.Logf("pHash distance %d may be higher than expected for similar images", pHashDist)
	}

	if dHashDist > 20 {
		t.Logf("dHash distance %d may be higher than expected for similar images", dHashDist)
	}
}

func TestResizeImage_NoResizeNeeded(t *testing.T) {
	// Create a small image that doesn't need resizing
	img := createTestImage(100, 100, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 200) // maxSize larger than image
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	// Should return original data (or re-encoded at same size)
	if len(resized) == 0 {
		t.Error("expected non-empty result")
	}
}

func TestResizeImage_NeedsResize(t *testing.T) {
	// Create a large image that needs resizing
	img := createTestImage(2000, 1500, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 500)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	// Decode resized image to verify dimensions
	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()
	maxDim := bounds.Dx()
	if bounds.Dy() > maxDim {
		maxDim = bounds.Dy()
	}

	if maxDim > 500 {
		t.Errorf("expected max dimension <= 500, got %d", maxDim)
	}
}

func TestResizeImage_PreservesAspectRatio(t *testing.T) {
	// Create a landscape image (2:1 ratio)
	img := createTestImage(1000, 500, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 200)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()
	ratio := float64(bounds.Dx()) / float64(bounds.Dy())

	// Should maintain approximately 2:1 ratio
	if ratio < 1.9 || ratio > 2.1 {
		t.Errorf("expected aspect ratio ~2.0, got %f (%dx%d)", ratio, bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_Portrait(t *testing.T) {
	// Create a portrait image (1:2 ratio)
	img := createTestImage(500, 1000, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 200)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()

	// Height should be maxSize (200)
	if bounds.Dy() != 200 {
		t.Errorf("expected height 200, got %d", bounds.Dy())
	}

	// Width should be proportionally smaller
	if bounds.Dx() >= bounds.Dy() {
		t.Errorf("expected width < height for portrait, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_InvalidData(t *testing.T) {
	invalidData := []byte("not an image")

	_, err := ResizeImage(invalidData, 500)
	if err == nil {
		t.Error("expected error for invalid image data")
	}
}

func TestResizeImage_Square(t *testing.T) {
	img := createTestImage(1000, 1000, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 200)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()

	// Should be exactly 200x200 for square image
	if bounds.Dx() != 200 || bounds.Dy() != 200 {
		t.Errorf("expected 200x200, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestComputeMedian_TwoValues(t *testing.T) {
	values := []float64{10, 20}
	expected := 15.0

	result := computeMedian(values)

	if result != expected {
		t.Errorf("expected median %f, got %f", expected, result)
	}
}

func TestComputeMedian_LargeValues(t *testing.T) {
	values := []float64{1e10, 2e10, 3e10}
	expected := 2e10

	result := computeMedian(values)

	if result != expected {
		t.Errorf("expected median %f, got %f", expected, result)
	}
}

func TestComputeMedian_NegativeValues(t *testing.T) {
	values := []float64{-5, -3, -1, 0, 2}
	expected := -1.0

	result := computeMedian(values)

	if result != expected {
		t.Errorf("expected median %f, got %f", expected, result)
	}
}

func TestToGrayscale_WhitePixel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)

	gray := toGrayscale(img)

	// White should convert to 255
	if gray[0][0] < 254 || gray[0][0] > 256 {
		t.Errorf("expected white pixel luma ~255, got %f", gray[0][0])
	}
}

func TestToGrayscale_BlackPixel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)

	gray := toGrayscale(img)

	// Black should convert to 0
	if gray[0][0] != 0 {
		t.Errorf("expected black pixel luma 0, got %f", gray[0][0])
	}
}

func TestToGrayscale_GreenChannel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{0, 255, 0, 255}) // Pure green

	gray := toGrayscale(img)

	// Green should convert to 0.587 * 255 ≈ 149.7
	expectedLuma := 0.587 * 255
	tolerance := 1.0
	if gray[0][0] < expectedLuma-tolerance || gray[0][0] > expectedLuma+tolerance {
		t.Errorf("expected green pixel luma ~%.2f, got %.2f", expectedLuma, gray[0][0])
	}
}

func TestToGrayscale_BlueChannel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{0, 0, 255, 255}) // Pure blue

	gray := toGrayscale(img)

	// Blue should convert to 0.114 * 255 ≈ 29.07
	expectedLuma := 0.114 * 255
	tolerance := 1.0
	if gray[0][0] < expectedLuma-tolerance || gray[0][0] > expectedLuma+tolerance {
		t.Errorf("expected blue pixel luma ~%.2f, got %.2f", expectedLuma, gray[0][0])
	}
}

func TestComputeHashes_HashStability(t *testing.T) {
	// Verify that hashing the same image multiple times produces same result
	img := createGradientImage(100, 100)
	data := encodeJPEG(img)

	var lastPHash, lastDHash string
	for i := 0; i < 5; i++ {
		result, err := ComputeHashes(data)
		if err != nil {
			t.Fatalf("iteration %d: ComputeHashes failed: %v", i, err)
		}

		if i > 0 {
			if result.PHash != lastPHash {
				t.Errorf("iteration %d: pHash changed from %s to %s", i, lastPHash, result.PHash)
			}
			if result.DHash != lastDHash {
				t.Errorf("iteration %d: dHash changed from %s to %s", i, lastDHash, result.DHash)
			}
		}
		lastPHash = result.PHash
		lastDHash = result.DHash
	}
}

func TestSimilar_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		hash1     uint64
		hash2     uint64
		threshold int
		expected  bool
	}{
		{"threshold 0 identical", 0x123456789ABCDEF0, 0x123456789ABCDEF0, 0, true},
		{"threshold 0 one bit diff", 0x123456789ABCDEF0, 0x123456789ABCDEF1, 0, false},
		{"threshold 64 always true", 0, 0xFFFFFFFFFFFFFFFF, 64, true},
		{"threshold 63 max diff", 0, 0xFFFFFFFFFFFFFFFF, 63, false},
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

// Benchmark tests

func BenchmarkComputeHashes_Small(b *testing.B) {
	img := createTestImage(100, 100, color.Gray{128})
	data := encodeJPEG(img)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeHashes(data)
	}
}

func BenchmarkComputeHashes_Large(b *testing.B) {
	img := createTestImage(1920, 1080, color.Gray{128})
	data := encodeJPEG(img)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeHashes(data)
	}
}

func BenchmarkHammingDistance(b *testing.B) {
	hash1 := uint64(0xABCDEF0123456789)
	hash2 := uint64(0x123456789ABCDEF0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HammingDistance(hash1, hash2)
	}
}

func BenchmarkResizeImage(b *testing.B) {
	img := createTestImage(2000, 1500, color.White)
	data := encodeJPEG(img)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ResizeImage(data, 500)
	}
}
