package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/mock"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// createPhotosHandlerForTest creates a PhotosHandler for testing.
func createPhotosHandlerForTest(cfg *config.Config) *PhotosHandler {
	return &PhotosHandler{
		config:          cfg,
		sessionManager:  nil,
		embeddingReader: nil,
	}
}

// createPhotosHandlerWithEmbeddings creates a PhotosHandler with a mock embedding reader.
func createPhotosHandlerWithEmbeddings(cfg *config.Config, reader database.EmbeddingReader) *PhotosHandler {
	return &PhotosHandler{
		config:          cfg,
		sessionManager:  nil,
		embeddingReader: reader,
	}
}

func TestPhotosHandler_List_Success(t *testing.T) {
	photosData := `[
		{"UID": "photo1", "Title": "Photo One", "Type": "image", "Width": 1920, "Height": 1080},
		{"UID": "photo2", "Title": "Photo Two", "Type": "image", "Width": 3840, "Height": 2160}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(photosData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var photos []PhotoResponse
	parseJSONResponse(t, recorder, &photos)

	if len(photos) != 2 {
		t.Errorf("expected 2 photos, got %d", len(photos))
	}

	if photos[0].UID != "photo1" {
		t.Errorf("expected first photo UID 'photo1', got '%s'", photos[0].UID)
	}
}

func TestPhotosHandler_List_WithFilters(t *testing.T) {
	var receivedQuery string
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			receivedQuery = r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos?year=2024&label=vacation", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	// Check that filters were properly combined.
	if receivedQuery == "" {
		t.Error("expected q parameter with filters")
	}
}

func TestPhotosHandler_List_WithPagination(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("count") != "50" {
				t.Errorf("expected count=50, got %s", query.Get("count"))
			}
			if query.Get("offset") != "100" {
				t.Errorf("expected offset=100, got %s", query.Get("offset"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos?count=50&offset=100", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestPhotosHandler_List_NoClient(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/photos", nil)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestPhotosHandler_List_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get photos")
}

func TestPhotosHandler_Get_Success(t *testing.T) {
	photosData := `[{"UID": "photo123", "Title": "Test Photo", "Type": "image", "Width": 1920, "Height": 1080}]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("q")
			if q != "uid:photo123" {
				t.Errorf("expected q=uid:photo123, got %s", q)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(photosData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var photo PhotoResponse
	parseJSONResponse(t, recorder, &photo)

	if photo.UID != "photo123" {
		t.Errorf("expected photo UID 'photo123', got '%s'", photo.UID)
	}
}

func TestPhotosHandler_Get_MissingUID(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/photos/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing photo UID")
}

func TestPhotosHandler_Get_NotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`)) // Empty array = not found
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/nonexistent", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "photo not found")
}

func TestPhotosHandler_Update_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":         "photo123",
				"Title":       "Updated Title",
				"Description": "New description",
				"Type":        "image",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"title": "Updated Title", "description": "New description"}`)
	req := httptest.NewRequest("PUT", "/api/v1/photos/photo123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var photo PhotoResponse
	parseJSONResponse(t, recorder, &photo)

	if photo.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got '%s'", photo.Title)
	}
}

func TestPhotosHandler_Update_MissingUID(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"title": "Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/photos/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing photo UID")
}

func TestPhotosHandler_Update_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("PUT", "/api/v1/photos/photo123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_Thumbnail_Success(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"UID": "photo123", "Hash": "testhash123", "Type": "image"}]`))
		},
		"/api/v1/t/testhash123/test-download-token/fit_1280": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(imageData)
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/photo123/thumb/fit_1280", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123", "size": "fit_1280"})
	recorder := httptest.NewRecorder()

	handler.Thumbnail(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	if recorder.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected Content-Type 'image/png', got '%s'", recorder.Header().Get("Content-Type"))
	}

	if recorder.Body.Len() != 4 {
		t.Errorf("expected 4 bytes of data, got %d", recorder.Body.Len())
	}
}

func TestPhotosHandler_Thumbnail_InvalidSize(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/photos/photo123/thumb/invalid_size", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123", "size": "invalid_size"})
	recorder := httptest.NewRecorder()

	handler.Thumbnail(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid size")
}

func TestPhotosHandler_Thumbnail_MissingParams(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/photos//thumb/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Thumbnail(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing photo UID or size")
}

func TestPhotosHandler_Thumbnail_PhotoNotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/photos/nonexistent/thumb/fit_1280", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "nonexistent", "size": "fit_1280"})
	recorder := httptest.NewRecorder()

	handler.Thumbnail(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "photo not found")
}

func TestPhotosHandler_BatchAddLabels_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos/photo1/label": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo1"}`))
		},
		"/api/v1/photos/photo2/label": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"UID": "photo2"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uids": ["photo1", "photo2"], "label": "vacation"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response BatchAddLabelsResponse
	parseJSONResponse(t, recorder, &response)

	if response.Updated != 2 {
		t.Errorf("expected updated=2, got %d", response.Updated)
	}
}

func TestPhotosHandler_BatchAddLabels_MissingPhotoUIDs(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"label": "vacation"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uids is required")
}

func TestPhotosHandler_BatchAddLabels_MissingLabel(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uids": ["photo1"]}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "label is required")
}

func TestPhotosHandler_BatchAddLabels_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/batch/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchAddLabels(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilar_NoEmbedding(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"photo_uid": "photo123"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "embeddings not available")
}

func TestPhotosHandler_FindSimilar_MissingPhotoUID(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestPhotosHandler_FindSimilar_PhotoNotFound(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{"photo_uid": "nonexistent"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "no embedding found for this photo. Run 'photo info --embedding' first")
}

func TestPhotosHandler_FindSimilar_Success(t *testing.T) {
	mockReader := mock.NewMockEmbeddingReader()
	// Add source and similar embeddings.
	mockReader.AddEmbedding(database.StoredEmbedding{
		PhotoUID:  "photo1",
		Embedding: make([]float32, 768),
	})
	mockReader.AddEmbedding(database.StoredEmbedding{
		PhotoUID:  "photo2",
		Embedding: make([]float32, 768),
	})

	handler := createPhotosHandlerWithEmbeddings(testConfig(), mockReader)

	body := bytes.NewBufferString(`{"photo_uid": "photo1", "limit": 10, "threshold": 0.5}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response SimilarResponse
	parseJSONResponse(t, recorder, &response)

	if response.SourcePhotoUID != "photo1" {
		t.Errorf("expected source_photo_uid 'photo1', got '%s'", response.SourcePhotoUID)
	}
}

func TestPhotosHandler_FindSimilar_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilar(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_SearchByText_EmptyText(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": ""}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "text is required")
}

func TestPhotosHandler_SearchByText_WhitespaceText(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": "   "}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "text is required")
}

func TestPhotosHandler_SearchByText_NoEmbeddings(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"text": "sunset beach"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "embeddings not available")
}

func TestPhotosHandler_SearchByText_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/search-by-text", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.SearchByText(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilarToCollection_InvalidJSON(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestPhotosHandler_FindSimilarToCollection_MissingSourceType(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_id": "vacation"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_type is required")
}

func TestPhotosHandler_FindSimilarToCollection_MissingSourceID(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_type": "label"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_id is required")
}

func TestPhotosHandler_FindSimilarToCollection_InvalidSourceType(t *testing.T) {
	handler := createPhotosHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"source_type": "invalid", "source_id": "test"}`)
	req := httptest.NewRequest("POST", "/api/v1/photos/similar-to-collection", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.FindSimilarToCollection(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "source_type must be 'label' or 'album'")
}
