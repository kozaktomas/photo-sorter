package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// testConfig creates a minimal config for testing
func testConfig() *config.Config {
	return &config.Config{
		PhotoPrism: config.PhotoPrismConfig{
			URL: "http://localhost:2342",
		},
	}
}

// requestWithPhotoPrism creates a request with a PhotoPrism client in context
func requestWithPhotoPrism(t *testing.T, method, path string, pp *photoprism.PhotoPrism) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	ctx := middleware.SetPhotoPrismInContext(req.Context(), pp)
	return req.WithContext(ctx)
}

// requestWithChiParams creates a request with chi URL parameters
func requestWithChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// setupMockPhotoPrismServer creates a mock PhotoPrism server for handler tests
func setupMockPhotoPrismServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Mock auth endpoint (always needed for NewPhotoPrism)
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "test-session-id",
			"access_token": "test-token",
			"config": map[string]string{
				"downloadToken": "test-download-token",
				"previewToken":  "test-preview-token",
			},
			"user": map[string]string{
				"UID": "test-user-uid",
			},
		})
	})

	// Add custom handlers
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}

	return httptest.NewServer(mux)
}

// createPhotoPrismClient creates a PhotoPrism client connected to a mock server
func createPhotoPrismClient(t *testing.T, server *httptest.Server) *photoprism.PhotoPrism {
	t.Helper()
	pp, err := photoprism.NewPhotoPrism(server.URL, "test", "test")
	if err != nil {
		t.Fatalf("failed to create PhotoPrism client: %v", err)
	}
	return pp
}

// parseJSONResponse parses a JSON response body into the target type
func parseJSONResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nBody: %s", err, recorder.Body.String())
	}
}

// assertStatusCode checks if the response has the expected status code
func assertStatusCode(t *testing.T, recorder *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if recorder.Code != expected {
		t.Errorf("expected status %d, got %d\nBody: %s", expected, recorder.Code, recorder.Body.String())
	}
}

// assertContentType checks if the response has the expected content type
func assertContentType(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()
	ct := recorder.Header().Get("Content-Type")
	if ct != expected {
		t.Errorf("expected Content-Type '%s', got '%s'", expected, ct)
	}
}

// assertJSONError checks if the response is a JSON error with the expected message
func assertJSONError(t *testing.T, recorder *httptest.ResponseRecorder, expectedMessage string) {
	t.Helper()
	var result map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse error response: %v\nBody: %s", err, recorder.Body.String())
	}
	if result["error"] != expectedMessage {
		t.Errorf("expected error '%s', got '%s'", expectedMessage, result["error"])
	}
}
