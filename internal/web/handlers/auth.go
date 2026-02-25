package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(cfg *config.Config, sm *middleware.SessionManager) *AuthHandler {
	return &AuthHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// loginRequest represents a login request
type loginRequest struct {
	username string
	password string
}

func (l *loginRequest) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal login request: %w", err)
	}
	l.username = raw["username"]
	l.password = raw["password"]
	return nil
}

// LoginResponse represents a login response
type LoginResponse struct {
	Success   bool   `json:"success"`
	SessionID string `json:"session_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	// Require both username and password
	if req.username == "" || req.password == "" {
		respondError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	// Authenticate with PhotoPrism and capture tokens
	client := &authClient{}
	if err := client.auth(h.config.PhotoPrism.URL, req.username, req.password); err != nil {
		respondJSON(w, http.StatusUnauthorized, LoginResponse{
			Success: false,
			Error:   "invalid credentials",
		})
		return
	}

	// Create session
	session, err := h.sessionManager.CreateSession(client.token, client.downloadToken)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Set session cookie
	h.sessionManager.SetSessionCookie(w, r, session)

	respondJSON(w, http.StatusOK, LoginResponse{
		Success:   true,
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt.Format("2006-01-02T15:04:05Z"),
	})
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session := h.sessionManager.GetSessionFromRequest(r)
	if session != nil {
		// Logout from PhotoPrism
		pp, err := getPhotoPrismClient(h.config, session)
		if err == nil {
			pp.Logout()
		}
		// Delete session
		h.sessionManager.DeleteSession(session.ID)
	}

	h.sessionManager.ClearSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// StatusResponse represents the auth status response
type StatusResponse struct {
	Authenticated bool   `json:"authenticated"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

// Status checks if the user is authenticated by validating the session.
func (h *AuthHandler) Status(w http.ResponseWriter, r *http.Request) {
	session := h.sessionManager.GetSessionFromRequest(r)
	if session == nil {
		respondJSON(w, http.StatusOK, StatusResponse{Authenticated: false})
		return
	}
	respondJSON(w, http.StatusOK, StatusResponse{
		Authenticated: true,
		ExpiresAt:     session.ExpiresAt.Format("2006-01-02T15:04:05Z"),
	})
}

// authClient is a minimal client just for authentication
type authClient struct {
	token         string
	downloadToken string
}

func (c *authClient) auth(url, username, password string) error {
	inputBody, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return fmt.Errorf("could not marshal input: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url+"/api/v1/sessions", bytes.NewReader(inputBody))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL from trusted server config
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed with status %d", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("could not unmarshal response: %w", err)
	}

	var token string
	if err := json.Unmarshal(raw["access_token"], &token); err != nil {
		return fmt.Errorf("could not read access token: %w", err)
	}

	var cfg struct {
		DownloadToken string `json:"downloadToken"`
	}
	if err := json.Unmarshal(raw["config"], &cfg); err != nil {
		return fmt.Errorf("could not read config: %w", err)
	}

	c.token = token
	c.downloadToken = cfg.DownloadToken

	return nil
}
