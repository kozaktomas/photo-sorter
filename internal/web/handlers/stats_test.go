package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatsHandler_Get_Success(t *testing.T) {
	photosData := `[
		{"UID": "photo1", "Type": "image"},
		{"UID": "photo2", "Type": "image"},
		{"UID": "photo3", "Type": "image"}
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
	handler := NewStatsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/stats", pp)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var stats StatsResponse
	parseJSONResponse(t, recorder, &stats)

	if stats.TotalPhotos != 3 {
		t.Errorf("expected total_photos=3, got %d", stats.TotalPhotos)
	}
}

func TestStatsHandler_Get_NoClient(t *testing.T) {
	handler := NewStatsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestStatsHandler_Get_Caching(t *testing.T) {
	callCount := 0
	photosData := `[
		{"UID": "photo1", "Type": "image"},
		{"UID": "photo2", "Type": "image"}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(photosData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewStatsHandler(testConfig(), nil)

	// First request - should fetch from PhotoPrism
	req1 := requestWithPhotoPrism(t, "GET", "/api/v1/stats", pp)
	recorder1 := httptest.NewRecorder()
	handler.Get(recorder1, req1)

	assertStatusCode(t, recorder1, http.StatusOK)
	firstCallCount := callCount

	// Second request - should use cache
	req2 := requestWithPhotoPrism(t, "GET", "/api/v1/stats", pp)
	recorder2 := httptest.NewRecorder()
	handler.Get(recorder2, req2)

	assertStatusCode(t, recorder2, http.StatusOK)

	// Should not have made additional API calls due to caching
	if callCount != firstCallCount {
		t.Errorf("expected no additional API calls, but got %d (was %d)", callCount, firstCallCount)
	}

	// Both responses should be identical
	var stats1, stats2 StatsResponse
	parseJSONResponse(t, recorder1, &stats1)
	parseJSONResponse(t, recorder2, &stats2)

	if stats1.TotalPhotos != stats2.TotalPhotos {
		t.Error("cached response should match original")
	}
}

func TestStatsHandler_Get_EmptyLibrary(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	// Create a new handler to avoid cache from other tests
	handler := NewStatsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/stats", pp)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var stats StatsResponse
	parseJSONResponse(t, recorder, &stats)

	if stats.TotalPhotos != 0 {
		t.Errorf("expected total_photos=0, got %d", stats.TotalPhotos)
	}

	if stats.PhotosProcessed != 0 {
		t.Errorf("expected photos_processed=0, got %d", stats.PhotosProcessed)
	}

	if stats.TotalFaces != 0 {
		t.Errorf("expected total_faces=0, got %d", stats.TotalFaces)
	}

	if stats.TotalEmbeddings != 0 {
		t.Errorf("expected total_embeddings=0, got %d", stats.TotalEmbeddings)
	}
}

func TestStatsResponse_Fields(t *testing.T) {
	// Test that all fields are present in the response
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/photos": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"UID": "photo1"}]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewStatsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/stats", pp)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]any
	parseJSONResponse(t, recorder, &result)

	expectedFields := []string{
		"total_photos",
		"photos_processed",
		"photos_with_embeddings",
		"photos_with_faces",
		"total_faces",
		"total_embeddings",
	}

	for _, field := range expectedFields {
		if _, ok := result[field]; !ok {
			t.Errorf("expected field '%s' in response", field)
		}
	}
}
