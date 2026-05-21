package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// newExifEditRequest builds a PUT /api/v1/photos/{uid}/exif request with
// a JSON body and the chi URLParam wired in.
func newExifEditRequest(t *testing.T, uid, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), "PUT", "/api/v1/photos/"+uid+"/exif", bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	return requestWithChiParams(req, map[string]string{"uid": uid})
}

func TestPhotosHandler_EditExif_UpdatesDBRow(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto(
		"photo123", "abc123456789", "Old", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	body := `{
		"taken_at": "1995-08-12T10:00:00Z",
		"lat": 50.08, "lng": 14.43, "altitude": 220,
		"camera_make": "Canon", "camera_model": "EOS R5",
		"lens_model": "RF 50mm f/1.2", "iso": 100,
		"aperture": 1.8, "exposure": "1/250", "focal_length": 50,
		"title": "Veselice", "description": "Po dešti", "notes": "internal"
	}`
	req := newExifEditRequest(t, "photo123", body)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	assertContentType(t, rec, "application/json")

	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	if stored.TakenAt == nil || stored.TakenAt.Year() != 1995 {
		t.Errorf("TakenAt = %v, want 1995", stored.TakenAt)
	}
	if stored.Lat == nil || *stored.Lat != 50.08 || stored.Lng == nil || *stored.Lng != 14.43 {
		t.Errorf("lat/lng = %v/%v, want 50.08/14.43", stored.Lat, stored.Lng)
	}
	if stored.Altitude == nil || *stored.Altitude != 220 {
		t.Errorf("Altitude = %v, want 220", stored.Altitude)
	}
	if stored.CameraMake != "Canon" || stored.CameraModel != "EOS R5" {
		t.Errorf("camera = %q/%q", stored.CameraMake, stored.CameraModel)
	}
	if stored.LensModel != "RF 50mm f/1.2" {
		t.Errorf("LensModel = %q", stored.LensModel)
	}
	if stored.ISO == nil || *stored.ISO != 100 {
		t.Errorf("ISO = %v, want 100", stored.ISO)
	}
	if stored.Aperture == nil || *stored.Aperture != 1.8 {
		t.Errorf("Aperture = %v, want 1.8", stored.Aperture)
	}
	if stored.FocalLength == nil || *stored.FocalLength != 50 {
		t.Errorf("FocalLength = %v, want 50", stored.FocalLength)
	}
	if stored.Exposure != "1/250" {
		t.Errorf("Exposure = %q, want 1/250", stored.Exposure)
	}
	if stored.Title != "Veselice" || stored.Description != "Po dešti" || stored.Notes != "internal" {
		t.Errorf("text fields = %q/%q/%q", stored.Title, stored.Description, stored.Notes)
	}
}

func TestPhotosHandler_EditExif_WritesSidecar(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed; skipping sidecar assertion")
	}

	reader := newFakePhotoReader()
	photo := samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	photo.FilePath = "2024/06/IMG_1234.jpg"
	photo.FileName = "IMG_1234.jpg"
	reader.add(photo)

	store := newTestStorage(t)
	// Place an original file so the sidecar lives next to a real file
	// (not strictly required — exiftool creates the .xmp regardless — but
	// it mirrors production conditions).
	writeOriginalFile(t, store, photo.FilePath, []byte("fake jpeg"))

	h := createPhotosHandlerNative(testConfig(), reader, store)

	body := `{"title": "Sunset", "lat": 50.08, "lng": 14.43, "iso": 100, "taken_at": "2024-06-15T14:30:00Z"}`
	req := newExifEditRequest(t, "photo123", body)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)

	assertStatusCode(t, rec, http.StatusOK)

	// Sidecar path: same dir + same basename + .xmp.
	sidecarAbs, err := store.AbsOriginal("2024/06/IMG_1234.xmp")
	if err != nil {
		t.Fatalf("AbsOriginal: %v", err)
	}
	info, statErr := os.Stat(sidecarAbs)
	if statErr != nil {
		t.Fatalf("sidecar not written: %v", statErr)
	}
	if info.Size() == 0 {
		t.Errorf("sidecar is empty")
	}
	// The .tmp transitional file must be gone.
	if _, statErr := os.Stat(sidecarAbs + ".tmp"); statErr == nil {
		t.Errorf("sidecar .tmp left behind")
	}
}

