package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "photo_sorter_session"
	sessionDuration   = 24 * time.Hour
	cleanupInterval   = 10 * time.Minute
)

// Session represents a user session
type Session struct {
	ID            string    `json:"id"`
	Token         string    `json:"token"`          // PhotoPrism access token
	DownloadToken string    `json:"download_token"` // PhotoPrism download token
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// SessionRepository defines the interface for persistent session storage
type SessionRepository interface {
	Save(ctx context.Context, id, token, downloadToken string, createdAt, expiresAt time.Time) error
	Get(ctx context.Context, sessionID string) (*StoredSession, error)
	Delete(ctx context.Context, sessionID string) error
	DeleteExpired(ctx context.Context) (int64, error)
}

// StoredSession represents session data from the repository
type StoredSession struct {
	ID            string
	Token         string
	DownloadToken string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// SessionManager handles session creation and validation
type SessionManager struct {
	secret   []byte
	sessions map[string]*Session
	mu       sync.RWMutex
	repo     SessionRepository // optional persistent storage
	stopCh   chan struct{}     // channel to stop cleanup goroutine
}

// NewSessionManager creates a new session manager
func NewSessionManager(secret string, repo SessionRepository) *SessionManager {
	// Use a default secret if none provided (for development)
	if secret == "" {
		log.Println("WARNING: WEB_SESSION_SECRET is not set — using insecure default. Set WEB_SESSION_SECRET for production use.")
		secret = "photo-sorter-dev-secret-change-in-production" //nolint:gosec // dev fallback, not a real credential
	}
	sm := &SessionManager{
		secret:   []byte(secret),
		sessions: make(map[string]*Session),
		repo:     repo,
		stopCh:   make(chan struct{}),
	}

	// Start background cleanup goroutine if we have a repository
	if repo != nil {
		go sm.cleanupLoop()
	}

	return sm
}

// cleanupLoop periodically removes expired sessions from the database
func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			deleted, err := sm.repo.DeleteExpired(ctx)
			cancel()
			if err != nil {
				log.Printf("Failed to cleanup expired sessions: %v", err)
			} else if deleted > 0 {
				log.Printf("Cleaned up %d expired sessions", deleted)
			}
		case <-sm.stopCh:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (sm *SessionManager) Stop() {
	if sm.stopCh != nil {
		close(sm.stopCh)
	}
}

// CreateSession creates a new session for a user
func (sm *SessionManager) CreateSession(token, downloadToken string) (*Session, error) {
	// Generate session ID
	idBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generating random bytes: %w", err)
	}
	sessionID := base64.URLEncoding.EncodeToString(idBytes)

	now := time.Now()
	session := &Session{
		ID:            sessionID,
		Token:         token,
		DownloadToken: downloadToken,
		CreatedAt:     now,
		ExpiresAt:     now.Add(sessionDuration),
	}

	// Store in memory
	sm.mu.Lock()
	sm.sessions[sessionID] = session
	sm.mu.Unlock()

	// Persist to database if available
	if sm.repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sm.repo.Save(ctx, session.ID, session.Token, session.DownloadToken, session.CreatedAt, session.ExpiresAt); err != nil {
			log.Printf("Warning: failed to persist session to database: %v", err)
			// Continue anyway - session is still in memory
		}
	}

	return session, nil
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(sessionID string) *Session {
	// Check memory first
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if ok {
		// Check if session has expired
		if time.Now().After(session.ExpiresAt) {
			go sm.DeleteSession(sessionID)
			return nil
		}
		return session
	}

	// Not in memory - try database if available
	if sm.repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		stored, err := sm.repo.Get(ctx, sessionID)
		if err != nil {
			log.Printf("Warning: failed to get session from database: %v", err)
			return nil
		}
		if stored == nil {
			return nil
		}

		// Restore to memory cache
		session = &Session{
			ID:            stored.ID,
			Token:         stored.Token,
			DownloadToken: stored.DownloadToken,
			CreatedAt:     stored.CreatedAt,
			ExpiresAt:     stored.ExpiresAt,
		}

		sm.mu.Lock()
		sm.sessions[sessionID] = session
		sm.mu.Unlock()

		return session
	}

	return nil
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) {
	// Remove from memory
	sm.mu.Lock()
	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	// Remove from database if available
	if sm.repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sm.repo.Delete(ctx, sessionID); err != nil {
			log.Printf("Warning: failed to delete session from database: %v", err)
		}
	}
}

// SetSessionCookie sets the session cookie on the response.
// Secure flag is auto-detected from X-Forwarded-Proto or TLS state.
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, r *http.Request, session *Session) {
	// Sign the session ID
	signature := sm.signData(session.ID)
	cookieValue := session.ID + "." + signature

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
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
	if session := sm.getSessionFromCookie(r); session != nil {
		return session
	}

	// Try Authorization header
	authHeader := r.Header.Get("Authorization")
	if sessionID, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		if session := sm.GetSession(sessionID); session != nil {
			return session
		}
	}

	return nil
}

// getSessionFromCookie extracts and validates a session from the request cookie.
func (sm *SessionManager) getSessionFromCookie(r *http.Request) *Session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(cookie.Value, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	sessionID := parts[0]
	if !sm.verifySignature(sessionID, parts[1]) {
		return nil
	}
	return sm.GetSession(sessionID)
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

// SessionJSONData is a helper struct for JSON responses
type SessionJSONData struct {
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
}

// ToJSON returns the session data for JSON response
func (s *Session) ToJSON() SessionJSONData {
	return SessionJSONData{
		SessionID: s.ID,
		ExpiresAt: s.ExpiresAt.Format(time.RFC3339),
	}
}

// MarshalJSON implements json.Marshaler (excludes sensitive fields)
func (s *Session) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(s.ToJSON())
	if err != nil {
		return nil, fmt.Errorf("marshaling session: %w", err)
	}
	return data, nil
}
