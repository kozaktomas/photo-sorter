package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "photo_sorter_session"
	sessionDuration   = 24 * time.Hour
)

// Session represents a user session
type Session struct {
	ID            string    `json:"id"`
	Token         string    `json:"token"`          // PhotoPrism access token
	DownloadToken string    `json:"download_token"` // PhotoPrism download token
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// SessionManager handles session creation and validation
type SessionManager struct {
	secret   []byte
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager
func NewSessionManager(secret string) *SessionManager {
	// Use a default secret if none provided (for development)
	if secret == "" {
		secret = "photo-sorter-dev-secret-change-in-production"
	}
	return &SessionManager{
		secret:   []byte(secret),
		sessions: make(map[string]*Session),
	}
}

// CreateSession creates a new session for a user
func (sm *SessionManager) CreateSession(token, downloadToken string) (*Session, error) {
	// Generate session ID
	idBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	sessionID := base64.URLEncoding.EncodeToString(idBytes)

	session := &Session{
		ID:            sessionID,
		Token:         token,
		DownloadToken: downloadToken,
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(sessionDuration),
	}

	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	return session, nil
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[sessionID]
	if !ok {
		return nil
	}

	// Check if session has expired
	if time.Now().After(session.ExpiresAt) {
		go sm.DeleteSession(sessionID)
		return nil
	}

	return session
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()
}

// SetSessionCookie sets the session cookie on the response
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, session *Session) {
	// Sign the session ID
	signature := sm.signData(session.ID)
	cookieValue := session.ID + "." + signature

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// ClearSessionCookie removes the session cookie
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// GetSessionFromRequest extracts the session from a request
func (sm *SessionManager) GetSessionFromRequest(r *http.Request) *Session {
	// Try cookie first
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		parts := strings.SplitN(cookie.Value, ".", 2)
		if len(parts) == 2 {
			sessionID := parts[0]
			signature := parts[1]
			if sm.verifySignature(sessionID, signature) {
				if session := sm.GetSession(sessionID); session != nil {
					return session
				}
			}
		}
	}

	// Try Authorization header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		sessionID := strings.TrimPrefix(authHeader, "Bearer ")
		if session := sm.GetSession(sessionID); session != nil {
			return session
		}
	}

	return nil
}

// signData creates an HMAC signature for data
func (sm *SessionManager) signData(data string) string {
	h := hmac.New(sha256.New, sm.secret)
	h.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// verifySignature verifies an HMAC signature
func (sm *SessionManager) verifySignature(data, signature string) bool {
	expected := sm.signData(data)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// SessionData is a helper struct for JSON responses
type SessionData struct {
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
}

// ToJSON returns the session data for JSON response
func (s *Session) ToJSON() SessionData {
	return SessionData{
		SessionID: s.ID,
		ExpiresAt: s.ExpiresAt.Format(time.RFC3339),
	}
}

// MarshalJSON implements json.Marshaler (excludes sensitive fields)
func (s *Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToJSON())
}
