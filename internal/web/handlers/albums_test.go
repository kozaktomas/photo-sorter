package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestAlbumsHandler_List_Success(t *testing.T) {
	albumsData := `[
		{"UID": "album1", "Title": "Album One", "Type": "album", "PhotoCount": 10},
		{"UID": "album2", "Title": "Album Two", "Type": "album", "PhotoCount": 5}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(albumsData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var albums []AlbumResponse
	parseJSONResponse(t, recorder, &albums)

	if len(albums) != 2 {
		t.Errorf("expected 2 albums, got %d", len(albums))
	}

	if albums[0].UID != "album1" {
		t.Errorf("expected first album UID 'album1', got '%s'", albums[0].UID)
	}

	if albums[0].Title != "Album One" {
		t.Errorf("expected first album title 'Album One', got '%s'", albums[0].Title)
	}
}

func TestAlbumsHandler_List_WithPagination(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("count") != "50" {
				t.Errorf("expected count=50, got %s", query.Get("count"))
			}
			if query.Get("offset") != "10" {
				t.Errorf("expected offset=10, got %s", query.Get("offset"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums?count=50&offset=10", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestAlbumsHandler_List_NoClient(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/albums", nil)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestAlbumsHandler_List_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get albums")
}

func TestAlbumsHandler_Get_Success(t *testing.T) {
	albumData := `{"UID": "album123", "Title": "Test Album", "Type": "album", "PhotoCount": 15}`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums/album123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(albumData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums/album123", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var album AlbumResponse
	parseJSONResponse(t, recorder, &album)

	if album.UID != "album123" {
		t.Errorf("expected album UID 'album123', got '%s'", album.UID)
	}

	if album.Title != "Test Album" {
		t.Errorf("expected title 'Test Album', got '%s'", album.Title)
	}
}

func TestAlbumsHandler_Get_MissingUID(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/albums/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing album UID")
}

func TestAlbumsHandler_Get_NotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums/nonexistent": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "album not found"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums/nonexistent", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "album not found")
}

func TestAlbumsHandler_Create_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
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

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"UID":   "new-album-uid",
				"Title": input.Title,
				"Type":  "album",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"title": "New Album"}`)
	req := httptest.NewRequest("POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	assertStatusCode(t, recorder, http.StatusCreated)
	assertContentType(t, recorder, "application/json")

	var album AlbumResponse
	parseJSONResponse(t, recorder, &album)

	if album.UID != "new-album-uid" {
		t.Errorf("expected album UID 'new-album-uid', got '%s'", album.UID)
	}

	if album.Title != "New Album" {
		t.Errorf("expected title 'New Album', got '%s'", album.Title)
	}
}

func TestAlbumsHandler_Create_MissingTitle(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"title": ""}`)
	req := httptest.NewRequest("POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "title is required")
}

func TestAlbumsHandler_Create_InvalidJSON(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/albums", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestAlbumsHandler_GetPhotos_Success(t *testing.T) {
	photosData := `[
		{"UID": "photo1", "Title": "Photo One", "Type": "image"},
		{"UID": "photo2", "Title": "Photo Two", "Type": "image"}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("s") != "album123" {
				t.Errorf("expected album query s=album123")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(photosData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/albums/album123/photos", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})
	recorder := httptest.NewRecorder()

	handler.GetPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var photos []PhotoResponse
	parseJSONResponse(t, recorder, &photos)

	if len(photos) != 2 {
		t.Errorf("expected 2 photos, got %d", len(photos))
	}
}

func TestAlbumsHandler_GetPhotos_MissingUID(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/albums//photos", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.GetPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing album UID")
}

func TestAlbumsHandler_AddPhotos_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
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

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"photo_uids": ["photo1", "photo2"]}`)
	req := httptest.NewRequest("POST", "/api/v1/albums/album123/photos", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})

	recorder := httptest.NewRecorder()

	handler.AddPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]int
	parseJSONResponse(t, recorder, &result)

	if result["added"] != 2 {
		t.Errorf("expected added=2, got %d", result["added"])
	}
}

func TestAlbumsHandler_AddPhotos_EmptyList(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"photo_uids": []}`)
	req := httptest.NewRequest("POST", "/api/v1/albums/album123/photos", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})

	recorder := httptest.NewRecorder()

	handler.AddPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uids is required")
}

func TestAlbumsHandler_AddPhotos_MissingUID(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"photo_uids": ["photo1"]}`)
	req := httptest.NewRequest("POST", "/api/v1/albums//photos", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.AddPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing album UID")
}

func TestAlbumsHandler_ClearPhotos_Success(t *testing.T) {
	photosData := `[
		{"UID": "photo1", "Type": "image"},
		{"UID": "photo2", "Type": "image"}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(photosData))
		},
		"/api/v1/albums/album123/photos": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "DELETE", "/api/v1/albums/album123/clear", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})
	recorder := httptest.NewRecorder()

	handler.ClearPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]int
	parseJSONResponse(t, recorder, &result)

	if result["removed"] != 2 {
		t.Errorf("expected removed=2, got %d", result["removed"])
	}
}

func TestAlbumsHandler_ClearPhotos_EmptyAlbum(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewAlbumsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "DELETE", "/api/v1/albums/album123/clear", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "album123"})
	recorder := httptest.NewRecorder()

	handler.ClearPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]int
	parseJSONResponse(t, recorder, &result)

	if result["removed"] != 0 {
		t.Errorf("expected removed=0, got %d", result["removed"])
	}
}

func TestAlbumsHandler_ClearPhotos_MissingUID(t *testing.T) {
	handler := NewAlbumsHandler(testConfig(), nil)

	req := httptest.NewRequest("DELETE", "/api/v1/albums//clear", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.ClearPhotos(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing album UID")
}
