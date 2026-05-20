package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// touchLastLoginTimeout caps the fire-and-forget UPDATE that records the
// caller's last_login_at. The query is cheap and runs in its own goroutine,
// so a short context bound is enough to keep the background work from
// outliving a sluggish DB.
const touchLastLoginTimeout = 5 * time.Second

// errInvalidCredentials is the generic message returned for any login
// failure (missing user, disabled user, wrong password). Using one string
// for all three paths avoids leaking whether an account exists.
const errInvalidCredentials = "invalid credentials" //nolint:gosec // user-facing message, not a literal credential

// AuthHandler handles the native login / logout / status endpoints. It
// authenticates against the local users table and persists the resulting
// session via the shared SessionManager.
type AuthHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	userReader     database.UserReader
	userWriter     database.UserWriter
}

// NewAuthHandler constructs an AuthHandler. userReader is required for
// login and status; userWriter is required for the fire-and-forget
// last_login_at update on successful login. Either may be nil in tests
// that exercise only error paths, but a nil reader makes login always
// return 500.
func NewAuthHandler(
	cfg *config.Config,
	sm *middleware.SessionManager,
	userReader database.UserReader,
	userWriter database.UserWriter,
) *AuthHandler {
	return &AuthHandler{
		config:         cfg,
		sessionManager: sm,
		userReader:     userReader,
		userWriter:     userWriter,
	}
}

// loginRequest is the JSON payload accepted by POST /api/v1/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// userPayload is the user view returned by Login and Status.
type userPayload struct {
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// loginResponse is the JSON body returned by a successful login.
type loginResponse struct {
	User userPayload `json:"user"`
}

// statusResponse is the JSON body returned by GET /api/v1/auth/status.
type statusResponse struct {
	Authenticated bool         `json:"authenticated"`
	User          *userPayload `json:"user,omitempty"`
}

// Login authenticates a username + password pair against the native users
// table and, on success, mints a 30-day session.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if h.userReader == nil {
		respondError(w, http.StatusInternalServerError, "user store unavailable")
		return
	}

	user, ok := h.lookupAndVerify(r.Context(), req.Username, req.Password)
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": errInvalidCredentials})
		return
	}

	session, err := h.sessionManager.CreateSession("", "", user.UID, user.Role)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	h.touchLastLogin(user.UID)

	h.sessionManager.SetSessionCookie(w, r, session)
	respondJSON(w, http.StatusOK, loginResponse{User: toUserPayload(&user.User)})
}

// lookupAndVerify resolves the username to a user row and checks the
// supplied password against the stored bcrypt hash. It returns false for
// any failure (missing user, disabled, wrong password, transport error),
// so the caller can answer with one generic 401 message.
func (h *AuthHandler) lookupAndVerify(
	ctx context.Context, username, password string,
) (*database.UserWithSecret, bool) {
	user, err := h.userReader.GetUserByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Printf("auth: lookup user %q: %v", sanitizeForLog(username), err)
		}
		return nil, false
	}
	if user.Disabled {
		return nil, false
	}
	if !auth.CheckPassword(password, user.PasswordHash) {
		return nil, false
	}
	return user, true
}

// touchLastLogin updates users.last_login_at in a background goroutine.
// Errors are logged but never propagated to the user — the login itself
// has already succeeded by the time we get here.
func (h *AuthHandler) touchLastLogin(uid string) {
	if h.userWriter == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), touchLastLoginTimeout)
		defer cancel()
		if err := h.userWriter.TouchLastLogin(ctx, uid); err != nil {
			log.Printf("auth: touch last_login_at for %q: %v", sanitizeForLog(uid), err)
		}
	}()
}

// Logout hard-deletes the active session row (if any) and clears the
// session cookie. Always returns 200 so callers can issue logout
// unconditionally without first checking whether a session exists.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session := h.sessionManager.GetSessionFromRequest(r)
	if session != nil {
		h.sessionManager.DeleteSession(session.ID)
	}
	h.sessionManager.ClearSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Status reports whether the request carries a valid session cookie and,
// when it does, returns the user payload (uid / username / display name /
// role) for the client to render the header.
func (h *AuthHandler) Status(w http.ResponseWriter, r *http.Request) {
	session := h.sessionManager.GetSessionFromRequest(r)
	if session == nil {
		respondJSON(w, http.StatusOK, statusResponse{Authenticated: false})
		return
	}

	payload := userPayload{UID: session.UserUID, Role: session.Role}
	if h.userReader != nil && session.UserUID != "" {
		if user, err := h.userReader.GetUser(r.Context(), session.UserUID); err == nil {
			payload = toUserPayload(user)
		} else if !errors.Is(err, database.ErrNotFound) {
			log.Printf("auth: status lookup %q: %v", sanitizeForLog(session.UserUID), err)
		}
	}
	respondJSON(w, http.StatusOK, statusResponse{Authenticated: true, User: &payload})
}

// toUserPayload builds the JSON-facing user view from a stored user row.
func toUserPayload(u *database.User) userPayload {
	return userPayload{
		UID:         u.UID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
	}
}
