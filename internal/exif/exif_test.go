package exif

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireExiftool skips the current test when the exiftool binary is not
// available on PATH so CI/dev machines without it still pass.
func requireExiftool(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool binary not installed; skipping")
	}
}

func TestExtract_BasicJPEG(t *testing.T) {
	requireExiftool(t)
	md, err := Extract(context.Background(), "testdata/basic.jpg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if md.Mime != "image/jpeg" {
		t.Errorf("Mime = %q, want image/jpeg", md.Mime)
	}
	if md.TakenAt.IsZero() {
		t.Errorf("TakenAt is zero, want EXIF-derived date")
	}
	if md.TakenAtSource != "exif" {
		t.Errorf("TakenAtSource = %q, want exif", md.TakenAtSource)
	}
	if md.CameraMake == "" || md.CameraModel == "" {
		t.Errorf("camera fields empty: make=%q model=%q", md.CameraMake, md.CameraModel)
	}
	if md.Width == 0 || md.Height == 0 {
		t.Errorf("width/height not populated: %dx%d", md.Width, md.Height)
	}
}

func TestExtract_GPSJPEG(t *testing.T) {
	requireExiftool(t)
	md, err := Extract(context.Background(), "testdata/gps.jpg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if md.Lat == nil || md.Lng == nil {
		t.Fatalf("expected lat/lng populated, got nil; raw keys: %v", keysOf(md.Raw))
	}
	// The sample's GPSPosition is approximately 39.9156 N, 116.3908 E (Beijing).
	if *md.Lat < 39 || *md.Lat > 41 {
		t.Errorf("Lat = %f, expected ~39.92", *md.Lat)
	}
	if *md.Lng < 115 || *md.Lng > 117 {
		t.Errorf("Lng = %f, expected ~116.39", *md.Lng)
	}
}

func TestExtract_FallbackParsesJPEGWithoutExiftool(t *testing.T) {
	// This test runs whether or not exiftool is installed. With exiftool we
	// exercise the happy path; without it the JPEG fallback must succeed.
	md, err := Extract(context.Background(), "testdata/basic.jpg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if md.Mime != "image/jpeg" {
		t.Errorf("Mime = %q, want image/jpeg", md.Mime)
	}
	if md.Orientation == 0 {
		t.Errorf("Orientation default not applied (got 0)")
	}
	if md.Width == 0 {
		t.Errorf("Width not populated by fallback")
	}
}

func TestExtract_MalformedFile(t *testing.T) {
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "bad.jpg")
	if err := os.WriteFile(bad, []byte("not really a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	md, err := Extract(context.Background(), bad)
	if err != nil {
		t.Fatalf("Extract on malformed file returned error: %v", err)
	}
	if md == nil {
		t.Fatal("expected non-nil Metadata")
	}
	if md.TakenAtSource != "unknown" {
		t.Errorf("TakenAtSource = %q, want unknown", md.TakenAtSource)
	}
	if md.Orientation != 1 {
		t.Errorf("Orientation = %d, want default 1", md.Orientation)
	}
}

func TestExtract_EmptyPath(t *testing.T) {
	_, err := Extract(context.Background(), "")
	if err == nil {
		t.Fatal("expected error on empty path")
	}
}

func TestExtractFromReader(t *testing.T) {
	data, err := os.ReadFile("testdata/basic.jpg")
	if err != nil {
		t.Fatal(err)
	}
	md, err := ExtractFromReader(context.Background(), bytes.NewReader(data), "IMG_20210521_143215.jpg")
	if err != nil {
		t.Fatalf("ExtractFromReader: %v", err)
	}
	if md.Mime != "image/jpeg" {
		t.Errorf("Mime = %q, want image/jpeg", md.Mime)
	}
	// With exiftool absent the basic.jpg fallback still produces an EXIF
	// date, so TakenAtSource should be "exif". If exiftool stripped EXIF
	// for some reason it would fall back to "filename" via the IMG_ name.
	if md.TakenAtSource == "unknown" {
		t.Errorf("TakenAtSource = unknown; expected exif or filename")
	}
}

func TestExtractFromReader_FilenameHeuristic(t *testing.T) {
	// Bytes that are neither JPEG nor HEIC nor PNG. Both exiftool (if
	// present) and the JPEG fallback should bail, leaving the filename
	// heuristic as the only source of a date.
	md, err := ExtractFromReader(
		context.Background(),
		bytes.NewReader([]byte("\x00\x00\x00garbage")),
		"PXL_20231005_173045.jpg",
	)
	if err != nil {
		t.Fatalf("ExtractFromReader: %v", err)
	}
	if md.TakenAtSource != "filename" {
		t.Errorf("TakenAtSource = %q, want filename", md.TakenAtSource)
	}
	want := time.Date(2023, 10, 5, 17, 30, 45, 0, time.Local)
	if !md.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", md.TakenAt, want)
	}
}

func TestExtractFromReader_NilReader(t *testing.T) {
	_, err := ExtractFromReader(context.Background(), nil, "foo.jpg")
	if err == nil {
		t.Fatal("expected error on nil reader")
	}
}

