package exif

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireExiftoolWrite skips when exiftool is missing. Sidecar writes
// have nothing meaningful to assert without it.
func requireExiftoolWrite(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool binary not installed; skipping")
	}
}

func TestWriteSidecar_CreatesNewSidecar(t *testing.T) {
	requireExiftoolWrite(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_0001.xmp")
	taken := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	lat, lng, alt := 50.08, 14.43, 220.0
	iso := 100
	aperture := 1.8
	focal := 50.0

	err := WriteSidecar(context.Background(), path, SidecarFields{
		TakenAt:     &taken,
		Lat:         &lat,
		Lng:         &lng,
		Altitude:    &alt,
		CameraMake:  "Canon",
		CameraModel: "EOS R5",
		LensModel:   "RF 50mm f/1.2",
		ISO:         &iso,
		Aperture:    &aperture,
		FocalLength: &focal,
		Exposure:    "1/250",
		Title:       "Sunset",
		Description: "Veselice po dešti",
		Notes:       "internal note",
	})
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("sidecar not written: %v", statErr)
	}
	if _, statErr := os.Stat(path + ".tmp"); statErr == nil {
		t.Errorf("tmp file was not cleaned up")
	}

	tags := readSidecarJSON(t, path)
	if got, _ := tags["Title"].(string); got != "Sunset" {
		t.Errorf("Title = %q, want Sunset", got)
	}
	if got, _ := tags["Description"].(string); got != "Veselice po dešti" {
		t.Errorf("Description = %q, want Veselice po dešti", got)
	}
	if !hasFloatNear(tags["GPSLatitude"], 50.08, 0.001) {
		t.Errorf("GPSLatitude = %v, want ~50.08", tags["GPSLatitude"])
	}
	if !hasFloatNear(tags["GPSLongitude"], 14.43, 0.001) {
		t.Errorf("GPSLongitude = %v, want ~14.43", tags["GPSLongitude"])
	}
	if !hasFloatNear(tags["FNumber"], 1.8, 0.001) {
		t.Errorf("FNumber = %v, want 1.8", tags["FNumber"])
	}
	// ISO can round-trip as a JSON number; tolerate float64.
	if !hasFloatNear(tags["ISO"], 100, 0.5) {
		t.Errorf("ISO = %v, want 100", tags["ISO"])
	}
	// exiftool writes DateTimeOriginal in EXIF colon format.
	dtKey := "DateTimeOriginal"
	if got, _ := tags[dtKey].(string); got == "" {
		t.Errorf("%s missing in sidecar", dtKey)
	}
}

func TestWriteSidecar_NoOpOnEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.xmp")
	if err := WriteSidecar(context.Background(), path, SidecarFields{}); err != nil {
		t.Fatalf("WriteSidecar on empty fields: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty WriteSidecar should not create a file; stat err = %v", err)
	}
}

func TestWriteSidecar_EmptyPath(t *testing.T) {
	taken := time.Now()
	err := WriteSidecar(context.Background(), "", SidecarFields{TakenAt: &taken})
	if err == nil {
		t.Fatal("expected error on empty sidecar path")
	}
}

func TestWriteSidecar_OverwritesExisting(t *testing.T) {
	requireExiftoolWrite(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "img.xmp")
	first := "Initial"
	if err := WriteSidecar(context.Background(), path, SidecarFields{Title: first}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if got, _ := readSidecarJSON(t, path)["Title"].(string); got != first {
		t.Fatalf("after first write Title = %q, want %q", got, first)
	}

	second := "Updated"
	if err := WriteSidecar(context.Background(), path, SidecarFields{Title: second}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got, _ := readSidecarJSON(t, path)["Title"].(string); got != second {
		t.Errorf("after second write Title = %q, want %q", got, second)
	}
}

// readSidecarJSON shells out to exiftool to read the sidecar tags back as
// a JSON map. Test-only helper — keeps the assertions independent of the
// production parse code.
func readSidecarJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	// #nosec G204 -- path comes from t.TempDir(); test-only.
	out, err := exec.CommandContext(
		context.Background(), "exiftool", "-json", "-n", path,
	).Output()
	if err != nil {
		t.Fatalf("exiftool read: %v", err)
	}
	var arr []map[string]any
	if err := json.Unmarshal(out, &arr); err != nil {
		t.Fatalf("parse exiftool json: %v", err)
	}
	if len(arr) == 0 {
		t.Fatal("exiftool returned empty array")
	}
	return arr[0]
}

// hasFloatNear reports whether v is a JSON number within ±tol of want.
func hasFloatNear(v any, want, tol float64) bool {
	f, ok := toFloat(v)
	if !ok {
		return false
	}
	d := f - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}
