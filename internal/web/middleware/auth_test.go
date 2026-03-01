package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)
	if sm == nil {
		t.Fatal("NewSessionManager returned nil")
		return
	}
	if sm.sessions == nil {
		t.Error("sessions map is nil")
	}
}

func TestSessionManager_CreateSession(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)

	session, err := sm.CreateSession("token123", "download456")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if session.ID == "" {
		t.Error("session ID is empty")
	}
	if session.Token != "token123" {
		t.Errorf("Token = %s, want token123", session.Token)
	}
	if session.DownloadToken != "download456" {
		t.Errorf("DownloadToken = %s, want download456", session.DownloadToken)
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Error("session expires in the past")
	}
}

func TestSessionManager_GetSession(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)

	session, _ := sm.CreateSession("token123", "download456")

	// Get existing session.
	retrieved := sm.GetSession(session.ID)
	if retrieved == nil {
		t.Fatal("GetSession() returned nil for existing session")
		return
	}
	if retrieved.Token != "token123" {
		t.Errorf("Token = %s, want token123", retrieved.Token)
	}

	// Get non-existing session.
	notFound := sm.GetSession("nonexistent-id")
	if notFound != nil {
		t.Error("GetSession() should return nil for non-existing session")
	}
}

func TestSessionManager_DeleteSession(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)

	session, _ := sm.CreateSession("token123", "download456")

	// Delete the session.
	sm.DeleteSession(session.ID)

	// Verify it's gone.
	retrieved := sm.GetSession(session.ID)
	if retrieved != nil {
		t.Error("GetSession() should return nil after deletion")
	}
}

func TestSessionManager_SetAndGetSessionCookie(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)
	session, _ := sm.CreateSession("token123", "download456")

	// Create a test response to capture the cookie.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	sm.SetSessionCookie(w, r, session)

	// Get the cookie from the response.
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No cookies set")
	}

	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("Session cookie not found")
		return
	}

	// Create a request with the cookie.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(sessionCookie)

	// Verify the session can be retrieved from the request.
	retrieved := sm.GetSessionFromRequest(req)
	if retrieved == nil {
		t.Fatal("GetSessionFromRequest() returned nil")
		return
	}
	if retrieved.ID != session.ID {
		t.Errorf("Session ID = %s, want %s", retrieved.ID, session.ID)
	}
}

func TestSessionManager_InvalidCookie(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)

	// Request with invalid cookie signature.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "invalid-session.invalid-signature",
	})

	session := sm.GetSessionFromRequest(req)
	if session != nil {
		t.Error("GetSessionFromRequest() should return nil for invalid signature")
	}
}

func TestSessionManager_BearerAuth(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)
	session, _ := sm.CreateSession("token123", "download456")

	// Request with Bearer token.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+session.ID)

	retrieved := sm.GetSessionFromRequest(req)
	if retrieved == nil {
		t.Fatal("GetSessionFromRequest() returned nil for Bearer auth")
		return
	}
	if retrieved.ID != session.ID {
		t.Errorf("Session ID = %s, want %s", retrieved.ID, session.ID)
	}
}

func TestRequireAuth(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)
	session, _ := sm.CreateSession("token123", "download456")

	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		// Verify session is in context.
		s := GetSessionFromContext(r.Context())
		if s == nil {
			t.Error("Session not found in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireAuth(sm)
	protectedHandler := middleware(testHandler)

	// Test with valid session.
	t.Run("valid session", func(t *testing.T) {
		handlerCalled = false
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+session.ID)

		protectedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}
		if !handlerCalled {
			t.Error("Handler was not called")
		}
	})

	// Test without session.
	t.Run("no session", func(t *testing.T) {
		handlerCalled = false
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)

		protectedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
		if handlerCalled {
			t.Error("Handler should not be called for unauthorized request")
		}
	})
}

func TestGetSessionFromContext(t *testing.T) {
	// Test with session in context.
	session := &Session{ID: "test123", Token: "token456"}
	ctx := context.WithValue(context.Background(), sessionContextKey, session)

	retrieved := GetSessionFromContext(ctx)
	if retrieved == nil {
		t.Fatal("GetSessionFromContext() returned nil")
		return
	}
	if retrieved.ID != "test123" {
		t.Errorf("Session ID = %s, want test123", retrieved.ID)
	}

	// Test without session in context.
	emptyCtx := context.Background()
	notFound := GetSessionFromContext(emptyCtx)
	if notFound != nil {
		t.Error("GetSessionFromContext() should return nil for empty context")
	}
}

func TestSessionManager_ClearSessionCookie(t *testing.T) {
	sm := NewSessionManager("test-secret", nil)

	w := httptest.NewRecorder()
	sm.ClearSessionCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("No cookies set")
	}

	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("Session cookie not found")
		return
	}

	if sessionCookie.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1 (expired)", sessionCookie.MaxAge)
	}
}

func TestSession_MarshalJSON(t *testing.T) {
	session := &Session{
		ID:            "test123",
		Token:         "secret-token",
		DownloadToken: "secret-download",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	}

	data, err := session.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Verify sensitive fields are not included.
	jsonStr := string(data)
	if contains(jsonStr, "secret-token") {
		t.Error("JSON should not contain Token")
	}
	if contains(jsonStr, "secret-download") {
		t.Error("JSON should not contain DownloadToken")
	}
	if !contains(jsonStr, "test123") {
		t.Error("JSON should contain session_id")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
