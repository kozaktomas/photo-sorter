package ai

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// Helper functions for creating test images

func createTestImage(width, height int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := range width {
		for y := range height {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodeJPEG(img image.Image) []byte {
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// --- ResizeImage tests ---

func TestResizeImage_NoResizeNeeded(t *testing.T) {
	img := createTestImage(100, 100, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 200)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	if len(resized) == 0 {
		t.Error("expected non-empty result")
	}

	// Verify it's a valid JPEG
	_, format, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	if format != "jpeg" {
		t.Errorf("expected jpeg format, got %s", format)
	}
}

func TestResizeImage_NeedsResize_Landscape(t *testing.T) {
	img := createTestImage(2000, 1000, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 500)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()

	// Width should be maxSize
	if bounds.Dx() != 500 {
		t.Errorf("expected width 500, got %d", bounds.Dx())
	}

	// Height should maintain aspect ratio (2000/1000 = 2:1)
	if bounds.Dy() != 250 {
		t.Errorf("expected height 250, got %d", bounds.Dy())
	}
}

func TestResizeImage_NeedsResize_Portrait(t *testing.T) {
	img := createTestImage(1000, 2000, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 500)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()

	// Height should be maxSize
	if bounds.Dy() != 500 {
		t.Errorf("expected height 500, got %d", bounds.Dy())
	}

	// Width should maintain aspect ratio
	if bounds.Dx() != 250 {
		t.Errorf("expected width 250, got %d", bounds.Dx())
	}
}

func TestResizeImage_NeedsResize_Square(t *testing.T) {
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

	// Should be exactly 200x200
	if bounds.Dx() != 200 || bounds.Dy() != 200 {
		t.Errorf("expected 200x200, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_PreservesAspectRatio(t *testing.T) {
	// 4:3 aspect ratio
	img := createTestImage(1600, 1200, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 400)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()
	ratio := float64(bounds.Dx()) / float64(bounds.Dy())
	expectedRatio := 4.0 / 3.0

	// Allow small tolerance for rounding
	if ratio < expectedRatio-0.1 || ratio > expectedRatio+0.1 {
		t.Errorf("expected aspect ratio ~%.2f, got %.2f (%dx%d)",
			expectedRatio, ratio, bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_InvalidData(t *testing.T) {
	invalidData := []byte("not an image")

	_, err := ResizeImage(invalidData, 500)
	if err == nil {
		t.Error("expected error for invalid image data")
	}
}

func TestResizeImage_EmptyData(t *testing.T) {
	_, err := ResizeImage([]byte{}, 500)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestResizeImage_PNGInput(t *testing.T) {
	img := createTestImage(100, 100, color.White)
	data := encodePNG(img)

	resized, err := ResizeImage(data, 200)
	if err != nil {
		t.Fatalf("ResizeImage failed for PNG: %v", err)
	}

	// Should convert to JPEG
	_, format, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	if format != "jpeg" {
		t.Errorf("expected jpeg output format, got %s", format)
	}
}

func TestResizeImage_LargeImage(t *testing.T) {
	// Test with a large image
	img := createTestImage(4000, 3000, color.Gray{128})
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 1920)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode resized image: %v", err)
	}

	bounds := decodedImg.Bounds()

	if bounds.Dx() > 1920 || bounds.Dy() > 1920 {
		t.Errorf("expected max dimension 1920, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_ExactlyMaxSize(t *testing.T) {
	// Image exactly at maxSize should still be returned (re-encoded)
	img := createTestImage(500, 500, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 500)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	bounds := decodedImg.Bounds()
	if bounds.Dx() != 500 || bounds.Dy() != 500 {
		t.Errorf("expected 500x500, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeImage_OneDimensionAtMax(t *testing.T) {
	// Image with one dimension at max, other smaller
	img := createTestImage(500, 300, color.White)
	data := encodeJPEG(img)

	resized, err := ResizeImage(data, 500)
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	// Should not resize
	decodedImg, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	bounds := decodedImg.Bounds()
	if bounds.Dx() != 500 || bounds.Dy() != 300 {
		t.Errorf("expected 500x300, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

// --- Data structure tests ---

func TestPhotoMetadata_ZeroValue(t *testing.T) {
	meta := PhotoMetadata{}

	if meta.OriginalName != "" {
		t.Error("expected empty OriginalName")
	}

	if meta.Year != 0 {
		t.Error("expected Year 0")
	}

	if meta.Lat != 0 {
		t.Error("expected Lat 0")
	}
}

func TestUsage_ZeroValue(t *testing.T) {
	usage := Usage{}

	if usage.InputTokens != 0 {
		t.Error("expected InputTokens 0")
	}

	if usage.OutputTokens != 0 {
		t.Error("expected OutputTokens 0")
	}

	if usage.TotalCost != 0 {
		t.Error("expected TotalCost 0")
	}
}

func TestBatchStatus_Fields(t *testing.T) {
	status := BatchStatus{
		ID:             "batch-123",
		Status:         "in_progress",
		TotalRequests:  100,
		CompletedCount: 50,
		FailedCount:    5,
	}

	if status.ID != "batch-123" {
		t.Errorf("expected ID 'batch-123', got '%s'", status.ID)
	}

	if status.Status != "in_progress" {
		t.Errorf("expected Status 'in_progress', got '%s'", status.Status)
	}

	if status.TotalRequests != 100 {
		t.Errorf("expected TotalRequests 100, got %d", status.TotalRequests)
	}

	if status.CompletedCount != 50 {
		t.Errorf("expected CompletedCount 50, got %d", status.CompletedCount)
	}

	if status.FailedCount != 5 {
		t.Errorf("expected FailedCount 5, got %d", status.FailedCount)
	}
}

func TestPhotoAnalysis_Labels(t *testing.T) {
	analysis := PhotoAnalysis{
		Labels: []LabelWithConfidence{
			{Name: "vacation", Confidence: 0.95},
			{Name: "beach", Confidence: 0.85},
			{Name: "sunset", Confidence: 0.70},
		},
		Description:   "A beautiful sunset at the beach",
		EstimatedDate: "2024-06-15",
	}

	if len(analysis.Labels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(analysis.Labels))
	}

	if analysis.Labels[0].Name != "vacation" {
		t.Errorf("expected first label 'vacation', got '%s'", analysis.Labels[0].Name)
	}

	if analysis.Labels[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", analysis.Labels[0].Confidence)
	}
}

func TestLabelWithConfidence_Threshold(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		above80    bool
	}{
		{"95% confident", 0.95, true},
		{"80% confident", 0.80, false}, // Not > 0.8
		{"81% confident", 0.81, true},
		{"79% confident", 0.79, false},
		{"50% confident", 0.50, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label := LabelWithConfidence{
				Name:       "test",
				Confidence: tc.confidence,
			}

			result := label.Confidence > 0.8
			if result != tc.above80 {
				t.Errorf("confidence %f > 0.8 = %v, expected %v",
					tc.confidence, result, tc.above80)
			}
		})
	}
}

func TestAlbumDateEstimate_Fields(t *testing.T) {
	estimate := AlbumDateEstimate{
		EstimatedDate: "1985-07-15",
		Confidence:    0.85,
		Reasoning:     "Based on clothing styles and cars visible in photos",
	}

	if estimate.EstimatedDate != "1985-07-15" {
		t.Errorf("expected date '1985-07-15', got '%s'", estimate.EstimatedDate)
	}

	if estimate.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", estimate.Confidence)
	}

	if estimate.Reasoning == "" {
		t.Error("expected non-empty reasoning")
	}
}

func TestSortSuggestion_Fields(t *testing.T) {
	suggestion := SortSuggestion{
		PhotoUID: "photo123",
		Labels: []LabelWithConfidence{
			{Name: "sports", Confidence: 0.90},
		},
		Description:   "People playing sports",
		EstimatedDate: "2024-01-15",
	}

	if suggestion.PhotoUID != "photo123" {
		t.Errorf("expected PhotoUID 'photo123', got '%s'", suggestion.PhotoUID)
	}

	if len(suggestion.Labels) != 1 {
		t.Errorf("expected 1 label, got %d", len(suggestion.Labels))
	}

	if suggestion.Description != "People playing sports" {
		t.Errorf("unexpected description")
	}

	if suggestion.EstimatedDate != "2024-01-15" {
		t.Errorf("expected date '2024-01-15', got '%s'", suggestion.EstimatedDate)
	}
}

func TestBatchPhotoRequest_Fields(t *testing.T) {
	req := BatchPhotoRequest{
		PhotoUID:        "photo456",
		ImageData:       []byte{0xFF, 0xD8}, // JPEG magic bytes
		Metadata:        &PhotoMetadata{OriginalName: "test.jpg"},
		AvailableLabels: []string{"label1", "label2"},
		EstimateDate:    true,
	}

	if req.PhotoUID != "photo456" {
		t.Errorf("expected PhotoUID 'photo456', got '%s'", req.PhotoUID)
	}

	if len(req.ImageData) != 2 {
		t.Errorf("expected 2 bytes, got %d", len(req.ImageData))
	}

	if req.Metadata == nil {
		t.Error("expected non-nil Metadata")
	}

	if len(req.AvailableLabels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(req.AvailableLabels))
	}

	if !req.EstimateDate {
		t.Error("expected EstimateDate true")
	}
}

func TestBatchPhotoResult_Success(t *testing.T) {
	result := BatchPhotoResult{
		PhotoUID: "photo789",
		Analysis: &PhotoAnalysis{
			Description: "A test photo",
		},
		Error: "",
	}

	if result.PhotoUID != "photo789" {
		t.Errorf("expected PhotoUID 'photo789', got '%s'", result.PhotoUID)
	}

	if result.Analysis == nil {
		t.Error("expected non-nil Analysis")
	}

	if result.Error != "" {
		t.Errorf("expected empty Error, got '%s'", result.Error)
	}
}

func TestBatchPhotoResult_Failure(t *testing.T) {
	result := BatchPhotoResult{
		PhotoUID: "photo789",
		Analysis: nil,
		Error:    "failed to process image",
	}

	if result.Analysis != nil {
		t.Error("expected nil Analysis for failed result")
	}

	if result.Error != "failed to process image" {
		t.Errorf("expected error message, got '%s'", result.Error)
	}
}

// Benchmarks

func BenchmarkResizeImage_Small(b *testing.B) {
	img := createTestImage(100, 100, color.Gray{128})
	data := encodeJPEG(img)

	b.ResetTimer()
	for range b.N {
		ResizeImage(data, 50)
	}
}

func BenchmarkResizeImage_Large(b *testing.B) {
	img := createTestImage(4000, 3000, color.Gray{128})
	data := encodeJPEG(img)

	b.ResetTimer()
	for range b.N {
		ResizeImage(data, 1920)
	}
}
