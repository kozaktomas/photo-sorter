package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestProcessJobManager_GetActiveJob(t *testing.T) {
	manager := NewProcessJobManager()

	// Initially no active job
	if job := manager.GetActiveJob(); job != nil {
		t.Error("expected no active job initially")
	}

	// Set an active job
	testJob := &ProcessJob{
		ID:     "test-job-1",
		Status: JobStatusRunning,
	}
	manager.SetActiveJob(testJob)

	// Should return the active job
	if job := manager.GetActiveJob(); job == nil {
		t.Error("expected active job to be returned")
	} else if job.ID != "test-job-1" {
		t.Errorf("expected job ID 'test-job-1', got '%s'", job.ID)
	}
}

func TestProcessJobManager_GetJob(t *testing.T) {
	manager := NewProcessJobManager()

	testJob := &ProcessJob{
		ID:     "test-job-2",
		Status: JobStatusPending,
	}
	manager.SetActiveJob(testJob)

	// Should find the job by ID
	if job := manager.GetJob("test-job-2"); job == nil {
		t.Error("expected to find job by ID")
	}

	// Should not find non-existent job
	if job := manager.GetJob("non-existent"); job != nil {
		t.Error("expected nil for non-existent job")
	}
}

func TestProcessJobManager_ClearActiveJob(t *testing.T) {
	manager := NewProcessJobManager()

	testJob := &ProcessJob{
		ID:     "test-job-3",
		Status: JobStatusCompleted,
	}
	manager.SetActiveJob(testJob)

	// Verify job is set
	if manager.GetActiveJob() == nil {
		t.Error("expected active job before clearing")
	}

	// Clear the job
	manager.ClearActiveJob()

	// Verify job is cleared
	if manager.GetActiveJob() != nil {
		t.Error("expected no active job after clearing")
	}
}

func TestProcessJob_Listeners(t *testing.T) {
	job := &ProcessJob{
		ID:     "test-job-listeners",
		Status: JobStatusRunning,
	}

	// Add a listener
	ch := job.AddListener()
	if ch == nil {
		t.Fatal("expected channel from AddListener")
	}

	// Send an event
	go func() {
		job.SendEvent(JobEvent{Type: "test", Message: "hello"})
	}()

	// Receive the event
	event := <-ch
	if event.Type != "test" {
		t.Errorf("expected event type 'test', got '%s'", event.Type)
	}
	if event.Message != "hello" {
		t.Errorf("expected message 'hello', got '%s'", event.Message)
	}

	// Remove listener
	job.RemoveListener(ch)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after RemoveListener")
	}
}

func TestProcessJob_Cancel(t *testing.T) {
	job := &ProcessJob{
		ID:     "test-job-cancel",
		Status: JobStatusRunning,
	}

	// Add a listener to catch the cancelled event
	ch := job.AddListener()
	defer job.RemoveListener(ch)

	// Cancel the job
	job.Cancel()

	// Check status
	if job.Status != JobStatusCancelled {
		t.Errorf("expected status 'cancelled', got '%s'", job.Status)
	}

	// Check event
	event := <-ch
	if event.Type != "cancelled" {
		t.Errorf("expected event type 'cancelled', got '%s'", event.Type)
	}
}

func TestProcessHandler_Start_InvalidJSON(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/process", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	// Should fail because DATABASE_URL is not configured
	// The handler checks database initialization first
	assertStatusCode(t, recorder, http.StatusBadRequest)
}

func TestProcessHandler_Start_SkipBothOptions(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	// This test would need database to be initialized
	// For now, we test the validation logic by checking the error
	body := bytes.NewBufferString(`{"no_faces": true, "no_embeddings": true}`)
	req := httptest.NewRequest("POST", "/api/v1/process", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Start(recorder, req)

	// Will fail with DATABASE_URL not configured first
	assertStatusCode(t, recorder, http.StatusBadRequest)
}

func TestProcessHandler_Cancel_MissingJobID(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/v1/process/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing job ID")
}

func TestProcessHandler_Cancel_JobNotFound(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/v1/process/nonexistent", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "job not found")
}

func TestProcessHandler_Cancel_Success(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	// Create a job manually
	job := &ProcessJob{
		ID:     "cancel-test-job",
		Status: JobStatusRunning,
	}
	handler.jobManager.SetActiveJob(job)

	req := httptest.NewRequest("DELETE", "/api/v1/process/cancel-test-job", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "cancel-test-job"})
	recorder := httptest.NewRecorder()

	handler.Cancel(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]bool
	parseJSONResponse(t, recorder, &result)

	if !result["cancelled"] {
		t.Error("expected cancelled to be true")
	}

	if job.Status != JobStatusCancelled {
		t.Errorf("expected job status 'cancelled', got '%s'", job.Status)
	}
}

func TestProcessHandler_Events_MissingJobID(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/v1/process//events", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Events(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "missing job ID")
}

func TestProcessHandler_Events_JobNotFound(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("GET", "/api/v1/process/nonexistent/events", nil)
	req = requestWithChiParams(req, map[string]string{"jobId": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Events(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "job not found")
}

func TestProcessHandler_RebuildIndex_NoRebuilder(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/v1/process/rebuild-index", nil)
	recorder := httptest.NewRecorder()

	handler.RebuildIndex(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestProcessHandler_SyncCache_NoClient(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewProcessHandler(cfg, sm, nil, nil, nil)

	req := httptest.NewRequest("POST", "/api/v1/process/sync-cache", nil)
	recorder := httptest.NewRecorder()

	handler.SyncCache(recorder, req)

	// Should fail because no PhotoPrism client in context
	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestProcessJobOptions_Defaults(t *testing.T) {
	opts := ProcessJobOptions{}

	if opts.Concurrency != 0 {
		t.Errorf("expected default concurrency 0, got %d", opts.Concurrency)
	}

	if opts.Limit != 0 {
		t.Errorf("expected default limit 0, got %d", opts.Limit)
	}

	if opts.NoFaces {
		t.Error("expected no_faces to be false by default")
	}

	if opts.NoEmbeddings {
		t.Error("expected no_embeddings to be false by default")
	}
}

func TestProcessStartRequest_JSON(t *testing.T) {
	jsonData := `{"concurrency": 10, "limit": 100, "no_faces": true, "no_embeddings": false}`

	var req ProcessStartRequest
	if err := json.Unmarshal([]byte(jsonData), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if req.Concurrency != 10 {
		t.Errorf("expected concurrency 10, got %d", req.Concurrency)
	}

	if req.Limit != 100 {
		t.Errorf("expected limit 100, got %d", req.Limit)
	}

	if !req.NoFaces {
		t.Error("expected no_faces to be true")
	}

	if req.NoEmbeddings {
		t.Error("expected no_embeddings to be false")
	}
}

func TestProcessJobResult_JSON(t *testing.T) {
	result := ProcessJobResult{
		EmbedSuccess:    50,
		EmbedError:      2,
		FaceSuccess:     48,
		FaceError:       3,
		TotalNewFaces:   120,
		TotalEmbeddings: 1000,
		TotalFaces:      3000,
		TotalFacePhotos: 800,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed ProcessJobResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.EmbedSuccess != 50 {
		t.Errorf("expected embed_success 50, got %d", parsed.EmbedSuccess)
	}

	if parsed.TotalNewFaces != 120 {
		t.Errorf("expected total_new_faces 120, got %d", parsed.TotalNewFaces)
	}
}
