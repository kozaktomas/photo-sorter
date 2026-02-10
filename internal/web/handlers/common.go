package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// errInvalidRequestBody is a shared error message for invalid JSON request bodies
const errInvalidRequestBody = "invalid request body"

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// respondError sends an error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// getPhotoPrismClient creates a PhotoPrism client.
// If a session is provided, uses its tokens. Otherwise, authenticates with config credentials.
// This allows the API to work both with and without user sessions.
func getPhotoPrismClient(cfg *config.Config, session *middleware.Session) (*photoprism.PhotoPrism, error) {
	if session != nil && session.Token != "" {
		return photoprism.NewPhotoPrismFromToken(cfg.PhotoPrism.URL, session.Token, session.DownloadToken)
	}
	// No session - authenticate directly with config credentials
	return photoprism.NewPhotoPrism(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password)
}

// HealthCheck handles the health check endpoint
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