func TestParseFilenameDate(t *testing.T) {
	loc := time.Local
	cases := []struct {
		name string
		want time.Time
		ok   bool
	}{
		{"IMG_20210521_143215.jpg", time.Date(2021, 5, 21, 14, 32, 15, 0, loc), true},
		{"img_20210521_143215.JPEG", time.Date(2021, 5, 21, 14, 32, 15, 0, loc), true},
		{"IMG_20210521_143215_001.jpg", time.Date(2021, 5, 21, 14, 32, 15, 0, loc), true},
		{"PXL_20231005_173045123.MP.jpg", time.Date(2023, 10, 5, 17, 30, 45, 0, loc), true},
		{"2024-05-21 14.32.10.jpg", time.Date(2024, 5, 21, 14, 32, 10, 0, loc), true},
		{"Screenshot 2024-05-21 at 14.32.10.png", time.Date(2024, 5, 21, 14, 32, 10, 0, loc), true},
		{"random.jpg", time.Time{}, false},
		{"", time.Time{}, false},
		// Out-of-range numerics should fail rather than wrap silently.
		{"IMG_20211345_143215.jpg", time.Time{}, false},
	}
	for _, tc := range cases {
		got, ok := parseFilenameDate(tc.name)
		if ok != tc.ok {
			t.Errorf("parseFilenameDate(%q) ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if ok && !got.Equal(tc.want) {
			t.Errorf("parseFilenameDate(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseExiftoolJSON(t *testing.T) {
	raw := map[string]any{
		"MIMEType":         "image/jpeg",
		"DateTimeOriginal": "2024:05:21 14:32:15",
		"ImageWidth":       float64(4000),
		"ImageHeight":      float64(3000),
		"Orientation":      float64(6),
		"GPSLatitude":      float64(50.123),
		"GPSLongitude":     float64(14.456),
		"GPSAltitude":      float64(220),
		"GPSAltitudeRef":   float64(0),
		"Make":             "Canon  ",
		"Model":            " EOS R5 ",
		"LensModel":        "RF24-70mm F2.8 L IS USM",
		"ISO":              float64(400),
		"FNumber":          float64(2.8),
		"FocalLength":      float64(35),
		"ExposureTime":     0.004,
	}
	md := parseExiftoolJSON(raw)
	if md.Mime != "image/jpeg" {
		t.Errorf("Mime = %q", md.Mime)
	}
	if md.TakenAt.IsZero() {
		t.Errorf("TakenAt empty")
	}
	if md.TakenAtSource != "exif" {
		t.Errorf("TakenAtSource = %q", md.TakenAtSource)
	}
	if md.Width != 4000 || md.Height != 3000 {
		t.Errorf("dims = %dx%d", md.Width, md.Height)
	}
	if md.Orientation != 6 {
		t.Errorf("Orientation = %d", md.Orientation)
	}
	if md.Lat == nil || md.Lng == nil || *md.Lat != 50.123 || *md.Lng != 14.456 {
		t.Errorf("lat/lng wrong: %+v %+v", md.Lat, md.Lng)
	}
	if md.Altitude == nil || *md.Altitude != 220 {
		t.Errorf("Altitude wrong: %+v", md.Altitude)
	}
	if md.CameraMake != "Canon" || md.CameraModel != "EOS R5" {
		t.Errorf("camera trim failed: %q / %q", md.CameraMake, md.CameraModel)
	}
	if md.LensModel == "" {
		t.Errorf("LensModel empty")
	}
	if md.ISO == nil || *md.ISO != 400 {
		t.Errorf("ISO wrong: %+v", md.ISO)
	}
	if md.Aperture == nil || *md.Aperture != 2.8 {
		t.Errorf("Aperture wrong: %+v", md.Aperture)
	}
	if md.FocalLength == nil || *md.FocalLength != 35 {
		t.Errorf("FocalLength wrong: %+v", md.FocalLength)
	}
	if md.Exposure != "1/250" {
		t.Errorf("Exposure = %q, want 1/250", md.Exposure)
	}
	if md.Raw == nil {
		t.Errorf("Raw is nil; want passthrough map")
	}
}

func TestParseExiftoolJSON_SouthWestSign(t *testing.T) {
	// Older exiftool builds without -n applying signs return magnitude and
	// a separate Ref field. parseLatLng should flip the sign.
	raw := map[string]any{
		"GPSLatitude":     float64(33.870415),
		"GPSLatitudeRef":  "S",
		"GPSLongitude":    float64(117.722366),
		"GPSLongitudeRef": "W",
	}
	md := parseExiftoolJSON(raw)
	if md.Lat == nil || *md.Lat > 0 {
		t.Errorf("Lat = %+v, want negative", md.Lat)
	}
	if md.Lng == nil || *md.Lng > 0 {
		t.Errorf("Lng = %+v, want negative", md.Lng)
	}
}

func TestFormatExposure(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{0.004, "1/250"},
		{0.00025, "1/4000"},
		{1.0, "1s"},
		{30.0, "30s"},
		{"1/250", "1/250"}, // pass-through if exiftool emitted a string
		{0.0, ""},
		{nil, ""},
	}
	for _, tc := range cases {
		got := formatExposure(tc.in)
		if got != tc.want {
			t.Errorf("formatExposure(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDetectMime(t *testing.T) {
	mime, isJPEG, err := detectMime("testdata/basic.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" || !isJPEG {
		t.Errorf("got %q/%v, want image/jpeg/true", mime, isJPEG)
	}
}

// keysOf returns the keys of a map for friendly test failure messages.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
