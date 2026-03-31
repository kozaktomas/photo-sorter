package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
)

// TextHandler handles AI text operations.
type TextHandler struct {
	config *config.Config
	mu     sync.RWMutex
	cache  map[string]cachedResult
}

type cachedResult struct {
	response map[string]any
}

// NewTextHandler creates a new text handler.
func NewTextHandler(cfg *config.Config) *TextHandler {
	return &TextHandler{
		config: cfg,
		cache:  make(map[string]cachedResult),
	}
}

// cacheKey computes a SHA-256 hash of the given parts joined by a null byte.
func cacheKey(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// getCache returns a cached response if it exists.
func (h *TextHandler) getCache(key string) (map[string]any, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if cached, ok := h.cache[key]; ok {
		return cached.response, true
	}
	return nil, false
}

// setCache stores a response in the cache with cost zeroed and cached flag set.
func (h *TextHandler) setCache(key string, resp map[string]any) {
	cached := make(map[string]any, len(resp))
	maps.Copy(cached, resp)
	cached["cost_czk"] = 0.0
	cached["cached"] = true
	h.mu.Lock()
	h.cache[key] = cachedResult{response: cached}
	h.mu.Unlock()
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

	key := cacheKey("check", req.Text)
	if cached, ok := h.getCache(key); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	result, err := ai.CheckText(r.Context(), h.config.OpenAI.Token, req.Text)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text check failed: "+err.Error())
		return
	}

	resp := map[string]any{
		"corrected_text":    result.CorrectedText,
		"readability_score": result.ReadabilityScore,
		"changes":           result.Changes,
		"cost_czk":          h.computeCostCZK(result.Usage),
		"cached":            false,
	}
	respondJSON(w, http.StatusOK, resp)
	h.setCache(key, resp)
}

// Consistency handles POST /api/v1/text/consistency.
func (h *TextHandler) Consistency(w http.ResponseWriter, r *http.Request) {
	if h.config.OpenAI.Token == "" {
		respondError(w, http.StatusServiceUnavailable, "OpenAI not configured")
		return
	}

	var req struct {
		Texts []ai.ConsistencyTextEntry `json:"texts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if len(req.Texts) < 2 {
		respondError(w, http.StatusBadRequest, "at least 2 texts are required")
		return
	}

	// Build cache key from all text contents
	parts := make([]string, 0, len(req.Texts)+1)
	parts = append(parts, "consistency")
	for _, t := range req.Texts {
		parts = append(parts, t.ID+":"+t.Content)
	}
	key := cacheKey(parts...)
	if cached, ok := h.getCache(key); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	result, err := ai.CheckConsistency(r.Context(), h.config.OpenAI.Token, req.Texts)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "consistency check failed: "+err.Error())
		return
	}

	resp := map[string]any{
		"consistency_score": result.ConsistencyScore,
		"tone":              result.Tone,
		"issues":            result.Issues,
		"cost_czk":          h.computeCostCZK(result.Usage),
		"cached":            false,
	}
	respondJSON(w, http.StatusOK, resp)
	h.setCache(key, resp)
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

	key := cacheKey("rewrite", req.Text, req.TargetLength)
	if cached, ok := h.getCache(key); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	result, err := ai.RewriteText(r.Context(), h.config.OpenAI.Token, req.Text, req.TargetLength)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text rewrite failed: "+err.Error())
		return
	}

	resp := map[string]any{
		"rewritten_text": result.RewrittenText,
		"cost_czk":       h.computeCostCZK(result.Usage),
		"cached":         false,
	}
	respondJSON(w, http.StatusOK, resp)
	h.setCache(key, resp)
}
