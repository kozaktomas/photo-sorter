package photoprism

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadTestData(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to load test data %s: %v", filename, err)
	}
	return data
}

func setupMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	sessionData := loadTestData(t, "sessions_20260107_203245.json")
	albumData := loadTestData(t, "albums_at8e94h6pa15hbk7_20260107_190750.json")
	photosData := loadTestData(t, "photos_album_at8e94h6pa15hbk7_offset_0_20260107_190750.json")
	labelsData := loadTestData(t, "labels_20260107_204156.json")

	mux := http.NewServeMux()

	// Mock auth endpoint
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(sessionData)
	})

	// Mock logout endpoint
	mux.HandleFunc("/api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Mock get album endpoint
	mux.HandleFunc("/api/v1/albums/at8e94h6pa15hbk7", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(albumData)
	})

	// Mock get photos endpoint
	mux.HandleFunc("/api/v1/photos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(photosData)
	})

	// Mock get labels endpoint
	mux.HandleFunc("/api/v1/labels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(labelsData)
	})

	return httptest.NewServer(mux)
}

func TestAuth(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("NewPhotoPrism failed: %v", err)
	}

	// Verify tokens were parsed from session response
	if pp.token == "" {
		t.Error("expected access token to be set")
	}

	if pp.downloadToken == "" {
		t.Error("expected download token to be set")
	}

	if pp.downloadToken != "downloadtoken123" {
		t.Errorf("expected downloadToken 'downloadtoken123', got '%s'", pp.downloadToken)
	}
}

func TestLogout(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("NewPhotoPrism failed: %v", err)
	}

	// Verify we have tokens
	if pp.token == "" {
		t.Fatal("expected token to be set before logout")
	}

	// Logout
	err = pp.Logout()
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Verify tokens are cleared
	if pp.token != "" {
		t.Errorf("expected token to be empty after logout, got '%s'", pp.token)
	}

	if pp.downloadToken != "" {
		t.Errorf("expected downloadToken to be empty after logout, got '%s'", pp.downloadToken)
	}

	// Logout again should be no-op
	err = pp.Logout()
	if err != nil {
		t.Fatalf("second Logout failed: %v", err)
	}
}

func TestGetLabels(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	labels, err := pp.GetLabels(1000, 0, false)
	if err != nil {
		t.Fatalf("GetLabels failed: %v", err)
	}

	if len(labels) != 22 {
		t.Errorf("expected 22 labels, got %d", len(labels))
	}

	// Check first label
	firstLabel := labels[0]
	if firstLabel.UID != "lt4l73hjorg9e6s8" {
		t.Errorf("expected first label UID 'lt4l73hjorg9e6s8', got '%s'", firstLabel.UID)
	}

	if firstLabel.Name != "Sdh" {
		t.Errorf("expected first label Name 'Sdh', got '%s'", firstLabel.Name)
	}

	if firstLabel.PhotoCount != 116 {
		t.Errorf("expected first label PhotoCount 116, got %d", firstLabel.PhotoCount)
	}
}

func TestGetAlbum(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	album, err := pp.GetAlbum("at8e94h6pa15hbk7")
	if err != nil {
		t.Fatalf("GetAlbum failed: %v", err)
	}

	if album.UID != "at8e94h6pa15hbk7" {
		t.Errorf("expected UID 'at8e94h6pa15hbk7', got '%s'", album.UID)
	}

	if album.Title != "Sports Event 1985" {
		t.Errorf("expected Title 'Sports Event 1985', got '%s'", album.Title)
	}

	if album.Type != "album" {
		t.Errorf("expected Type 'album', got '%s'", album.Type)
	}
}

func TestGetAlbumPhotos(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photos, err := pp.GetAlbumPhotos("at8e94h6pa15hbk7", 1000, 0)
	if err != nil {
		t.Fatalf("GetAlbumPhotos failed: %v", err)
	}

	if len(photos) != 17 {
		t.Errorf("expected 17 photos, got %d", len(photos))
	}

	// Check first photo
	firstPhoto := photos[0]
	if firstPhoto.UID != "pt8e94z35jw6c114" {
		t.Errorf("expected first photo UID 'pt8e94z35jw6c114', got '%s'", firstPhoto.UID)
	}

	if firstPhoto.Type != "image" {
		t.Errorf("expected Type 'image', got '%s'", firstPhoto.Type)
	}

	if firstPhoto.Width != 3271 {
		t.Errorf("expected Width 3271, got %d", firstPhoto.Width)
	}

	if firstPhoto.Height != 2047 {
		t.Errorf("expected Height 2047, got %d", firstPhoto.Height)
	}

	if firstPhoto.OriginalName != "S125.jpg" {
		t.Errorf("expected OriginalName 'S125.jpg', got '%s'", firstPhoto.OriginalName)
	}
}

