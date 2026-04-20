package latex

import (
	"image"
	"testing"
)

func TestValidatePhotoQuality(t *testing.T) {
	tests := []struct {
		in      string
		want    PhotoQuality
		wantErr bool
	}{
		{"", QualityMedium, false},
		{"low", QualityLow, false},
		{"medium", QualityMedium, false},
		{"original", QualityOriginal, false},
		{"LOW", QualityMedium, true},
		{"best", QualityMedium, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ValidatePhotoQuality(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePhotoQuality(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("ValidatePhotoQuality(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeQuality(t *testing.T) {
	cases := map[PhotoQuality]PhotoQuality{
		QualityDefault:  QualityMedium,
		QualityLow:      QualityLow,
		QualityMedium:   QualityMedium,
		QualityOriginal: QualityOriginal,
		"junk":          QualityMedium,
	}
	for in, want := range cases {
		if got := normalizeQuality(in); got != want {
			t.Errorf("normalizeQuality(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResizeToLongestSide(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	got := resizeToLongestSide(src, 1000)
	b := got.Bounds()
	if b.Dx() != 1000 {
		t.Errorf("width = %d, want 1000", b.Dx())
	}
	if b.Dy() != 500 {
		t.Errorf("height = %d, want 500 (aspect-preserving)", b.Dy())
	}

	portrait := image.NewRGBA(image.Rect(0, 0, 1000, 4000))
	got = resizeToLongestSide(portrait, 2000)
	b = got.Bounds()
	if b.Dy() != 2000 {
		t.Errorf("height = %d, want 2000", b.Dy())
	}
	if b.Dx() != 500 {
		t.Errorf("width = %d, want 500", b.Dx())
	}
}
