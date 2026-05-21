package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// createFacesHandlerForTest creates a FacesHandler for tests that do not
// exercise the database / marker / subject / photo paths. All native
// repositories are left nil so individual tests can wire what they need.
func createFacesHandlerForTest(cfg *config.Config) *FacesHandler {
	return &FacesHandler{
		config:         cfg,
		sessionManager: nil,
		faceReader:     nil,
		faceWriter:     nil,
	}
}

// createFacesHandlerWithSubjects wires a FacesHandler with the supplied
// subject + marker repositories — used by subject CRUD and ListSubjects
// tests so they can exercise the native repo path end-to-end.
func createFacesHandlerWithSubjects(
	cfg *config.Config,
	subjectRepo database.SubjectWriter,
	markerRepo database.MarkerWriter,
) *FacesHandler {
	return &FacesHandler{
		config:      cfg,
		subjectRepo: subjectRepo,
		markerRepo:  markerRepo,
	}
}

func TestFacesHandler_ListSubjects_Success(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	markerRepo := newFakeMarkerRepo()
	subjectRepo.attachMarkers(markerRepo)

	subjectRepo.seed("subj1", "John Doe")
	subjectRepo.seed("subj2", "Jane Doe")
	// Add markers so John Doe has 50 photos and Jane Doe has 30.
	for i := range 50 {
		markerRepo.seed(database.Marker{
			PhotoUID: "p-john-" + intToStr(i), SubjectUID: "subj1", Type: "face",
		})
	}
	for i := range 30 {
		markerRepo.seed(database.Marker{
			PhotoUID: "p-jane-" + intToStr(i), SubjectUID: "subj2", Type: "face",
		})
	}

	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, markerRepo)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects", nil)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subjects []SubjectResponse
	parseJSONResponse(t, recorder, &subjects)

	if len(subjects) != 2 {
		t.Fatalf("expected 2 subjects, got %d", len(subjects))
	}

	byUID := map[string]SubjectResponse{}
	for _, s := range subjects {
		byUID[s.UID] = s
	}
	if got := byUID["subj1"]; got.Name != "John Doe" {
		t.Errorf("expected subj1 name 'John Doe', got %q", got.Name)
	}
	if got := byUID["subj1"]; got.PhotoCount != 50 {
		t.Errorf("expected subj1 photo_count 50, got %d", got.PhotoCount)
	}
	if got := byUID["subj2"]; got.PhotoCount != 30 {
		t.Errorf("expected subj2 photo_count 30, got %d", got.PhotoCount)
	}
}

func TestFacesHandler_ListSubjects_WithPagination(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	// Seed enough rows that pagination is observable.
	for i := range 5 {
		subjectRepo.seed("s"+intToStr(i), "Subject "+intToStr(i))
	}
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET",
		"/api/v1/subjects?count=2&offset=1", nil)
	recorder := httptest.NewRecorder()
	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestFacesHandler_ListSubjects_NoRepo(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects", nil)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "subject storage not available")
}

func TestFacesHandler_ListSubjects_RepoError(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	subjectRepo.ListError = errors.New("boom")
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects", nil)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get subjects")
}

func TestFacesHandler_GetSubject_Success(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	s := subjectRepo.seed("subj123", "John Doe")
	s.Favorite = true

	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects/subj123", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subject SubjectResponse
	parseJSONResponse(t, recorder, &subject)

	if subject.UID != "subj123" {
		t.Errorf("expected subject UID 'subj123', got %q", subject.UID)
	}
	if subject.Name != "John Doe" {
		t.Errorf("expected name 'John Doe', got %q", subject.Name)
	}
	if !subject.Favorite {
		t.Error("expected Favorite to be true")
	}
}

func TestFacesHandler_GetSubject_MissingUID(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestFacesHandler_GetSubject_NotFound(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/subjects/nope", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "nope"})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "subject not found")
}

func TestFacesHandler_UpdateSubject_Success(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	subjectRepo.seed("subj123", "John Doe")
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	body := bytes.NewBufferString(`{"name": "Updated Name", "favorite": true}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subject SubjectResponse
	parseJSONResponse(t, recorder, &subject)

	if subject.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", subject.Name)
	}
	if !subject.Favorite {
		t.Error("expected Favorite to be true")
	}
	// Verify the slug was regenerated from the new name.
	stored, err := subjectRepo.GetSubject(context.Background(), "subj123")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if stored.Slug != "updated-name" {
		t.Errorf("expected slug 'updated-name', got %q", stored.Slug)
	}
}

func TestFacesHandler_UpdateSubject_PartialUpdate(t *testing.T) {
	subjectRepo := newFakeSubjectRepo()
	subjectRepo.seed("subj123", "Original Name")
	handler := createFacesHandlerWithSubjects(testConfig(), subjectRepo, nil)

	body := bytes.NewBufferString(`{"notes": "fresh notes"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	stored, err := subjectRepo.GetSubject(context.Background(), "subj123")
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if stored.Name != "Original Name" {
		t.Errorf("expected name unchanged, got %q", stored.Name)
	}
	if stored.Notes != "fresh notes" {
		t.Errorf("expected notes 'fresh notes', got %q", stored.Notes)
	}
}

func TestFacesHandler_UpdateSubject_MissingUID(t *testing.T) {
	handler := createFacesHandlerWithSubjects(testConfig(), newFakeSubjectRepo(), nil)

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestFacesHandler_UpdateSubject_InvalidJSON(t *testing.T) {
	handler := createFacesHandlerWithSubjects(testConfig(), newFakeSubjectRepo(), nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_UpdateSubject_NoRepo(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "subject storage not available")
}

func TestFacesHandler_UpdateSubject_NotFound(t *testing.T) {
	handler := createFacesHandlerWithSubjects(testConfig(), newFakeSubjectRepo(), nil)

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequestWithContext(context.Background(), "PUT", "/api/v1/subjects/nope", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "nope"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "subject not found")
}
