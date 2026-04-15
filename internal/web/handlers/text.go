package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
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
const textModel = ai.TextModel

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
		"suggestions":       result.Suggestions,
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

// checkResult holds the extracted result of a text check.
type checkResult struct {
	correctedText    string
	readabilityScore int
	changes          []string
	suggestions      []ai.TextSuggestion
	costCZK          float64
	cached           bool
	checkedAt        time.Time // populated only on a DB cache hit
}

// extractCachedChanges extracts the changes slice from a cached response.
func extractCachedChanges(cachedResp map[string]any) []string {
	if ch, ok := cachedResp["changes"].([]string); ok {
		return ch
	}
	chAny, ok := cachedResp["changes"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, c := range chAny {
		if s, ok := c.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// extractCachedSuggestions extracts the suggestions slice from a cached response.
func extractCachedSuggestions(cachedResp map[string]any) []ai.TextSuggestion {
	if s, ok := cachedResp["suggestions"].([]ai.TextSuggestion); ok {
		return s
	}
	sAny, ok := cachedResp["suggestions"].([]any)
	if !ok {
		return nil
	}
	out := make([]ai.TextSuggestion, 0, len(sAny))
	for _, item := range sAny {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sev, _ := m["severity"].(string)
		msg, _ := m["message"].(string)
		out = append(out, ai.TextSuggestion{Severity: sev, Message: msg})
	}
	return out
}

// extractCachedScore extracts readability_score from a cached response.
func extractCachedScore(cachedResp map[string]any) int {
	if rs, ok := cachedResp["readability_score"].(int); ok {
		return rs
	}
	if rsFl, ok := cachedResp["readability_score"].(float64); ok {
		return int(rsFl)
	}
	return 0
}

// checkAndSaveRequest is the parsed request for CheckAndSave.
type checkAndSaveRequest struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	Field      string `json:"field"`
	Text       string `json:"text"`
}

// valid returns true if all required fields are present.
func (r checkAndSaveRequest) valid() bool {
	return strings.TrimSpace(r.Text) != "" &&
		r.SourceType != "" && r.SourceID != "" && r.Field != ""
}

// CheckAndSave handles POST /api/v1/text/check-and-save.
// Runs the AI text check and persists the result to the database.
func (h *TextHandler) CheckAndSave(w http.ResponseWriter, r *http.Request) {
	if h.config.OpenAI.Token == "" {
		respondError(w, http.StatusServiceUnavailable, "OpenAI not configured")
		return
	}

	var req checkAndSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if !req.valid() {
		respondError(w, http.StatusBadRequest,
			"text, source_type, source_id, and field are required")
		return
	}

	contentHash := sha256Hex(req.Text)
	dbKey := database.TextCheckKey{
		SourceType: req.SourceType,
		SourceID:   req.SourceID,
		Field:      req.Field,
	}

	cr, fromDB, err := h.runCheckWithCache(r, req.Text, dbKey, contentHash)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "text check failed: "+err.Error())
		return
	}

	status := "clean"
	if len(cr.changes) > 0 {
		status = "has_errors"
	}

	dbResult, err := h.persistCheckResult(r.Context(), req, contentHash, status, cr, fromDB)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	checkedAt := dbResult.CheckedAt
	if fromDB {
		checkedAt = cr.checkedAt
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"corrected_text":    cr.correctedText,
		"readability_score": cr.readabilityScore,
		"changes":           cr.changes,
		"suggestions":       cr.suggestions,
		"cost_czk":          cr.costCZK,
		"cached":            cr.cached,
		"status":            status,
		"content_hash":      contentHash,
		"checked_at":        checkedAt,
	})
}

