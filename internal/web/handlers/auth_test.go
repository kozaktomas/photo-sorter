package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

func TestAuthHandler_Login_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, nil) // Default /api/v1/sessions handler returns success
	defer server.Close()

	cfg := testConfig()
	cfg.PhotoPrism.URL = server.URL
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	body := bytes.NewBufferString(`{"username": "testuser", "password": "testpass"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Login(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var response LoginResponse
	parseJSONResponse(t, recorder, &response)

	if !response.Success {
		t.Error("expected success to be true")
	}

	if response.SessionID == "" {
		t.Error("expected session_id to be set")
	}

	if response.ExpiresAt == "" {
		t.Error("expected expires_at to be set")
	}
}

func TestAuthHandler_Login_MissingCredentials(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing username", `{"username": "", "password": "testpass"}`},
		{"missing password", `{"username": "testuser", "password": ""}`},
		{"missing both", `{"username": "", "password": ""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			sm := middleware.NewSessionManager("test-secret", nil)
			handler := NewAuthHandler(cfg, sm)

			body := bytes.NewBufferString(tt.body)
			req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.Login(recorder, req)

			assertStatusCode(t, recorder, http.StatusBadRequest)
			assertJSONError(t, recorder, "username and password are required")
		})
	}
}

func TestAuthHandler_Login_InvalidJSON(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	body := bytes.NewBufferString(`{invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Login(recorder, req)

	assertStatusCode(t, recorder, http.StatusBadRequest)
	assertJSONError(t, recorder, "invalid request body")
}

func TestAuthHandler_Login_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "invalid credentials"}`))
			return
		}
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.PhotoPrism.URL = server.URL
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	body := bytes.NewBufferString(`{"username": "baduser", "password": "badpass"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Login(recorder, req)

	assertStatusCode(t, recorder, http.StatusUnauthorized)

	var response LoginResponse
	parseJSONResponse(t, recorder, &response)

	if response.Success {
		t.Error("expected success to be false")
	}

	if response.Error != "invalid credentials" {
		t.Errorf("expected error 'invalid credentials', got '%s'", response.Error)
	}
}

func TestAuthHandler_Logout_Success(t *testing.T) {
	server := setupMockPhotoPrismServer(t, map[string]http.HandlerFunc{
		"/api/v1/sessions/test-token": func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "DELETE" {
				w.WriteHeader(http.StatusOK)
				return
			}
		},
	})
	defer server.Close()

	cfg := testConfig()
	cfg.PhotoPrism.URL = server.URL
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	// Create a session first.
	session, _ := sm.CreateSession("test-token", "test-download-token")

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	// Add session cookie.
	cookie := &http.Cookie{
		Name:  "photo_sorter_session",
		Value: session.ID + "." + signSessionID(sm, session.ID),
	}
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]bool
	parseJSONResponse(t, recorder, &result)

	if !result["success"] {
		t.Error("expected success to be true")
	}

	// Verify session was deleted.
	if sm.GetSession(session.ID) != nil {
		t.Error("expected session to be deleted")
	}
}

func TestAuthHandler_Logout_NoSession(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var result map[string]bool
	parseJSONResponse(t, recorder, &result)

	if !result["success"] {
		t.Error("expected success to be true even without session")
	}
}

func TestAuthHandler_Status_Authenticated(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	// Create a session.
	session, _ := sm.CreateSession("test-token", "test-download-token")

	req := httptest.NewRequest("GET", "/api/v1/auth/status", nil)
	// Add session cookie.
	cookie := &http.Cookie{
		Name:  "photo_sorter_session",
		Value: session.ID + "." + signSessionID(sm, session.ID),
	}
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)
	assertContentType(t, recorder, "application/json")

	var status StatusResponse
	parseJSONResponse(t, recorder, &status)

	if !status.Authenticated {
		t.Error("expected authenticated to be true")
	}

	if status.ExpiresAt == "" {
		t.Error("expected expires_at to be set")
	}
}

func TestAuthHandler_Status_Unauthenticated(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	req := httptest.NewRequest("GET", "/api/v1/auth/status", nil)
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var status StatusResponse
	parseJSONResponse(t, recorder, &status)

	if status.Authenticated {
		t.Error("expected authenticated to be false")
	}

	if status.ExpiresAt != "" {
		t.Error("expected expires_at to be empty")
	}
}

func TestAuthHandler_Status_ExpiredSession(t *testing.T) {
	cfg := testConfig()
	sm := middleware.NewSessionManager("test-secret", nil)
	handler := NewAuthHandler(cfg, sm)

	// Create a session but don't add it to the manager.
	// This simulates an invalid/expired session.
	req := httptest.NewRequest("GET", "/api/v1/auth/status", nil)
	cookie := &http.Cookie{
		Name:  "photo_sorter_session",
		Value: "invalid-session-id.invalid-signature",
	}
	req.AddCookie(cookie)
	recorder := httptest.NewRecorder()

	handler.Status(recorder, req)

	assertStatusCode(t, recorder, http.StatusOK)

	var status StatusResponse
	parseJSONResponse(t, recorder, &status)

	if status.Authenticated {
		t.Error("expected authenticated to be false for invalid session")
	}
}

// Helper to sign session ID (mirrors SessionManager's internal method).
func signSessionID(sm *middleware.SessionManager, sessionID string) string {
	// We need to use the session manager's SetSessionCookie to get the proper signature.
	// For testing, we'll create a response recorder and extract the cookie.
	w := httptest.NewRecorder()
	session := &middleware.Session{ID: sessionID}
	r := httptest.NewRequest("GET", "/", nil)
	sm.SetSessionCookie(w, r, session)
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "photo_sorter_session" {
			parts := bytes.SplitN([]byte(c.Value), []byte("."), 2)
			if len(parts) == 2 {
				return string(parts[1])
			}
		}
	}
	return ""
}
