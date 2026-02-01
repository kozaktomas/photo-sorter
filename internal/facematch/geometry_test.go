package facematch

import (
	"math"
	"testing"
)

func TestComputeIoU(t *testing.T) {
	tests := []struct {
		name     string
		bbox1    []float64
		bbox2    []float64
		expected float64
	}{
		{
			name:     "identical boxes",
			bbox1:    []float64{0, 0, 10, 10},
			bbox2:    []float64{0, 0, 10, 10},
			expected: 1.0,
		},
		{
			name:     "no overlap",
			bbox1:    []float64{0, 0, 10, 10},
			bbox2:    []float64{20, 20, 30, 30},
			expected: 0.0,
		},
		{
			name:     "partial overlap",
			bbox1:    []float64{0, 0, 10, 10},
			bbox2:    []float64{5, 5, 15, 15},
			expected: 25.0 / 175.0, // intersection=25, union=100+100-25=175
		},
		{
			name:     "one inside other",
			bbox1:    []float64{0, 0, 20, 20},
			bbox2:    []float64{5, 5, 15, 15},
			expected: 100.0 / 400.0, // intersection=100, union=400 (larger box)
		},
		{
			name:     "invalid bbox1",
			bbox1:    []float64{0, 0, 10},
			bbox2:    []float64{0, 0, 10, 10},
			expected: 0.0,
		},
		{
			name:     "empty bboxes",
			bbox1:    []float64{},
			bbox2:    []float64{},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComputeIoU(tt.bbox1, tt.bbox2)
			if math.Abs(result-tt.expected) > 0.0001 {
				t.Errorf("ComputeIoU(%v, %v) = %v, want %v", tt.bbox1, tt.bbox2, result, tt.expected)
			}
		})
	}
}

func TestConvertPixelBBoxToRelative(t *testing.T) {
	tests := []struct {
		name     string
		bbox     []float64
		width    int
		height   int
		expected []float64
	}{
		{
			name:     "simple conversion",
			bbox:     []float64{100, 200, 300, 400},
			width:    1000,
			height:   1000,
			expected: []float64{0.1, 0.2, 0.3, 0.4},
		},
		{
			name:     "full image",
			bbox:     []float64{0, 0, 1920, 1080},
			width:    1920,
			height:   1080,
			expected: []float64{0, 0, 1, 1},
		},
		{
			name:     "invalid bbox",
			bbox:     []float64{100, 200},
			width:    1000,
			height:   1000,
			expected: []float64{100, 200},
		},
		{
			name:     "zero dimensions",
			bbox:     []float64{100, 200, 300, 400},
			width:    0,
			height:   1000,
			expected: []float64{100, 200, 300, 400},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPixelBBoxToRelative(tt.bbox, tt.width, tt.height)
			if len(result) != len(tt.expected) {
				t.Errorf("ConvertPixelBBoxToRelative() length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if math.Abs(result[i]-tt.expected[i]) > 0.0001 {
					t.Errorf("ConvertPixelBBoxToRelative()[%d] = %v, want %v", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestConvertPixelBBoxToDisplayRelative(t *testing.T) {
	tests := []struct {
		name        string
		bbox        []float64
		width       int  // raw file width from PhotoPrism
		height      int  // raw file height from PhotoPrism
		orientation int
		expected    []float64
	}{
		{
			name:        "orientation 1 (normal) - no dimension swap",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000,
			height:      800,
			orientation: 1,
			// display = raw, so divide by 1000 and 800
			expected: []float64{0.1, 0.25, 0.2, 0.25}, // x, y, w, h
		},
		{
			name:        "orientation 6 (90 CW) - dimensions swapped for display",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000, // raw width (landscape file)
			height:      800,  // raw height
			orientation: 6,
			// For orientation 6, display dimensions are swapped: 800x1000
			// InsightFace sees rotated image (800x1000), bbox in that space
			// Divide by display dims: 100/800=0.125, 200/1000=0.2, etc.
			expected: []float64{0.125, 0.2, 0.25, 0.2},
		},
		{
			name:        "orientation 8 (90 CCW) - dimensions swapped for display",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000,
			height:      800,
			orientation: 8,
			// Same as orientation 6 - dimensions swapped
			expected: []float64{0.125, 0.2, 0.25, 0.2},
		},
		{
			name:        "orientation 3 (180 rotation) - no dimension swap",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000,
			height:      800,
			orientation: 3,
			// Orientation 3 doesn't swap dimensions (just 180 rotation)
			expected: []float64{0.1, 0.25, 0.2, 0.25},
		},
		{
			name:        "orientation 5 (transpose) - dimensions swapped",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000,
			height:      800,
			orientation: 5,
			// Orientation 5-8 all swap dimensions
			expected: []float64{0.125, 0.2, 0.25, 0.2},
		},
		{
			name:        "invalid bbox",
			bbox:        []float64{100, 200},
			width:       1000,
			height:      800,
			orientation: 1,
			expected:    []float64{100, 200},
		},
		{
			name:        "zero width",
			bbox:        []float64{100, 200, 300, 400},
			width:       0,
			height:      800,
			orientation: 1,
			expected:    []float64{100, 200, 300, 400},
		},
		{
			name:        "zero height",
			bbox:        []float64{100, 200, 300, 400},
			width:       1000,
			height:      0,
			orientation: 1,
			expected:    []float64{100, 200, 300, 400},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertPixelBBoxToDisplayRelative(tt.bbox, tt.width, tt.height, tt.orientation)
			if len(result) != len(tt.expected) {
				t.Errorf("ConvertPixelBBoxToDisplayRelative() length = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if math.Abs(result[i]-tt.expected[i]) > 0.0001 {
					t.Errorf("ConvertPixelBBoxToDisplayRelative()[%d] = %v, want %v (full result: %v)", i, result[i], tt.expected[i], result)
				}
			}
		})
	}
}

func TestMarkerToCornerBBox(t *testing.T) {
	result := MarkerToCornerBBox(0.1, 0.2, 0.3, 0.4)
	expected := []float64{0.1, 0.2, 0.4, 0.6}
	for i := range result {
		if math.Abs(result[i]-expected[i]) > 0.0001 {
			t.Errorf("MarkerToCornerBBox() = %v, want %v", result, expected)
			break
		}
	}
}