// runCheckWithCache runs a text check using a three-tier lookup:
// in-memory cache → persistent DB cache (by source+content hash) → OpenAI.
// dbKey is optional; pass an empty TextCheckKey to skip the DB tier.
// The returned fromDB flag lets callers skip an idempotent DB upsert when
// the result was just read from the database.
func (h *TextHandler) runCheckWithCache(
	r *http.Request, text string, dbKey database.TextCheckKey, contentHash string,
) (*checkResult, bool, error) {
	key := cacheKey("check", text)

	// Tier 1: in-memory cache
	if cachedResp, ok := h.getCache(key); ok {
		correctedText, _ := cachedResp["corrected_text"].(string)
		return &checkResult{
			correctedText:    correctedText,
			readabilityScore: extractCachedScore(cachedResp),
			changes:          extractCachedChanges(cachedResp),
			suggestions:      extractCachedSuggestions(cachedResp),
			cached:           true,
		}, false, nil
	}

	// Tier 2: persistent DB cache — keyed by (source, id, field) + content hash.
	// Survives server restart, so after a reboot the next click on Check
	// for an unchanged text does not burn an OpenAI call.
	if dbKey.SourceType != "" && contentHash != "" {
		if cr, ok := h.lookupDBCache(r.Context(), dbKey, contentHash); ok {
			// Hydrate the in-memory tier so subsequent hits in this session
			// skip even the DB round trip.
			h.setCache(key, map[string]any{
				"corrected_text":    cr.correctedText,
				"readability_score": cr.readabilityScore,
				"changes":           cr.changes,
				"suggestions":       cr.suggestions,
				"cost_czk":          0.0,
				"cached":            false,
			})
			return cr, true, nil
		}
	}

	// Tier 3: call OpenAI
	result, err := ai.CheckText(r.Context(), h.config.OpenAI.Token, text)
	if err != nil {
		return nil, false, fmt.Errorf("check text: %w", err)
	}
	costCZK := h.computeCostCZK(result.Usage)
	h.setCache(key, map[string]any{
		"corrected_text":    result.CorrectedText,
		"readability_score": result.ReadabilityScore,
		"changes":           result.Changes,
		"suggestions":       result.Suggestions,
		"cost_czk":          costCZK,
		"cached":            false,
	})
	return &checkResult{
		correctedText:    result.CorrectedText,
		readabilityScore: result.ReadabilityScore,
		changes:          result.Changes,
		suggestions:      result.Suggestions,
		costCZK:          costCZK,
	}, false, nil
}

// persistCheckResult builds a database.TextCheckResult from a checkResult
// and upserts it. When fromDB is true the upsert is skipped — the row is
// already present with the same content hash, so rewriting it would only
// bump checked_at for no gain. Returns the (populated) dbResult so callers
// can forward CheckedAt to the response.
func (h *TextHandler) persistCheckResult(
	ctx context.Context, req checkAndSaveRequest, contentHash, status string,
	cr *checkResult, fromDB bool,
) (*database.TextCheckResult, error) {
	dbResult := &database.TextCheckResult{
		SourceType:       req.SourceType,
		SourceID:         req.SourceID,
		Field:            req.Field,
		ContentHash:      contentHash,
		Status:           status,
		ReadabilityScore: &cr.readabilityScore,
		CorrectedText:    cr.correctedText,
		Changes:          cr.changes,
		CostCZK:          cr.costCZK,
	}
	if fromDB {
		return dbResult, nil
	}

	store, err := database.GetTextCheckStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("database not available: %w", err)
	}
	dbSuggestions := make([]database.TextSuggestion, len(cr.suggestions))
	for i, s := range cr.suggestions {
		dbSuggestions[i] = database.TextSuggestion{Severity: s.Severity, Message: s.Message}
	}
	dbResult.Suggestions = dbSuggestions
	if err := store.SaveTextCheckResult(ctx, dbResult); err != nil {
		return nil, fmt.Errorf("failed to save check result: %w", err)
	}
	return dbResult, nil
}

// lookupDBCache looks up a persisted text check result in the database
// and returns it as a checkResult if the content hash matches the current
// text. Returns (nil, false) when the DB has no matching entry.
func (h *TextHandler) lookupDBCache(
	ctx context.Context, key database.TextCheckKey, contentHash string,
) (*checkResult, bool) {
	store, err := database.GetTextCheckStore(ctx)
	if err != nil {
		return nil, false
	}
	results, err := store.GetTextCheckResults(ctx, []database.TextCheckKey{key})
	if err != nil {
		return nil, false
	}
	mapKey := key.SourceType + ":" + key.SourceID + ":" + key.Field
	existing, ok := results[mapKey]
	if !ok || existing.ContentHash != contentHash {
		return nil, false
	}
	score := 0
	if existing.ReadabilityScore != nil {
		score = *existing.ReadabilityScore
	}
	suggestions := make([]ai.TextSuggestion, len(existing.Suggestions))
	for i, s := range existing.Suggestions {
		suggestions[i] = ai.TextSuggestion{Severity: s.Severity, Message: s.Message}
	}
	return &checkResult{
		correctedText:    existing.CorrectedText,
		readabilityScore: score,
		changes:          existing.Changes,
		suggestions:      suggestions,
		cached:           true,
		checkedAt:        existing.CheckedAt,
	}, true
}

