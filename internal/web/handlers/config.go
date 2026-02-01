package handlers

import (
	"net/http"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ConfigHandler handles configuration endpoints
type ConfigHandler struct {
	config *config.Config
}

// NewConfigHandler creates a new config handler
func NewConfigHandler(cfg *config.Config) *ConfigHandler {
	return &ConfigHandler{
		config: cfg,
	}
}

// ConfigResponse represents the configuration response
type ConfigResponse struct {
	Providers          []ProviderInfo `json:"providers"`
	PhotoPrismDomain   string         `json:"photoprism_domain,omitempty"`
	EmbeddingsWritable bool           `json:"embeddings_writable"`
}

// ProviderInfo represents information about an AI provider
type ProviderInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// Get returns the available configuration
func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	providers := []ProviderInfo{
		{
			Name:      "openai",
			Available: h.config.OpenAI.Token != "",
		},
		{
			Name:      "gemini",
			Available: h.config.Gemini.APIKey != "",
		},
		{
			Name:      "ollama",
			Available: true, // Always available (local)
		},
		{
			Name:      "llamacpp",
			Available: true, // Always available (local)
		},
	}

	// Check if PostgreSQL database is configured (always writable)
	embeddingsWritable := database.IsInitialized()

	response := ConfigResponse{
		Providers:          providers,
		PhotoPrismDomain:   h.config.PhotoPrism.Domain,
		EmbeddingsWritable: embeddingsWritable,
	}

	respondJSON(w, http.StatusOK, response)
}
