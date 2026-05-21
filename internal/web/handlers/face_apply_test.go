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

// createApplyHandler builds a FacesHandler wired with the supplied native
// marker + subject repositories for face_apply tests.
func createApplyHandler(
	cfg *config.Config, markerRepo database.MarkerWriter, subjectRepo database.SubjectWriter,
) *FacesHandler {
	return &FacesHandler{
		config:      cfg,
		markerRepo:  markerRepo,
		subjectRepo: subjectRepo,
	}
}

func TestFacesHandler_Apply_CreateMarker_Success(t *testing.T) {
	markerRepo := newFakeMarkerRepo()
	subjectRepo := newFakeSubjectRepo()
	handler := createApplyHandler(testConfig(), markerRepo, subjectRepo)

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"file_uid": "file123",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)
	if !response.Success {
		t.Errorf("expected success=true, got %v (err=%q)", response.Success, response.Error)
	}
	if response.MarkerUID == "" {
		t.Error("expected non-empty marker_uid")
	}
	// The marker row should now exist in the repo with the supplied bbox and
	// a subject UID resolved via EnsureSubject.
	m, err := markerRepo.GetMarker(context.Background(), response.MarkerUID)
	if err != nil {
		t.Fatalf("expected marker row to exist: %v", err)
	}
	if m.PhotoUID != "photo123" {
		t.Errorf("expected marker photo_uid 'photo123', got %q", m.PhotoUID)
	}
	if m.X != 0.1 || m.Y != 0.2 || m.W != 0.15 || m.H != 0.2 {
		t.Errorf("unexpected marker geometry: %+v", m)
	}
	if m.SubjectUID == "" {
		t.Error("expected subject UID to be populated via EnsureSubject")
	}
	// And the subject should be visible by name.
	subj, err := subjectRepo.GetSubjectByName(context.Background(), "John Doe")
	if err != nil {
		t.Fatalf("expected subject row: %v", err)
	}
	if subj.UID != m.SubjectUID {
		t.Errorf("marker subject_uid %q does not match subject row UID %q", m.SubjectUID, subj.UID)
	}
}

func TestFacesHandler_Apply_CreateMarker_WithExplicitSubjectUID(t *testing.T) {
	markerRepo := newFakeMarkerRepo()
	subjectRepo := newFakeSubjectRepo()
	subjectRepo.seed("subj-known", "John Doe")
	handler := createApplyHandler(testConfig(), markerRepo, subjectRepo)

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"subject_uid": "subj-known",
		"action": "create_marker",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)
	if !response.Success {
		t.Fatalf("expected success=true, got error=%q", response.Error)
	}

	m, err := markerRepo.GetMarker(context.Background(), response.MarkerUID)
	if err != nil {
		t.Fatalf("expected marker row: %v", err)
	}
	if m.SubjectUID != "subj-known" {
		t.Errorf("expected subject_uid 'subj-known', got %q", m.SubjectUID)
	}
}

func TestFacesHandler_Apply_CreateMarker_MissingBBox(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "file_uid and bbox_rel are required for create_marker")
}

func TestFacesHandler_Apply_CreateMarker_InvalidBBox(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"bbox_rel": [0.1, 0.2]
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "file_uid and bbox_rel are required for create_marker")
}

func TestFacesHandler_Apply_AssignPerson_Success(t *testing.T) {
	markerRepo := newFakeMarkerRepo()
	subjectRepo := newFakeSubjectRepo()
	markerRepo.seed(database.Marker{UID: "marker123", PhotoUID: "photo123", Type: "face"})
	handler := createApplyHandler(testConfig(), markerRepo, subjectRepo)

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)
	if !response.Success {
		t.Fatalf("expected success=true, got error=%q", response.Error)
	}
	if response.MarkerUID != "marker123" {
		t.Errorf("expected marker_uid 'marker123', got %q", response.MarkerUID)
	}

	m, err := markerRepo.GetMarker(context.Background(), "marker123")
	if err != nil {
		t.Fatalf("expected marker row: %v", err)
	}
	if m.SubjectUID == "" {
		t.Error("expected subject_uid to be populated after assign_person")
	}
}

func TestFacesHandler_Apply_AssignPerson_MissingMarkerUID(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "marker_uid is required for assign_person")
}

func TestFacesHandler_Apply_UnassignPerson_Success(t *testing.T) {
	markerRepo := newFakeMarkerRepo()
	markerRepo.seed(database.Marker{
		UID: "marker123", PhotoUID: "photo123", SubjectUID: "subj1", Type: "face",
	})
	handler := createApplyHandler(testConfig(), markerRepo, newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "unassign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)
	if !response.Success {
		t.Fatalf("expected success=true, got error=%q", response.Error)
	}
	if response.MarkerUID != "marker123" {
		t.Errorf("expected marker_uid 'marker123', got %q", response.MarkerUID)
	}

	m, err := markerRepo.GetMarker(context.Background(), "marker123")
	if err != nil {
		t.Fatalf("expected marker row: %v", err)
	}
	if m.SubjectUID != "" {
		t.Errorf("expected subject_uid to be cleared, got %q", m.SubjectUID)
	}
}

func TestFacesHandler_Apply_UnassignPerson_MissingMarkerUID(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "unassign_person"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "marker_uid is required for unassign_person")
}

func TestFacesHandler_Apply_InvalidAction(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "invalid_action"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid action")
}

func TestFacesHandler_Apply_MissingPhotoUID(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid and person_name are required")
}

func TestFacesHandler_Apply_MissingPersonName(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid and person_name are required")
}

func TestFacesHandler_Apply_InvalidJSON(t *testing.T) {
	handler := createApplyHandler(testConfig(), newFakeMarkerRepo(), newFakeSubjectRepo())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_Apply_NoMarkerRepo(t *testing.T) {
	handler := createApplyHandler(testConfig(), nil, newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusServiceUnavailable)
	assertJSONError(t, recorder, "marker storage not available")
}

func TestFacesHandler_Apply_CreateMarker_RepoError(t *testing.T) {
	markerRepo := newFakeMarkerRepo()
	markerRepo.CreateError = errors.New("create failed")
	handler := createApplyHandler(testConfig(), markerRepo, newFakeSubjectRepo())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"file_uid": "file123",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)
	if response.Success {
		t.Error("expected success=false")
	}
	if response.Error == "" {
		t.Error("expected error message")
	}
}

func TestFacesHandler_ComputeFaces_MissingUID(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos//faces/compute", nil)
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()
	handler.ComputeFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestFacesHandler_ComputeFaces_DatabaseNotConfigured(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/photos/photo123/faces/compute", nil)
	req = requestWithChiParams(req, map[string]string{"uid": "photo123"})

	recorder := httptest.NewRecorder()
	handler.ComputeFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response ComputeFacesResponse
	parseJSONResponse(t, recorder, &response)
	if response.Success {
		t.Error("expected success=false when database not configured")
	}
	if response.Error != "database not configured" {
		t.Errorf("expected error 'database not configured', got %q", response.Error)
	}
}