// textCheckStatusEntry is the JSON response shape for a single check status entry.
type textCheckStatusEntry struct {
	Status           string                    `json:"status"`
	ReadabilityScore *int                      `json:"readability_score,omitempty"`
	CheckedAt        string                    `json:"checked_at"`
	IsStale          bool                      `json:"is_stale"`
	CorrectedText    string                    `json:"corrected_text,omitempty"`
	Changes          []string                  `json:"changes,omitempty"`
	Suggestions      []database.TextSuggestion `json:"suggestions,omitempty"`
}

// TextCheckStatus handles GET /api/v1/books/{id}/text-check-status.
func (h *TextHandler) TextCheckStatus(w http.ResponseWriter, r *http.Request) {
	bookID := chi.URLParam(r, "id")
	if bookID == "" {
		respondError(w, http.StatusBadRequest, "book id is required")
		return
	}

	keys, contentHashes, err := h.collectBookTextKeys(r, bookID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(keys) == 0 {
		respondJSON(w, http.StatusOK, map[string]any{})
		return
	}

	store, err := database.GetTextCheckStore(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database not available: "+err.Error())
		return
	}
	results, err := store.GetTextCheckResults(r.Context(), keys)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get check results: "+err.Error())
		return
	}

	response := make(map[string]textCheckStatusEntry, len(results))
	for mapKey, result := range results {
		currentHash, exists := contentHashes[mapKey]
		isStale := !exists || currentHash != result.ContentHash
		entry := textCheckStatusEntry{
			Status:           result.Status,
			ReadabilityScore: result.ReadabilityScore,
			CheckedAt:        result.CheckedAt.Format("2006-01-02T15:04:05Z07:00"),
			IsStale:          isStale,
			Suggestions:      result.Suggestions,
		}
		if result.Status == "has_errors" {
			entry.CorrectedText = result.CorrectedText
			entry.Changes = result.Changes
		}
		response[mapKey] = entry
	}
	respondJSON(w, http.StatusOK, response)
}

// collectBookTextKeys gathers all text check keys and content hashes for a book.
func (h *TextHandler) collectBookTextKeys(
	r *http.Request, bookID string,
) ([]database.TextCheckKey, map[string]string, error) {
	bookReader, err := database.GetBookReader(r.Context())
	if err != nil {
		return nil, nil, fmt.Errorf("database not available: %w", err)
	}

	var keys []database.TextCheckKey
	contentHashes := make(map[string]string)

	if err := collectSectionPhotoKeys(r, bookReader, bookID, &keys, contentHashes); err != nil {
		return nil, nil, err
	}
	if err := collectPageSlotKeys(r, bookReader, bookID, &keys, contentHashes); err != nil {
		return nil, nil, err
	}
	return keys, contentHashes, nil
}

// collectSectionPhotoKeys adds text check keys for section photo descriptions.
func collectSectionPhotoKeys(
	r *http.Request, bookReader database.BookReader, bookID string,
	keys *[]database.TextCheckKey, hashes map[string]string,
) error {
	sections, err := bookReader.GetSections(r.Context(), bookID)
	if err != nil {
		return fmt.Errorf("failed to get sections: %w", err)
	}
	for _, section := range sections {
		photos, photosErr := bookReader.GetSectionPhotos(r.Context(), section.ID)
		if photosErr != nil {
			return fmt.Errorf("failed to get section photos: %w", photosErr)
		}
		for _, photo := range photos {
			if strings.TrimSpace(photo.Description) == "" {
				continue
			}
			sourceID := section.ID + ":" + photo.PhotoUID
			*keys = append(*keys, database.TextCheckKey{
				SourceType: "section_photo",
				SourceID:   sourceID,
				Field:      "description",
			})
			hashes["section_photo:"+sourceID+":description"] = sha256Hex(photo.Description)
		}
	}
	return nil
}

// collectPageSlotKeys adds text check keys for page text slots.
func collectPageSlotKeys(
	r *http.Request, bookReader database.BookReader, bookID string,
	keys *[]database.TextCheckKey, hashes map[string]string,
) error {
	pages, err := bookReader.GetPages(r.Context(), bookID)
	if err != nil {
		return fmt.Errorf("failed to get pages: %w", err)
	}
	for _, page := range pages {
		for _, slot := range page.Slots {
			if !slot.IsTextSlot() {
				continue
			}
			sourceID := fmt.Sprintf("%s:%d", page.ID, slot.SlotIndex)
			*keys = append(*keys, database.TextCheckKey{
				SourceType: "page_slot",
				SourceID:   sourceID,
				Field:      "text_content",
			})
			hashes["page_slot:"+sourceID+":text_content"] = sha256Hex(slot.TextContent)
		}
	}
	return nil
}

// sha256Hex returns the hex-encoded SHA-256 hash of the given string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
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
