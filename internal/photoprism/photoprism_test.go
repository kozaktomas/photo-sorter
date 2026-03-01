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

	// Mock auth endpoint.
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(sessionData)
	})

	// Mock logout endpoint.
	mux.HandleFunc("/api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Mock get album endpoint.
	mux.HandleFunc("/api/v1/albums/at8e94h6pa15hbk7", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(albumData)
	})

	// Mock get photos endpoint.
	mux.HandleFunc("/api/v1/photos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(photosData)
	})

	// Mock get labels endpoint.
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

	// Verify tokens were parsed from session response.
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

	// Verify we have tokens.
	if pp.token == "" {
		t.Fatal("expected token to be set before logout")
	}

	// Logout.
	err = pp.Logout()
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Verify tokens are cleared.
	if pp.token != "" {
		t.Errorf("expected token to be empty after logout, got '%s'", pp.token)
	}

	if pp.downloadToken != "" {
		t.Errorf("expected downloadToken to be empty after logout, got '%s'", pp.downloadToken)
	}

	// Logout again should be no-op.
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

	// Check first label.
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

	// Check first photo.
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

	// Verify we can count photos correctly.
	count := len(photos)
	if count != 17 {
		t.Errorf("expected photo count 17, got %d", count)
	}

	// Verify all photos have valid UIDs.
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

	// Find a photo with caption.
	var photoWithCaption *Photo
	for i := range photos {
		if photos[i].Caption != "" {
			photoWithCaption = &photos[i]
			break
		}
	}

	if photoWithCaption == nil {
		t.Fatal("expected to find at least one photo with caption")
		return
	}

	if photoWithCaption.Caption == "" {
		t.Error("expected non-empty caption")
	}

	// Find a portrait photo.
	var portraitPhoto *Photo
	for i := range photos {
		if photos[i].Height > photos[i].Width {
			portraitPhoto = &photos[i]
			break
		}
	}

	if portraitPhoto == nil {
		t.Fatal("expected to find at least one portrait photo")
		return
	}

	if portraitPhoto.Width >= portraitPhoto.Height {
		t.Error("portrait photo should have height > width")
	}
}

func setupErrorServer(statusCode int, body string) *httptest.Server {
	mux := http.NewServeMux()

	// Auth always succeeds.
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "test-session-id",
			"access_token": "test-token",
			"config": map[string]string{
				"downloadToken": "test-download-token",
				"previewToken":  "test-preview-token",
			},
		})
	})

	// All other endpoints return the error.
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
	// Use a port that's unlikely to be in use.
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

// --- GetAlbums tests ---