func TestPhotosHandler_EditExif_NoSidecarOnEmptyPath(t *testing.T) {
	reader := newFakePhotoReader()
	photo := samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	photo.FilePath = "" // forces the sidecar step to be skipped
	reader.add(photo)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newExifEditRequest(t, "photo123", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)

	assertStatusCode(t, rec, http.StatusOK)
	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	if stored.Title != "x" {
		t.Errorf("Title = %q, want x", stored.Title)
	}
}

func TestPhotosHandler_EditExif_InvalidDate(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unparseable", `{"taken_at": "yesterday"}`, "invalid taken_at"},
		{"too old", `{"taken_at": "1750-01-01T00:00:00Z"}`, "taken_at out of range"},
		{"too new", `{"taken_at": "2200-01-01T00:00:00Z"}`, "taken_at out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newExifEditRequest(t, "photo123", tc.body)
			rec := httptest.NewRecorder()
			h.EditExif(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tc.want)
		})
	}
}

func TestPhotosHandler_EditExif_InvalidLatLng(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	cases := []struct {
		name string
		body string
		want string
	}{
		{"lat alone", `{"lat": 50.0}`, "lat and lng must be provided together"},
		{"lng alone", `{"lng": 14.0}`, "lat and lng must be provided together"},
		{"lat out of range", `{"lat": 95.0, "lng": 14.0}`, "lat out of range"},
		{"lng out of range", `{"lat": 50.0, "lng": -200.0}`, "lng out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newExifEditRequest(t, "photo123", tc.body)
			rec := httptest.NewRecorder()
			h.EditExif(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tc.want)
		})
	}
}

