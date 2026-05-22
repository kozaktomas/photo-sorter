package imgedit

import (
	"image"
	"image/color"
	"testing"
)

func TestApplyEditsZeroReturnsSrc(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	out := ApplyEdits(src, PhotoEdits{})
	if out != src {
		t.Fatal("expected zero-edit ApplyEdits to return src unchanged")
	}
}

func TestApplyCropClampsAndCrops(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 50))
	// Fill the right half with red.
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	for y := range 50 {
		for x := 50; x < 100; x++ {
			src.SetRGBA(x, y, red)
		}
	}
	edits := PhotoEdits{Crop: &CropRect{X: 0.5, Y: 0, W: 0.5, H: 1}}
	out := ApplyEdits(src, edits)
	if out.Bounds().Dx() != 50 || out.Bounds().Dy() != 50 {
		t.Fatalf("unexpected crop size: %v", out.Bounds())
	}
	r, _, _, _ := out.At(10, 10).RGBA()
	if r>>8 != 255 {
		t.Fatalf("expected red pixel at top-left of crop, got r=%d", r>>8)
	}
}

func TestApplyRotation90SwapsDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	out := ApplyEdits(src, PhotoEdits{Rotation: 90})
	if out.Bounds().Dx() != 2 || out.Bounds().Dy() != 4 {
		t.Fatalf("expected 2x4 after rotate 90, got %v", out.Bounds())
	}
}

func TestApplyRotation180PreservesDimensions(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 5))
	out := ApplyEdits(src, PhotoEdits{Rotation: 180})
	if out.Bounds().Dx() != 3 || out.Bounds().Dy() != 5 {
		t.Fatalf("expected 3x5 after rotate 180, got %v", out.Bounds())
	}
}

func TestApplyBrightnessBrightens(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	out := ApplyEdits(src, PhotoEdits{Brightness: 0.5})
	r, _, _, _ := out.At(0, 0).RGBA()
	got := r >> 8
	// 100 + 0.5*255 ≈ 227.5 → rounds to 228.
	if got < 225 || got > 230 {
		t.Fatalf("expected brightened pixel near 228, got %d", got)
	}
}

func TestApplyContrastSeparatesFromMidpoint(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 200, G: 200, B: 200, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 50, G: 50, B: 50, A: 255})
	out := ApplyEdits(src, PhotoEdits{Contrast: 0.5})
	bright, _, _, _ := out.At(0, 0).RGBA()
	dark, _, _, _ := out.At(1, 0).RGBA()
	if bright>>8 < 200 {
		t.Fatalf("expected bright pixel brighter after contrast+, got %d", bright>>8)
	}
	if dark>>8 > 50 {
		t.Fatalf("expected dark pixel darker after contrast+, got %d", dark>>8)
	}
}

func TestPhotoEditsIsZero(t *testing.T) {
	if !(PhotoEdits{}).IsZero() {
		t.Fatal("default PhotoEdits should be zero")
	}
	if (PhotoEdits{Rotation: 90}).IsZero() {
		t.Fatal("rotation set should not be zero")
	}
	if (PhotoEdits{Crop: &CropRect{}}).IsZero() {
		t.Fatal("crop set should not be zero")
	}
	if (PhotoEdits{Brightness: 0.1}).IsZero() {
		t.Fatal("brightness set should not be zero")
	}
	if (PhotoEdits{Contrast: -0.1}).IsZero() {
		t.Fatal("contrast set should not be zero")
	}
}
