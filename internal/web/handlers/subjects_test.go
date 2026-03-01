package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// createFacesHandlerForTest creates a FacesHandler for testing (without database dependencies).
func createFacesHandlerForTest(cfg *config.Config) *FacesHandler {
	return &FacesHandler{
		config:         cfg,
		sessionManager: nil,
		faceReader:     nil,
		faceWriter:     nil,
	}
}

func TestFacesHandler_ListSubjects_Success(t *testing.T) {
	subjectsData := `[
		{"UID": "subj1", "Name": "John Doe", "Slug": "john-doe", "PhotoCount": 50},
		{"UID": "subj2", "Name": "Jane Doe", "Slug": "jane-doe", "PhotoCount": 30}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(subjectsData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/subjects", pp)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subjects []SubjectResponse
	parseJSONResponse(t, recorder, &subjects)

	if len(subjects) != 2 {
		t.Errorf("expected 2 subjects, got %d", len(subjects))
	}

	if subjects[0].UID != "subj1" {
		t.Errorf("expected first subject UID 'subj1', got '%s'", subjects[0].UID)
	}

	if subjects[0].Name != "John Doe" {
		t.Errorf("expected first subject name 'John Doe', got '%s'", subjects[0].Name)
	}

	if subjects[0].Slug != "john-doe" {
		t.Errorf("expected first subject slug 'john-doe', got '%s'", subjects[0].Slug)
	}
}

func TestFacesHandler_ListSubjects_WithPagination(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("count") != "25" {
				t.Errorf("expected count=25, got %s", query.Get("count"))
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
	handler := createFacesHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/subjects?count=25&offset=10", pp)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestFacesHandler_ListSubjects_NoClient(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/subjects", nil)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_ListSubjects_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/subjects", pp)
	recorder := httptest.NewRecorder()

	handler.ListSubjects(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get subjects")
}

func TestFacesHandler_GetSubject_Success(t *testing.T) {
	subjectData := `{
		"UID": "subj123",
		"Name": "John Doe",
		"Slug": "john-doe",
		"PhotoCount": 50,
		"Favorite": true,
		"About": "A person"
	}`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects/subj123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(subjectData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/subjects/subj123", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subject SubjectResponse
	parseJSONResponse(t, recorder, &subject)

	if subject.UID != "subj123" {
		t.Errorf("expected subject UID 'subj123', got '%s'", subject.UID)
	}

	if subject.Name != "John Doe" {
		t.Errorf("expected name 'John Doe', got '%s'", subject.Name)
	}

	if !subject.Favorite {
		t.Error("expected Favorite to be true")
	}
}

func TestFacesHandler_GetSubject_MissingUID(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	req := httptest.NewRequest("GET", "/api/v1/subjects/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestFacesHandler_GetSubject_NotFound(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects/nonexistent": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "subject not found"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	req := requestWithPhotoPrism(t, "GET", "/api/v1/subjects/nonexistent", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.GetSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get subject")
}

func TestFacesHandler_UpdateSubject_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects/subj123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":      "subj123",
				"Name":     "Updated Name",
				"Slug":     "updated-name",
				"Favorite": true,
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"name": "Updated Name", "favorite": true}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var subject SubjectResponse
	parseJSONResponse(t, recorder, &subject)

	if subject.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", subject.Name)
	}

	if !subject.Favorite {
		t.Error("expected Favorite to be true")
	}
}

func TestFacesHandler_UpdateSubject_PartialUpdate(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects/subj123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var update map[string]any
			json.NewDecoder(r.Body).Decode(&update)

			// Should only have About field.
			if _, ok := update["Name"]; ok {
				t.Error("Name should not be in update")
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":   "subj123",
				"Name":  "Original Name",
				"About": "New about text",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"about": "New about text"}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestFacesHandler_UpdateSubject_MissingUID(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestFacesHandler_UpdateSubject_InvalidJSON(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestFacesHandler_UpdateSubject_NoClient(t *testing.T) {
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestFacesHandler_UpdateSubject_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/subjects/subj123": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := createFacesHandlerForTest(testConfig())

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/subjects/subj123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "subj123"})

	recorder := httptest.NewRecorder()

	handler.UpdateSubject(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to update subject")
}