func TestGetAlbumPhotos_CountsCorrectly(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photos, err := pp.GetAlbumPhotos("at8e94h6pa15hbk7", 1000, 0)
	if err != nil {
		t.Fatalf("GetAlbumPhotos failed: %v", err)
	}

	// Verify we can count photos correctly
	count := len(photos)
	if count != 17 {
		t.Errorf("expected photo count 17, got %d", count)
	}

	// Verify all photos have valid UIDs
	for i, photo := range photos {
		if photo.UID == "" {
			t.Errorf("photo %d has empty UID", i)
		}
	}
}

func TestPhotoFields(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photos, err := pp.GetAlbumPhotos("at8e94h6pa15hbk7", 1000, 0)
	if err != nil {
		t.Fatalf("GetAlbumPhotos failed: %v", err)
	}

	// Find a photo with caption
	var photoWithCaption *Photo
	for i := range photos {
		if photos[i].Caption != "" {
			photoWithCaption = &photos[i]
			break
		}
	}

	if photoWithCaption == nil {
		t.Fatal("expected to find at least one photo with caption")
	}

	if photoWithCaption.Caption == "" {
		t.Error("expected non-empty caption")
	}

	// Find a portrait photo
	var portraitPhoto *Photo
	for i := range photos {
		if photos[i].Height > photos[i].Width {
			portraitPhoto = &photos[i]
			break
		}
	}

	if portraitPhoto == nil {
		t.Fatal("expected to find at least one portrait photo")
	}

	if portraitPhoto.Width >= portraitPhoto.Height {
		t.Error("portrait photo should have height > width")
	}
}

func setupErrorServer(statusCode int, body string) *httptest.Server {
	mux := http.NewServeMux()

	// Auth always succeeds
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":           "test-session-id",
			"access_token": "test-token",
			"config": map[string]string{
				"downloadToken": "test-download-token",
				"previewToken":  "test-preview-token",
			},
		})
	})

	// All other endpoints return the error
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	})

	return httptest.NewServer(mux)
}

func TestGetAlbum_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "album not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbum("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent album")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to contain '404', got: %v", err)
	}
}

func TestGetAlbum_Unauthorized(t *testing.T) {
	server := setupErrorServer(http.StatusUnauthorized, `{"error": "unauthorized"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbum("at8e94h6pa15hbk7")
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to contain '401', got: %v", err)
	}
}

func TestGetAlbum_InternalServerError(t *testing.T) {
	server := setupErrorServer(http.StatusInternalServerError, `{"error": "internal server error"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbum("at8e94h6pa15hbk7")
	if err == nil {
		t.Fatal("expected error for server error")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got: %v", err)
	}
}

func TestGetAlbumPhotos_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "album not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbumPhotos("nonexistent", 100, 0)
	if err == nil {
		t.Fatal("expected error for non-existent album")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to contain '404', got: %v", err)
	}
}

func TestGetAlbumPhotos_Forbidden(t *testing.T) {
	server := setupErrorServer(http.StatusForbidden, `{"error": "access denied"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbumPhotos("at8e94h6pa15hbk7", 100, 0)
	if err == nil {
		t.Fatal("expected error for forbidden request")
	}

	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected error to contain '403', got: %v", err)
	}
}

func TestNewPhotoPrism_AuthFailure_InvalidJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`not valid json`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := NewPhotoPrism(server.URL, "bad", "credentials")
	if err == nil {
		t.Fatal("expected error for auth failure with invalid JSON")
	}

	if !strings.Contains(err.Error(), "authenticate") {
		t.Errorf("expected error to mention authentication, got: %v", err)
	}
}

func TestNewPhotoPrism_AuthFailure_ConnectionRefused(t *testing.T) {
	// Use a port that's unlikely to be in use
	_, err := NewPhotoPrism("http://localhost:59999", "test", "test")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}

	if !strings.Contains(err.Error(), "authenticate") {
		t.Errorf("expected error to mention authentication, got: %v", err)
	}
}

func TestGetAlbum_ServiceUnavailable(t *testing.T) {
	server := setupErrorServer(http.StatusServiceUnavailable, `{"error": "service unavailable"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbum("at8e94h6pa15hbk7")
	if err == nil {
		t.Fatal("expected error for service unavailable")
	}

	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to contain '503', got: %v", err)
	}
}
