package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func createSortHandlerForTest(cfg *config.Config) *SortHandler {
	jm := NewJobManager()
	return NewSortHandler(cfg, nil, jm)
}

func TestSortHandler_Start_Success(t *testing.T) {
	albumData := `{"UID": "album123", "Title": "Test Album", "Type": "album"}`

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

	cfg := testConfig()
	cfg.OpenAI.Token = "test-token" // Set token so provider creation works
	handler := createSortHandlerForTest(cfg)

	body := bytes.NewBufferString(`{
		"album_uid": "album123",
		"provider": "openai",
		"dry_run": true
	}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	ctx = middleware.SetSessionInContext(ctx, &middleware.Session{
		Token:         "test-token",
		DownloadToken: "test-download-token",
	})
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusAccepted)
	assertContentType(t, recorder, "application/json")

	var result map[string]string
	parseJSONResponse(t, recorder, &result)

	if result["album_uid"] != "album123" {
		t.Errorf("expected album_uid 'album123', got '%s'", result["album_uid"])
	}

	if result["album_title"] != "Test Album" {
		t.Errorf("expected album_title 'Test Album', got '%s'", result["album_title"])
	}

	if result["status"] != "pending" {
		t.Errorf("expected status 'pending', got '%s'", result["status"])
	}

	if result["job_id"] == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestSortHandler_Start_MissingAlbumUID(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"provider": "openai"}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "album_uid is required")
}

func TestSortHandler_Start_InvalidJSON(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestSortHandler_Start_NoPhotoPrismClient(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"album_uid": "album123"}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestSortHandler_Start_AlbumNotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums/nonexistent": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "album not found"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createSortHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"album_uid": "nonexistent"}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "album not found")
}

func TestSortHandler_Start_DefaultProvider(t *testing.T) {
	albumData := `{"UID": "album123", "Title": "Test Album", "Type": "album"}`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/albums/album123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(albumData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)

	cfg := testConfig()
	cfg.OpenAI.Token = "test-token"
	handler := createSortHandlerForTest(cfg)

	// Request without provider - should default to openai.
	body := bytes.NewBufferString(`{"album_uid": "album123"}`)
	req := httptest.NewRequest("POST", "/api/v1/sort", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	ctx = middleware.SetSessionInContext(ctx, &middleware.Session{
		Token:         "test-token",
		DownloadToken: "test-download-token",
	})
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	assertStatusCode(t, recorder, http.StatusAccepted)
}

func TestSortHandler_Status_Success(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	// Create a job first.
	options := SortJobOptions{DryRun: true}
	job := handler.jobManager.CreateJob("test-job-id", "album123", "Test Album", options)

	req := httptest.NewRequest("GET", "/api/v1/sort/test-job-id/status", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "test-job-id"})
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var result map[string]any
	parseJSONResponse(t, recorder, &result)

	if result["id"] != job.ID {
		t.Errorf("expected job ID '%s', got '%v'", job.ID, result["id"])
	}

	if result["album_uid"] != "album123" {
		t.Errorf("expected album_uid 'album123', got '%v'", result["album_uid"])
	}
}

func TestSortHandler_Status_MissingJobID(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/sort//status", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing job ID")
}

func TestSortHandler_Status_NotFound(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/sort/nonexistent/status", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "job not found")
}

func TestSortHandler_Cancel_Success(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	// Create a job first.
	options := SortJobOptions{DryRun: true}
	handler.jobManager.CreateJob("test-job-id", "album123", "Test Album", options)

	req := httptest.NewRequest("DELETE", "/api/v1/sort/test-job-id", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "test-job-id"})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var result map[string]bool
	parseJSONResponse(t, recorder, &result)

	if !result["cancelled"] {
		t.Error("expected cancelled=true")
	}
}

func TestSortHandler_Cancel_MissingJobID(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("DELETE", "/api/v1/sort/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing job ID")
}

func TestSortHandler_Cancel_NotFound(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("DELETE", "/api/v1/sort/nonexistent", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "job not found")
}

func TestSortHandler_Events_MissingJobID(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/sort//events", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Events(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing job ID")
}

func TestSortHandler_Events_NotFound(t *testing.T) {
	handler := createSortHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/sort/nonexistent/events", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Events(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "job not found")
}

func TestStartRequest_Validation(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		expectStatus int
		expectError  string
	}{
		{
			name:         "valid request",
			body:         `{"album_uid": "album123", "provider": "ollama"}`,
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "missing album_uid",
			body:         `{"provider": "openai"}`,
			expectStatus: http.StatusBadRequest,
			expectError:  "album_uid is required",
		},
		{
			name:         "empty album_uid",
			body:         `{"album_uid": ""}`,
			expectStatus: http.StatusBadRequest,
			expectError:  "album_uid is required",
		},
		{
			name:         "dry_run flag",
			body:         `{"album_uid": "album123", "dry_run": true}`,
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "with limit",
			body:         `{"album_uid": "album123", "limit": 10}`,
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "with concurrency",
			body:         `{"album_uid": "album123", "concurrency": 3}`,
			expectStatus: http.StatusAccepted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			albumData := `{"UID": "album123", "Title": "Test Album", "Type": "album"}`
			server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
				"/api/v1/albums/album123": func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(albumData))
				},
			})
			defer server.Close()

			pp := createPhotoPrismClient(t, server)

			cfg := testConfig()
			cfg.OpenAI.Token = "test-token"
			cfg.Ollama.URL = "http://localhost:11434"
			cfg.Ollama.Model = "llama3.2-vision:11b"
			handler := createSortHandlerForTest(cfg)

			body := bytes.NewBufferString(tc.body)
			req := httptest.NewRequest("POST", "/api/v1/sort", body)
			req.Header.Set("Content-Type", "application/json")
			ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
			ctx = middleware.SetSessionInContext(ctx, &middleware.Session{
				Token:         "test-token",
				DownloadToken: "test-download-token",
			})
			req = req.WithContext(ctx)

			recorder := httptest.NewRecorder()

			handler.Start(recorder, req)

			assertStatusCode(t, recorder, tc.expectStatus)

			if tc.expectError != "" {
				assertJSONError(t, recorder, tc.expectError)
			}
		})
	}
}

func TestJobManager_CreateAndGet(t *testing.T) {
	jm := NewJobManager()

	options := SortJobOptions{
		DryRun:      true,
		Limit:       10,
		Provider:    "openai",
		Concurrency: 5,
	}

	job := jm.CreateJob("job123", "album456", "Test Album", options)

	if job.ID != "job123" {
		t.Errorf("expected job ID 'job123', got '%s'", job.ID)
	}

	if job.AlbumUID != "album456" {
		t.Errorf("expected album UID 'album456', got '%s'", job.AlbumUID)
	}

	if job.AlbumTitle != "Test Album" {
		t.Errorf("expected album title 'Test Album', got '%s'", job.AlbumTitle)
	}

	if job.Status != JobStatusPending {
		t.Errorf("expected status pending, got %v", job.Status)
	}

	// Get the job.
	retrieved := jm.GetJob("job123")
	if retrieved == nil {
		t.Fatal("expected to retrieve job")
		return
	}

	if retrieved.ID != job.ID {
		t.Error("retrieved job should match created job")
	}
}

func TestJobManager_GetNonexistent(t *testing.T) {
	jm := NewJobManager()

	job := jm.GetJob("nonexistent")
	if job != nil {
		t.Error("expected nil for nonexistent job")
	}
}