func TestGetAlbums(t *testing.T) {
	albumsData := `[
		{"UID": "album1", "Title": "Album One", "Type": "album", "PhotoCount": 10},
		{"UID": "album2", "Title": "Album Two", "Type": "album", "PhotoCount": 5}
	]`

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// Verify query parameters.
			query := r.URL.Query()
			if query.Get("count") != "100" {
				t.Errorf("expected count=100, got %s", query.Get("count"))
			}
			if query.Get("offset") != "0" {
				t.Errorf("expected offset=0, got %s", query.Get("offset"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(albumsData))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	albums, err := pp.GetAlbums(100, 0, "", "", "")
	if err != nil {
		t.Fatalf("GetAlbums failed: %v", err)
	}

	if len(albums) != 2 {
		t.Errorf("expected 2 albums, got %d", len(albums))
	}

	if albums[0].UID != "album1" {
		t.Errorf("expected first album UID 'album1', got '%s'", albums[0].UID)
	}

	if albums[0].Title != "Album One" {
		t.Errorf("expected first album title 'Album One', got '%s'", albums[0].Title)
	}

	if albums[0].PhotoCount != 10 {
		t.Errorf("expected first album photo count 10, got %d", albums[0].PhotoCount)
	}
}

func TestGetAlbums_WithFilters(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("type") != "album" {
				t.Errorf("expected type=album, got %s", query.Get("type"))
			}
			if query.Get("order") != "newest" {
				t.Errorf("expected order=newest, got %s", query.Get("order"))
			}
			if query.Get("q") != "vacation" {
				t.Errorf("expected q=vacation, got %s", query.Get("q"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbums(100, 0, "newest", "vacation", "album")
	if err != nil {
		t.Fatalf("GetAlbums with filters failed: %v", err)
	}
}

func TestGetAlbums_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetAlbums(100, 0, "", "", "")
	if err == nil {
		t.Fatal("expected error for not found")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to contain '404', got: %v", err)
	}
}

// --- CreateAlbum tests ---

func TestCreateAlbum(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var input struct {
				Title string `json:"Title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if input.Title != "New Album" {
				t.Errorf("expected title 'New Album', got '%s'", input.Title)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"UID": "new-album-uid", "Title": "New Album", "Type": "album"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	album, err := pp.CreateAlbum("New Album")
	if err != nil {
		t.Fatalf("CreateAlbum failed: %v", err)
	}

	if album.UID != "new-album-uid" {
		t.Errorf("expected album UID 'new-album-uid', got '%s'", album.UID)
	}

	if album.Title != "New Album" {
		t.Errorf("expected album title 'New Album', got '%s'", album.Title)
	}
}

func TestCreateAlbum_ServerError(t *testing.T) {
	server := setupErrorServer(http.StatusInternalServerError, `{"error": "internal error"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.CreateAlbum("New Album")
	if err == nil {
		t.Fatal("expected error for server error")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got: %v", err)
	}
}

// --- AddPhotosToAlbum / RemovePhotosFromAlbum tests ---

func TestAddPhotosToAlbum(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/albums/album123/photos": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var input struct {
				Photos []string `json:"photos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if len(input.Photos) != 2 {
				t.Errorf("expected 2 photos, got %d", len(input.Photos))
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.AddPhotosToAlbum("album123", []string{"photo1", "photo2"})
	if err != nil {
		t.Fatalf("AddPhotosToAlbum failed: %v", err)
	}
}

func TestAddPhotosToAlbum_EmptyList(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Should return nil without making request.
	err = pp.AddPhotosToAlbum("album123", []string{})
	if err != nil {
		t.Fatalf("AddPhotosToAlbum with empty list failed: %v", err)
	}
}

func TestRemovePhotosFromAlbum(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/albums/album123/photos": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var input struct {
				Photos []string `json:"photos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if len(input.Photos) != 3 {
				t.Errorf("expected 3 photos, got %d", len(input.Photos))
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.RemovePhotosFromAlbum("album123", []string{"photo1", "photo2", "photo3"})
	if err != nil {
		t.Fatalf("RemovePhotosFromAlbum failed: %v", err)
	}
}

func TestRemovePhotosFromAlbum_EmptyList(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.RemovePhotosFromAlbum("album123", []string{})
	if err != nil {
		t.Fatalf("RemovePhotosFromAlbum with empty list failed: %v", err)
	}
}

// --- DeleteLabels tests ---

func TestDeleteLabels(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/batch/labels/delete": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var input struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if len(input.Labels) != 2 {
				t.Errorf("expected 2 labels, got %d", len(input.Labels))
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.DeleteLabels([]string{"label1", "label2"})
	if err != nil {
		t.Fatalf("DeleteLabels failed: %v", err)
	}
}

func TestDeleteLabels_EmptyList(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.DeleteLabels([]string{})
	if err != nil {
		t.Fatalf("DeleteLabels with empty list failed: %v", err)
	}
}

func TestDeleteLabels_ServerError(t *testing.T) {
	server := setupErrorServer(http.StatusInternalServerError, `{"error": "internal error"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.DeleteLabels([]string{"label1"})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

// --- EditPhoto tests ---

func TestEditPhoto(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var update PhotoUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if update.Title == nil || *update.Title != "New Title" {
				t.Errorf("expected title 'New Title', got %v", update.Title)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "New Title", "Type": "image"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	title := "New Title"
	photo, err := pp.EditPhoto("photo123", PhotoUpdate{Title: &title})
	if err != nil {
		t.Fatalf("EditPhoto failed: %v", err)
	}

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}

	if photo.Title != "New Title" {
		t.Errorf("expected title 'New Title', got '%s'", photo.Title)
	}
}

func TestEditPhoto_MultipleFields(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var update PhotoUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if update.Title == nil || *update.Title != "Updated" {
				t.Errorf("expected title 'Updated'")
			}
			if update.Description == nil || *update.Description != "A description" {
				t.Errorf("expected description 'A description'")
			}
			if update.Favorite == nil || *update.Favorite != true {
				t.Errorf("expected favorite to be true")
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "Updated", "Description": "A description", "Favorite": true}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	title := "Updated"
	desc := "A description"
	fav := true
	_, err = pp.EditPhoto("photo123", PhotoUpdate{
		Title:       &title,
		Description: &desc,
		Favorite:    &fav,
	})
	if err != nil {
		t.Fatalf("EditPhoto failed: %v", err)
	}
}

func TestEditPhoto_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "photo not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	title := "New Title"
	_, err = pp.EditPhoto("nonexistent", PhotoUpdate{Title: &title})
	if err == nil {
		t.Fatal("expected error for not found")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to contain '404', got: %v", err)
	}
}

// --- Photo label operations tests ---

func TestAddPhotoLabel(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123/label": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var label PhotoLabel
			if err := json.NewDecoder(r.Body).Decode(&label); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if label.Name != "vacation" {
				t.Errorf("expected label name 'vacation', got '%s'", label.Name)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "Photo", "Type": "image"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photo, err := pp.AddPhotoLabel("photo123", PhotoLabel{Name: "vacation", Uncertainty: 10})
	if err != nil {
		t.Fatalf("AddPhotoLabel failed: %v", err)
	}

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}
}

func TestRemovePhotoLabel(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123/label/label456": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "Photo", "Type": "image"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photo, err := pp.RemovePhotoLabel("photo123", "label456")
	if err != nil {
		t.Fatalf("RemovePhotoLabel failed: %v", err)
	}

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}
}

func TestUpdatePhotoLabel(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123/label/label456": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var label PhotoLabel
			if err := json.NewDecoder(r.Body).Decode(&label); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if label.Uncertainty != 5 {
				t.Errorf("expected uncertainty 5, got %d", label.Uncertainty)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "Photo", "Type": "image"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photo, err := pp.UpdatePhotoLabel("photo123", "label456", PhotoLabel{Uncertainty: 5})
	if err != nil {
		t.Fatalf("UpdatePhotoLabel failed: %v", err)
	}

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}
}

// --- GetPhotos tests ---

func TestGetPhotos(t *testing.T) {
	server := setupMockServer(t)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photos, err := pp.GetPhotos(100, 0)
	if err != nil {
		t.Fatalf("GetPhotos failed: %v", err)
	}

	// The mock server returns the album photos data which has 17 photos.
	if len(photos) != 17 {
		t.Errorf("expected 17 photos, got %d", len(photos))
	}
}

func TestGetPhotosWithQuery(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("q") != "label:vacation" {
				t.Errorf("expected q=label:vacation, got %s", query.Get("q"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"UID": "photo1", "Type": "image"}]`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photos, err := pp.GetPhotosWithQuery(100, 0, "label:vacation")
	if err != nil {
		t.Fatalf("GetPhotosWithQuery failed: %v", err)
	}

	if len(photos) != 1 {
		t.Errorf("expected 1 photo, got %d", len(photos))
	}
}

func TestGetPhotosWithQueryAndOrder(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("q") != "year:2024" {
				t.Errorf("expected q=year:2024, got %s", query.Get("q"))
			}
			if query.Get("order") != "newest" {
				t.Errorf("expected order=newest, got %s", query.Get("order"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetPhotosWithQueryAndOrder(100, 0, "year:2024", "newest")
	if err != nil {
		t.Fatalf("GetPhotosWithQueryAndOrder failed: %v", err)
	}
}

// --- GetPhotoDetails tests ---

func TestGetPhotoDetails(t *testing.T) {
	photoDetailsData := loadTestData(t, "photos_pt8sur39icikrn19_details_20260118_230600.json")

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/pt8sur39icikrn19": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(photoDetailsData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	details, err := pp.GetPhotoDetails("pt8sur39icikrn19")
	if err != nil {
		t.Fatalf("GetPhotoDetails failed: %v", err)
	}

	if details["UID"] != "pt8sur39icikrn19" {
		t.Errorf("expected UID 'pt8sur39icikrn19', got '%v'", details["UID"])
	}

	if details["Type"] != "image" {
		t.Errorf("expected Type 'image', got '%v'", details["Type"])
	}

	// Check nested Files array.
	files, ok := details["Files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatal("expected non-empty Files array")
	}

	// Check Labels array.
	labels, ok := details["Labels"].([]any)
	if !ok || len(labels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(labels))
	}
}

func TestGetPhotoDetails_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "photo not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetPhotoDetails("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- GetPhotoFileUID tests ---

func TestGetPhotoFileUID(t *testing.T) {
	photoDetailsData := loadTestData(t, "photos_pt8sur39icikrn19_details_20260118_230600.json")

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/pt8sur39icikrn19": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(photoDetailsData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	fileUID, err := pp.GetPhotoFileUID("pt8sur39icikrn19")
	if err != nil {
		t.Fatalf("GetPhotoFileUID failed: %v", err)
	}

	if fileUID != "ft8sur3ptsof6hj0" {
		t.Errorf("expected file UID 'ft8sur3ptsof6hj0', got '%s'", fileUID)
	}
}

func TestGetPhotoFileUID_NoFiles(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Files": []}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.GetPhotoFileUID("photo123")
	if err == nil {
		t.Fatal("expected error for photo with no files")
	}

	if !strings.Contains(err.Error(), "could not find file UID") {
		t.Errorf("expected 'could not find file UID' error, got: %v", err)
	}
}

// --- ApprovePhoto tests ---

func TestApprovePhoto(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123/approve": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Title": "Approved Photo", "Type": "image"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	photo, err := pp.ApprovePhoto("photo123")
	if err != nil {
		t.Fatalf("ApprovePhoto failed: %v", err)
	}

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}
}

func TestApprovePhoto_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "photo not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.ApprovePhoto("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- GetFaces tests ---

func TestGetFaces(t *testing.T) {
	facesData := `[
		{"ID": "face1", "MarkerUID": "marker1", "Name": "Person One", "Samples": 5},
		{"ID": "face2", "MarkerUID": "marker2", "Name": "Person Two", "Samples": 3}
	]`

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/faces": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			query := r.URL.Query()
			if query.Get("count") != "100" {
				t.Errorf("expected count=100, got %s", query.Get("count"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(facesData))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	faces, err := pp.GetFaces(100, 0)
	if err != nil {
		t.Fatalf("GetFaces failed: %v", err)
	}

	if len(faces) != 2 {
		t.Errorf("expected 2 faces, got %d", len(faces))
	}

	if faces[0].ID != "face1" {
		t.Errorf("expected first face ID 'face1', got '%s'", faces[0].ID)
	}

	if faces[0].Name != "Person One" {
		t.Errorf("expected first face name 'Person One', got '%s'", faces[0].Name)
	}
}

// --- GetSubjects tests ---

func TestGetSubjects(t *testing.T) {
	subjectsData := `[
		{"UID": "subj1", "Name": "John Doe", "PhotoCount": 50},
		{"UID": "subj2", "Name": "Jane Doe", "PhotoCount": 30}
	]`

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			query := r.URL.Query()
			if query.Get("type") != "person" {
				t.Errorf("expected type=person, got %s", query.Get("type"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(subjectsData))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	subjects, err := pp.GetSubjects(100, 0)
	if err != nil {
		t.Fatalf("GetSubjects failed: %v", err)
	}

	if len(subjects) != 2 {
		t.Errorf("expected 2 subjects, got %d", len(subjects))
	}

	if subjects[0].UID != "subj1" {
		t.Errorf("expected first subject UID 'subj1', got '%s'", subjects[0].UID)
	}

	if subjects[0].Name != "John Doe" {
		t.Errorf("expected first subject name 'John Doe', got '%s'", subjects[0].Name)
	}
}

// --- GetPhotoMarkers tests ---

func TestGetPhotoMarkers(t *testing.T) {
	photoDetailsData := loadTestData(t, "photos_pt8sur39icikrn19_details_20260118_230600.json")

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/pt8sur39icikrn19": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(photoDetailsData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	markers, err := pp.GetPhotoMarkers("pt8sur39icikrn19")
	if err != nil {
		t.Fatalf("GetPhotoMarkers failed: %v", err)
	}

	if len(markers) != 2 {
		t.Errorf("expected 2 markers, got %d", len(markers))
	}

	// Check first marker.
	if markers[0].UID != "mt8sur3f4rl6r2ii" {
		t.Errorf("expected first marker UID 'mt8sur3f4rl6r2ii', got '%s'", markers[0].UID)
	}

	if markers[0].Name != "Person A" {
		t.Errorf("expected first marker name 'Person A', got '%s'", markers[0].Name)
	}

	if markers[0].Type != "face" {
		t.Errorf("expected first marker type 'face', got '%s'", markers[0].Type)
	}

	// Check position data.
	if markers[0].X < 0.4 || markers[0].X > 0.42 {
		t.Errorf("expected marker X around 0.41, got %f", markers[0].X)
	}
}

func TestGetPhotoMarkers_NoMarkers(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Files": [{"UID": "file1", "Markers": []}]}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	markers, err := pp.GetPhotoMarkers("photo123")
	if err != nil {
		t.Fatalf("GetPhotoMarkers failed: %v", err)
	}

	if len(markers) != 0 {
		t.Errorf("expected 0 markers, got %d", len(markers))
	}
}

// --- CreateMarker tests ---

func TestCreateMarker(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/markers": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var marker MarkerCreate
			if err := json.NewDecoder(r.Body).Decode(&marker); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if marker.FileUID != "file123" {
				t.Errorf("expected FileUID 'file123', got '%s'", marker.FileUID)
			}
			if marker.Type != "face" {
				t.Errorf("expected Type 'face', got '%s'", marker.Type)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"UID": "marker123", "FileUID": "file123", "Type": "face"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	marker, err := pp.CreateMarker(MarkerCreate{
		FileUID: "file123",
		Type:    "face",
		X:       0.5,
		Y:       0.5,
		W:       0.1,
		H:       0.1,
		Src:     "manual",
	})
	if err != nil {
		t.Fatalf("CreateMarker failed: %v", err)
	}

	if marker.UID != "marker123" {
		t.Errorf("expected marker UID 'marker123', got '%s'", marker.UID)
	}
}

func TestCreateMarker_ServerError(t *testing.T) {
	server := setupErrorServer(http.StatusInternalServerError, `{"error": "internal error"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.CreateMarker(MarkerCreate{FileUID: "file123", Type: "face"})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

// --- UpdateMarker tests ---

func TestUpdateMarker(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/markers/marker123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var update MarkerUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if update.Name != "John Doe" {
				t.Errorf("expected Name 'John Doe', got '%s'", update.Name)
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "marker123", "Name": "John Doe"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	marker, err := pp.UpdateMarker("marker123", MarkerUpdate{Name: "John Doe", SubjSrc: "manual"})
	if err != nil {
		t.Fatalf("UpdateMarker failed: %v", err)
	}

	if marker.UID != "marker123" {
		t.Errorf("expected marker UID 'marker123', got '%s'", marker.UID)
	}

	if marker.Name != "John Doe" {
		t.Errorf("expected marker name 'John Doe', got '%s'", marker.Name)
	}
}

func TestUpdateMarker_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "marker not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = pp.UpdateMarker("nonexistent", MarkerUpdate{Name: "Test"})
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- Download operations tests ---

func TestGetPhotoThumbnail(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/t/testhash/test-download-token/fit_1280": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(imageData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	data, contentType, err := pp.GetPhotoThumbnail("testhash", "fit_1280")
	if err != nil {
		t.Fatalf("GetPhotoThumbnail failed: %v", err)
	}

	if contentType != "image/jpeg" {
		t.Errorf("expected content type 'image/jpeg', got '%s'", contentType)
	}

	if len(data) != 4 {
		t.Errorf("expected 4 bytes of data, got %d", len(data))
	}
}

func TestGetPhotoThumbnail_NotFound(t *testing.T) {
	server := setupErrorServer(http.StatusNotFound, `{"error": "not found"}`)
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, _, err = pp.GetPhotoThumbnail("nonexistent", "fit_1280")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestGetFileDownload(t *testing.T) {
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/dl/filehash123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// Verify download token in query.
			if r.URL.Query().Get("t") != "test-download-token" {
				t.Errorf("expected download token in query")
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(imageData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	data, contentType, err := pp.GetFileDownload("filehash123")
	if err != nil {
		t.Fatalf("GetFileDownload failed: %v", err)
	}

	if contentType != "image/jpeg" {
		t.Errorf("expected content type 'image/jpeg', got '%s'", contentType)
	}

	if len(data) != 4 {
		t.Errorf("expected 4 bytes of data, got %d", len(data))
	}
}

func TestGetPhotoDownload(t *testing.T) {
	photoDetailsData := loadTestData(t, "photos_pt8sur39icikrn19_details_20260118_230600.json")
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0}

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/pt8sur39icikrn19": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(photoDetailsData)
		},
		"/api/v1/dl/ca75c4d9fbc7c063b2ac9b1d4e50c19d31ca6df0": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(imageData)
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	data, contentType, err := pp.GetPhotoDownload("pt8sur39icikrn19")
	if err != nil {
		t.Fatalf("GetPhotoDownload failed: %v", err)
	}

	if contentType != "image/jpeg" {
		t.Errorf("expected content type 'image/jpeg', got '%s'", contentType)
	}

	if len(data) != 4 {
		t.Errorf("expected 4 bytes of data, got %d", len(data))
	}
}

func TestGetPhotoDownload_NoFileHash(t *testing.T) {
	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo123", "Files": [{"UID": "file1"}]}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, _, err = pp.GetPhotoDownload("photo123")
	if err == nil {
		t.Fatal("expected error for photo with no file hash")
	}

	if !strings.Contains(err.Error(), "could not find file hash") {
		t.Errorf("expected 'could not find file hash' error, got: %v", err)
	}
}

// --- RemoveAllPhotoLabels tests ---

func TestRemoveAllPhotoLabels(t *testing.T) {
	photoDetailsData := loadTestData(t, "photos_pt8sur39icikrn19_details_20260118_230600.json")
	removedLabels := make(map[string]bool)

	server := setupMockServerWithHandlers(t, map[string]http.HandlerFunc{
		"/api/v1/photos/pt8sur39icikrn19": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(photoDetailsData)
		},
		"/api/v1/photos/pt8sur39icikrn19/label/248": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			removedLabels["248"] = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "pt8sur39icikrn19"}`))
		},
		"/api/v1/photos/pt8sur39icikrn19/label/22": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			removedLabels["22"] = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "pt8sur39icikrn19"}`))
		},
		"/api/v1/photos/pt8sur39icikrn19/label/19": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			removedLabels["19"] = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "pt8sur39icikrn19"}`))
		},
	})
	defer server.Close()

	pp, err := NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = pp.RemoveAllPhotoLabels("pt8sur39icikrn19")
	if err != nil {
		t.Fatalf("RemoveAllPhotoLabels failed: %v", err)
	}

	// The photo has 3 labels with LabelIDs: 248, 22, 19.
	if len(removedLabels) != 3 {
		t.Errorf("expected 3 labels to be removed, got %d", len(removedLabels))
	}
}

// --- Helper function for mock server with custom handlers ---

func setupMockServerWithHandlers(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Mock auth endpoint (always needed).
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "test-session-id",
			"access_token": "test-token",
			"config": map[string]string{
				"downloadToken": "test-download-token",
				"previewToken":  "test-preview-token",
			},
			"user": map[string]string{
				"UID": "test-user-uid",
			},
		})
	})

	// Add custom handlers.
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}

	return httptest.NewServer(mux)
}