func TestPhotosHandler_EditExif_InvalidISO(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newExifEditRequest(t, "photo123", `{"iso": 0}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "iso must be positive")
}

func TestPhotosHandler_EditExif_MissingUID(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))

	req := httptest.NewRequestWithContext(
		context.Background(), "PUT", "/api/v1/photos//exif", bytes.NewBufferString(`{}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONError(t, rec, "missing photo UID")
}

func TestPhotosHandler_EditExif_NotFound(t *testing.T) {
	h := createPhotosHandlerNative(testConfig(), newFakePhotoReader(), newTestStorage(t))
	req := newExifEditRequest(t, "ghost", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
	assertJSONError(t, rec, "photo not found")
}

func TestPhotosHandler_EditExif_ArchivedNotFound(t *testing.T) {
	reader := newFakePhotoReader()
	p := samplePhoto("a1", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	now := time.Now()
	p.ArchivedAt = &now
	reader.add(p)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newExifEditRequest(t, "a1", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusNotFound)
}

func TestPhotosHandler_EditExif_NoRepo(t *testing.T) {
	h := createPhotosHandlerForTest(testConfig())
	req := newExifEditRequest(t, "photo123", `{"title": "x"}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusServiceUnavailable)
}

func TestPhotosHandler_EditExif_ViewerForbidden(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newExifEditRequest(t, "photo123", `{"title": "x"}`)
	ctx := middleware.SetSessionInContext(req.Context(), &middleware.Session{Role: "viewer"})
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusForbidden)
}

// TestPhotosHandler_EditExif_GapFixFields verifies that the metadata
// gap-fix fields (keywords, panorama, scan, exif_artist / copyright /
// license / software) round-trip from the request body onto the photo
// row. quality / time_zone / taken_at_offset are intentionally NOT in
// this payload because the EXIF endpoint rejects them; see
// TestPhotosHandler_EditExif_RejectsReadOnly.
func TestPhotosHandler_EditExif_GapFixFields(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p",
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	body := `{
		"keywords": ["sunset", "veselice", "Veselice ", "  ", "ČR 🇨🇿", "sunset"],
		"panorama": true,
		"scan": true,
		"exif_artist": "Alice Photographer",
		"exif_copyright": "(c) 2024 Alice",
		"exif_license": "CC BY-SA 4.0",
		"exif_software": "PhotoPrism 240801"
	}`
	req := newExifEditRequest(t, "photo123", body)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	wantKW := []string{"sunset", "veselice", "Veselice", "ČR 🇨🇿"}
	if len(stored.Keywords) != len(wantKW) {
		t.Fatalf("keywords = %v, want %v", stored.Keywords, wantKW)
	}
	for i, kw := range wantKW {
		if stored.Keywords[i] != kw {
			t.Errorf("keywords[%d] = %q, want %q", i, stored.Keywords[i], kw)
		}
	}
	if !stored.Panorama {
		t.Errorf("Panorama = false, want true")
	}
	if !stored.Scan {
		t.Errorf("Scan = false, want true")
	}
	if stored.ExifArtist != "Alice Photographer" {
		t.Errorf("ExifArtist = %q", stored.ExifArtist)
	}
	if stored.ExifCopyright != "(c) 2024 Alice" {
		t.Errorf("ExifCopyright = %q", stored.ExifCopyright)
	}
	if stored.ExifLicense != "CC BY-SA 4.0" {
		t.Errorf("ExifLicense = %q", stored.ExifLicense)
	}
	if stored.ExifSoftware != "PhotoPrism 240801" {
		t.Errorf("ExifSoftware = %q", stored.ExifSoftware)
	}
}

// TestPhotosHandler_EditExif_ClearKeywords confirms that a request
// containing `"keywords": []` clears the column. Distinguishing
// "absent" from "explicit empty" matters because the same handler is
// used for both partial updates and "reset to no keywords" actions.
func TestPhotosHandler_EditExif_ClearKeywords(t *testing.T) {
	reader := newFakePhotoReader()
	p := samplePhoto("photo123", "abc123456789", "p",
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
	p.Keywords = []string{"old", "tags"}
	reader.add(p)
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	req := newExifEditRequest(t, "photo123", `{"keywords": []}`)
	rec := httptest.NewRecorder()
	h.EditExif(rec, req)
	assertStatusCode(t, rec, http.StatusOK)

	stored, _ := reader.GetPhoto(context.Background(), "photo123")
	if len(stored.Keywords) != 0 {
		t.Errorf("keywords = %v, want empty slice", stored.Keywords)
	}
}

// TestPhotosHandler_EditExif_RejectsReadOnly exercises the three keys
// that GET surfaces but PUT rejects (quality / taken_at_offset /
// time_zone). Each must produce a 400 with a clear message so a buggy
// client gets a loud failure rather than a silently dropped field.
func TestPhotosHandler_EditExif_RejectsReadOnly(t *testing.T) {
	reader := newFakePhotoReader()
	reader.add(samplePhoto("photo123", "abc123456789", "p",
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)))
	h := createPhotosHandlerNative(testConfig(), reader, newTestStorage(t))

	cases := []struct {
		field, body string
	}{
		{"quality", `{"quality": 5}`},
		{"taken_at_offset", `{"taken_at_offset": 7200}`},
		{"time_zone", `{"time_zone": "Europe/Prague"}`},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			req := newExifEditRequest(t, "photo123", tc.body)
			rec := httptest.NewRecorder()
			h.EditExif(rec, req)
			assertStatusCode(t, rec, http.StatusBadRequest)
			assertJSONError(t, rec, tc.field+" is read-only")
		})
	}
}

func TestSidecarRelPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2024/06/IMG_1234.jpg", "2024/06/IMG_1234.xmp"},
		{"deep/nested/photo.HEIC", "deep/nested/photo.xmp"},
		{"no_extension", "no_extension.xmp"},
		{"", ""},
		// Confirm the dir part is preserved verbatim regardless of OS.
		{filepath.Join("a", "b", "c.cr2"), filepath.Join("a", "b", "c.xmp")},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := sidecarRelPath(tc.in)
			if got != tc.want {
				t.Errorf("sidecarRelPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got != "" && !strings.HasSuffix(got, ".xmp") {
				t.Errorf("result does not end in .xmp: %q", got)
			}
		})
	}
}
