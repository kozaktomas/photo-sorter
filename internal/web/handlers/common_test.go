package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespondJSON_SetsContentType(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := map[string]string{"status": "ok"}

	respondJSON(recorder, http.StatusOK, data)

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestRespondJSON_SetsStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK", http.StatusOK},
		{"Created", http.StatusCreated},
		{"BadRequest", http.StatusBadRequest},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			respondJSON(recorder, tc.statusCode, nil)

			if recorder.Code != tc.statusCode {
				t.Errorf("expected status %d, got %d", tc.statusCode, recorder.Code)
			}
		})
	}
}

func TestRespondJSON_EncodesData(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := map[string]interface{}{
		"message": "hello",
		"count":   42,
		"active":  true,
	}

	respondJSON(recorder, http.StatusOK, data)

	var result map[string]interface{}
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["message"] != "hello" {
		t.Errorf("expected message 'hello', got '%v'", result["message"])
	}

	if result["count"] != float64(42) { // JSON numbers are float64
		t.Errorf("expected count 42, got %v", result["count"])
	}

	if result["active"] != true {
		t.Errorf("expected active true, got %v", result["active"])
	}
}

func TestRespondJSON_NilData(t *testing.T) {
	recorder := httptest.NewRecorder()

	respondJSON(recorder, http.StatusOK, nil)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	// Body should be empty for nil data
	if recorder.Body.Len() != 0 {
		t.Errorf("expected empty body for nil data, got '%s'", recorder.Body.String())
	}
}

func TestRespondJSON_EmptyMap(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := map[string]string{}

	respondJSON(recorder, http.StatusOK, data)

	expected := "{}\n"
	if recorder.Body.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, recorder.Body.String())
	}
}

func TestRespondJSON_Array(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := []string{"one", "two", "three"}

	respondJSON(recorder, http.StatusOK, data)

	var result []string
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestRespondJSON_NestedStruct(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := struct {
		Name   string `json:"name"`
		Nested struct {
			Value int `json:"value"`
		} `json:"nested"`
	}{
		Name: "test",
	}
	data.Nested.Value = 123

	respondJSON(recorder, http.StatusOK, data)

	var result map[string]interface{}
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["name"] != "test" {
		t.Errorf("expected name 'test', got '%v'", result["name"])
	}

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("expected nested object")
	}

	if nested["value"] != float64(123) {
		t.Errorf("expected nested value 123, got %v", nested["value"])
	}
}

func TestRespondError_SetsStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"BadRequest", http.StatusBadRequest},
		{"Unauthorized", http.StatusUnauthorized},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			respondError(recorder, tc.statusCode, "test error")

			if recorder.Code != tc.statusCode {
				t.Errorf("expected status %d, got %d", tc.statusCode, recorder.Code)
			}
		})
	}
}

func TestRespondError_ContainsErrorKey(t *testing.T) {
	recorder := httptest.NewRecorder()
	errorMessage := "something went wrong"

	respondError(recorder, http.StatusBadRequest, errorMessage)

	var result map[string]string
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["error"] != errorMessage {
		t.Errorf("expected error '%s', got '%s'", errorMessage, result["error"])
	}
}

func TestRespondError_SetsContentType(t *testing.T) {
	recorder := httptest.NewRecorder()

	respondError(recorder, http.StatusBadRequest, "error")

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestRespondError_EmptyMessage(t *testing.T) {
	recorder := httptest.NewRecorder()

	respondError(recorder, http.StatusBadRequest, "")

	var result map[string]string
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Error key should exist but be empty
	if result["error"] != "" {
		t.Errorf("expected empty error message, got '%s'", result["error"])
	}
}

func TestHealthCheck_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	recorder := httptest.NewRecorder()

	HealthCheck(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestHealthCheck_ReturnsJSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	recorder := httptest.NewRecorder()

	HealthCheck(recorder, req)

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestHealthCheck_ReturnsStatusOk(t *testing.T) {
	req := httptest.NewRequest("GET", "/health", nil)
	recorder := httptest.NewRecorder()

	HealthCheck(recorder, req)

	var result map[string]string
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", result["status"])
	}
}

func TestHealthCheck_IgnoresHTTPMethod(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "HEAD"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			recorder := httptest.NewRecorder()

			HealthCheck(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Errorf("expected status %d for method %s, got %d", http.StatusOK, method, recorder.Code)
			}
		})
	}
}
