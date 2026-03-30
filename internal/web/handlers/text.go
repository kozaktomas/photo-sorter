package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
)

// TextHandler handles AI text operations.
type TextHandler struct {
	config *config.Config
}

// NewTextHandler creates a new text handler.
func NewTextHandler(cfg *config.Config) *TextHandler {
	return &TextHandler{config: cfg}
}

// usdToCZK is the approximate USD to CZK conversion rate.
const usdToCZK = 23.5

// textModel is the model used for text operations.
const textModel = "gpt-4.1-mini"

// computeCostCZK calculates cost in CZK from token usage and model pricing.
func (h *TextHandler) computeCostCZK(usage ai.TokenUsage) float64 {
	pricing := h.config.GetModelPricing(textModel)
	inputCostUSD := float64(usage.PromptTokens) / 1_000_000 * pricing.Standard.Input
	outputCostUSD := float64(usage.CompletionTokens) / 1_000_000 * pricing.Standard.Output
	return (inputCostUSD + outputCostUSD) * usdToCZK
}

// Check handles POST /api/v1/text/check.
func (h *TextHandler) Check(w http.ResponseWriter, r *http.Request) {
	if h.config.OpenAI.Token == "" {
		respondError(w, http.StatusServiceUnavailable, "OpenAI not configured")
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		respondError(w, http.StatusBadRequest, "text is required")
		return
	}

	result, err := ai.CheckText(r.Context(), h.config.OpenAI.Token, req.Text)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text check failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"corrected_text":    result.CorrectedText,
		"readability_score": result.ReadabilityScore,
		"changes":           result.Changes,
		"cost_czk":          h.computeCostCZK(result.Usage),
	})
}

// Rewrite handles POST /api/v1/text/rewrite.
func (h *TextHandler) Rewrite(w http.ResponseWriter, r *http.Request) {
	if h.config.OpenAI.Token == "" {
		respondError(w, http.StatusServiceUnavailable, "OpenAI not configured")
		return
	}

	var req struct {
		Text         string `json:"text"`
		TargetLength string `json:"target_length"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		respondError(w, http.StatusBadRequest, "text is required")
		return
	}

	validLengths := map[string]bool{
		"much_shorter": true,
		"shorter":      true,
		"longer":       true,
		"much_longer":  true,
	}
	if !validLengths[req.TargetLength] {
		respondError(w, http.StatusBadRequest, "target_length must be one of: much_shorter, shorter, longer, much_longer")
		return
	}

	result, err := ai.RewriteText(r.Context(), h.config.OpenAI.Token, req.Text, req.TargetLength)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text rewrite failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"rewritten_text": result.RewrittenText,
		"cost_czk":       h.computeCostCZK(result.Usage),
	})
}
