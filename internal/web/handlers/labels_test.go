package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestLabelsHandler_List_Success(t *testing.T) {
	labelsData := `[
		{"UID": "label1", "Name": "Nature", "Slug": "nature", "PhotoCount": 50},
		{"UID": "label2", "Name": "People", "Slug": "people", "PhotoCount": 30}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(labelsData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var labels []LabelResponse
	parseJSONResponse(t, recorder, &labels)

	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}

	if labels[0].UID != "label1" {
		t.Errorf("expected first label UID 'label1', got '%s'", labels[0].UID)
	}

	if labels[0].Name != "Nature" {
		t.Errorf("expected first label name 'Nature', got '%s'", labels[0].Name)
	}
}

func TestLabelsHandler_List_WithPagination(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("count") != "100" {
				t.Errorf("expected count=100, got %s", query.Get("count"))
			}
			if query.Get("offset") != "50" {
				t.Errorf("expected offset=50, got %s", query.Get("offset"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels?count=100&offset=50", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestLabelsHandler_List_WithAllFlag(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if query.Get("all") != "true" {
				t.Errorf("expected all=true, got %s", query.Get("all"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels?all=true", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestLabelsHandler_List_NoClient(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/labels", nil)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}

func TestLabelsHandler_List_PhotoPrismError(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal error"}`))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels", pp)
	recorder := httptest.NewRecorder()

	handler.List(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertJSONError(t, recorder, "failed to get labels")
}

func TestLabelsHandler_Get_Success(t *testing.T) {
	labelsData := `[
		{"UID": "label1", "Name": "Nature", "Slug": "nature", "PhotoCount": 50},
		{"UID": "label2", "Name": "People", "Slug": "people", "PhotoCount": 30}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(labelsData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels/label1", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "label1"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var label LabelResponse
	parseJSONResponse(t, recorder, &label)

	if label.UID != "label1" {
		t.Errorf("expected label UID 'label1', got '%s'", label.UID)
	}

	if label.Name != "Nature" {
		t.Errorf("expected name 'Nature', got '%s'", label.Name)
	}
}

func TestLabelsHandler_Get_MissingUID(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	req := httptest.NewRequest("GET", "/api/v1/labels/", nil)
	req = requestWithChiParams(req, map[string]string{})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestLabelsHandler_Get_NotFound(t *testing.T) {
	labelsData := `[
		{"UID": "label1", "Name": "Nature", "Slug": "nature", "PhotoCount": 50}
	]`

	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(labelsData))
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	req := requestWithPhotoPrism(t, "GET", "/api/v1/labels/nonexistent", pp)
	req = requestWithChiParams(req, map[string]string{"uid": "nonexistent"})
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	assertStatusCode(t, recorder, http.StatusNotFound)
	assertJSONError(t, recorder, "label not found")
}

func TestLabelsHandler_Update_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels/label123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":      "label123",
				"Name":     "Updated Name",
				"Slug":     "updated-name",
				"Favorite": true,
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"name": "Updated Name", "favorite": true}`)
	req := httptest.NewRequest("PUT", "/api/v1/labels/label123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "label123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var label LabelResponse
	parseJSONResponse(t, recorder, &label)

	if label.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", label.Name)
	}
}

func TestLabelsHandler_Update_PartialUpdate(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/labels/label123": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			var update map[string]any
			json.NewDecoder(r.Body).Decode(&update)

			// Should only have description field
			if _, ok := update["Name"]; ok {
				t.Error("Name should not be in update")
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"UID":         "label123",
				"Name":        "Original Name",
				"Description": "New Description",
			})
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"description": "New Description"}`)
	req := httptest.NewRequest("PUT", "/api/v1/labels/label123", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)
	req = requestWithChiParams(req, map[string]string{"uid": "label123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestLabelsHandler_Update_MissingUID(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"name": "Updated"}`)
	req := httptest.NewRequest("PUT", "/api/v1/labels/", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "uid is required")
}

func TestLabelsHandler_Update_InvalidJSON(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("PUT", "/api/v1/labels/label123", body)
	req.Header.Set("Content-Type", "application/json")
	req = requestWithChiParams(req, map[string]string{"uid": "label123"})

	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestLabelsHandler_BatchDelete_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
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

			if len(input.Labels) != 3 {
				t.Errorf("expected 3 labels, got %d", len(input.Labels))
			}

			w.WriteHeader(http.StatusOK)
		},
	})
	defer server.Close()

	pp := createPhotoPrismClient(t, server)
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"uids": ["label1", "label2", "label3"]}`)
	req := httptest.NewRequest("DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.BatchDelete(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]int
	parseJSONResponse(t, recorder, &result)

	if result["deleted"] != 3 {
		t.Errorf("expected deleted=3, got %d", result["deleted"])
	}
}

func TestLabelsHandler_BatchDelete_EmptyList(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"uids": []}`)
	req := httptest.NewRequest("DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchDelete(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "no labels specified")
}

func TestLabelsHandler_BatchDelete_InvalidJSON(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{invalid}`)
	req := httptest.NewRequest("DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchDelete(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestLabelsHandler_BatchDelete_NoClient(t *testing.T) {
	handler := NewLabelsHandler(testConfig(), nil)

	body := bytes.NewBufferString(`{"uids": ["label1"]}`)
	req := httptest.NewRequest("DELETE", "/api/v1/labels", body)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()

	handler.BatchDelete(recorder, req)

	assertStatusCode(t, recorder, http.StatusInternalServerError)
}
