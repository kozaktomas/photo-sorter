package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestFacesHandler_Apply_CreateMarker_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/markers": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"UID":     "marker-new",
				"FileUID": "file123",
				"SubjUID": "subj123",
				"Type":    "face",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"file_uid": "file123",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	if response.MarkerUID != "marker-new" {
		t.Errorf("expected marker_uid 'marker-new', got '%s'", response.MarkerUID)
	}
}

func TestFacesHandler_Apply_CreateMarker_MissingFileUID(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "file_uid and bbox_rel are required for create_marker")
}

func TestFacesHandler_Apply_CreateMarker_InvalidBBox(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"file_uid": "file123",
		"bbox_rel": [0.1, 0.2]
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "file_uid and bbox_rel are required for create_marker")
}

func TestFacesHandler_Apply_AssignPerson_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/markers/marker123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":     "marker123",
				"SubjUID": "subj123",
				"Name":    "John Doe",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	if response.MarkerUID != "marker123" {
		t.Errorf("expected marker_uid 'marker123', got '%s'", response.MarkerUID)
	}
}

func TestFacesHandler_Apply_AssignPerson_MissingMarkerUID(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "marker_uid is required for assign_person")
}

func TestFacesHandler_Apply_UnassignPerson_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/markers/marker123/subject": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":     "marker123",
				"SubjUID": "",
				"Name":    "",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "unassign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response ApplyResponse
	parseJSONResponse(t, recorder, &response)

	if !response.Success {
		t.Errorf("expected success=true, got %v", response.Success)
	}

	if response.MarkerUID != "marker123" {
		t.Errorf("expected marker_uid 'marker123', got '%s'", response.MarkerUID)
	}
}

func TestFacesHandler_Apply_UnassignPerson_MissingMarkerUID(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "unassign_person"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "marker_uid is required for unassign_person")
}

func TestFacesHandler_Apply_InvalidAction(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil)
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "invalid_action"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid action")
}

func TestFacesHandler_Apply_MissingPhotoUID(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid and person_name are required")
}

func TestFacesHandler_Apply_MissingPersonName(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid and person_name are required")
}

func TestFacesHandler_Apply_InvalidJSON(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_Apply_NoPhotoPrismClient(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "assign_person",
		"marker_uid": "marker123"
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.Apply(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_Apply_CreateMarker_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/markers": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{
		"photo_uid": "photo123",
		"person_name": "John Doe",
		"action": "create_marker",
		"file_uid": "file123",
		"bbox_rel": [0.1, 0.2, 0.15, 0.2]
	}`)
	req := httptest.NewRequest("POST", "/api/v1/faces/apply", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

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

	req := httptest.NewRequest("POST", "/api/v1/photos//faces/compute", nil)
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.ComputeFaces(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "photo_uid is required")
}

func TestFacesHandler_ComputeFaces_DatabaseNotConfigured(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequest("POST", "/api/v1/photos/photo123/faces/compute", nil)
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
		t.Errorf("expected error 'database not configured', got '%s'", response.Error)
	}
}
