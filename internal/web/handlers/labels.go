package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// LabelsHandler handles label-related endpoints. It reads and writes the
// native labels / photo_labels tables via LabelWriter.
type LabelsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager

	// repo backs every native label endpoint. May be nil in tests that
	// exercise paths which do not touch labels.
	repo database.LabelWriter
}

// NewLabelsHandler creates a new labels handler. repo backs the native
// endpoints; it may be nil in environments where those paths are unused
// (handlers will then surface a 503).
func NewLabelsHandler(
	cfg *config.Config, sm *middleware.SessionManager, repo database.LabelWriter,
) *LabelsHandler {
	return &LabelsHandler{
		config:         cfg,
		sessionManager: sm,
		repo:           repo,
	}
}

// LabelResponse represents a label in API responses. The shape mirrors the
// previous PhotoPrism passthrough (snake_case keys, photo_count populated)
// so the frontend contract stays stable. Description / Notes are kept on
// the wire so existing UI code that reads them does not break; the native
// labels table does not yet store them, so they always serialise as the
// empty string.
type LabelResponse struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Notes       string `json:"notes"`
	PhotoCount  int    `json:"photo_count"`
	Favorite    bool   `json:"favorite"`
	Priority    int    `json:"priority"`
	CreatedAt   string `json:"created_at"`
}

// labelToResponse maps a native database.Label to the wire shape.
func labelToResponse(l database.Label) LabelResponse {
	return LabelResponse{
		UID:        l.UID,
		Name:       l.Name,
		Slug:       l.Slug,
		PhotoCount: l.PhotoCount,
		Favorite:   l.Favorite,
		Priority:   l.Priority,
		CreatedAt:  l.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// requireLabelWriter returns the configured LabelWriter; on missing
// configuration it writes a 503 error response and returns nil.
func (h *LabelsHandler) requireLabelWriter(w http.ResponseWriter) database.LabelWriter {
	if h.repo != nil {
		return h.repo
	}
	respondError(w, http.StatusServiceUnavailable, "label storage not available")
	return nil
}

// parseLabelListQuery extracts the supported filter and pagination params
// from the request URL. On invalid input it writes a 400 and returns
// ok=false.
func parseLabelListQuery(w http.ResponseWriter, r *http.Request) (database.LabelQuery, bool) {
	q := r.URL.Query()
	out := database.LabelQuery{
		Search: q.Get("q"),
		SortBy: q.Get("sort"),
	}
	if v := q.Get("min_photos"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			respondError(w, http.StatusBadRequest, "invalid min_photos")
			return out, false
		}
		out.MinPhotos = n
	}
	limit, ok := parseNonNegativeQueryInt(w, q, "limit", 0)
	if !ok {
		return out, false
	}
	// Accept the legacy "count" alias from the previous PhotoPrism passthrough
	// so existing frontend callers keep working without changes.
	if limit == 0 {
		if v, ok := parseNonNegativeQueryInt(w, q, "count", 0); ok {
			limit = v
		} else {
			return out, false
		}
	}
	out.Limit = limit
	offset, ok := parseNonNegativeQueryInt(w, q, "offset", 0)
	if !ok {
		return out, false
	}
	out.Offset = offset
	return out, true
}

// List returns labels matching the supplied filter + pagination params.
// Supported query params: q (search), min_photos, sort, limit/count, offset.
func (h *LabelsHandler) List(w http.ResponseWriter, r *http.Request) {
	repo := h.requireLabelWriter(w)
	if repo == nil {
		return
	}
	query, ok := parseLabelListQuery(w, r)
	if !ok {
		return
	}
	labels, err := repo.ListLabels(r.Context(), query)
	if err != nil {
		log.Printf("labels list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get labels")
		return
	}
	response := make([]LabelResponse, 0, len(labels))
	for i := range labels {
		response = append(response, labelToResponse(labels[i]))
	}
	respondJSON(w, http.StatusOK, response)
}

// Get returns a single label by UID.
func (h *LabelsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	repo := h.requireLabelWriter(w)
	if repo == nil {
		return
	}
	label, err := repo.GetLabel(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "label not found")
		return
	}
	if err != nil {
		log.Printf("labels get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get label")
		return
	}
	respondJSON(w, http.StatusOK, labelToResponse(*label))
}

// LabelUpdateRequest represents the request body for updating a label.
// Pointers preserve the distinction between "key omitted" and "key set to
// an explicit zero value" so a caller can clear the favorite flag without
// also overwriting priority.
type LabelUpdateRequest struct {
	Name     *string `json:"name,omitempty"`
	Priority *int    `json:"priority,omitempty"`
	Favorite *bool   `json:"favorite,omitempty"`
}

// applyLabelUpdateFields copies the supplied request fields into the
// target label. A non-empty name change clears the slug so the writer
// regenerates a fresh slug (with collision suffix if needed).
func applyLabelUpdateFields(label *database.Label, req LabelUpdateRequest) {
	if req.Name != nil && *req.Name != "" {
		label.Name = *req.Name
		label.Slug = ""
	}
	if req.Priority != nil {
		label.Priority = *req.Priority
	}
	if req.Favorite != nil {
		label.Favorite = *req.Favorite
	}
}

// Update applies a partial update to a label. A name change re-slugs the
// row via the writer's slug-collision resolver.
func (h *LabelsHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req LabelUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	repo := h.requireLabelWriter(w)
	if repo == nil {
		return
	}
	label, err := repo.GetLabel(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "label not found")
		return
	}
	if err != nil {
		log.Printf("labels update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get label")
		return
	}
	applyLabelUpdateFields(label, req)
	if err := repo.UpdateLabel(r.Context(), label); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "label not found")
			return
		}
		log.Printf("labels update %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to update label")
		return
	}
	respondJSON(w, http.StatusOK, labelToResponse(*label))
}

// BatchDeleteRequest represents a batch delete request.
type BatchDeleteRequest struct {
	UIDs []string `json:"uids"`
}

// BatchDelete deletes the labels identified by the supplied UIDs and
// reports the number of rows actually removed. Unknown UIDs are silently
// skipped so a partially-stale client request still makes progress.
func (h *LabelsHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req BatchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.UIDs) == 0 {
		respondError(w, http.StatusBadRequest, "no labels specified")
		return
	}
	repo := h.requireLabelWriter(w)
	if repo == nil {
		return
	}
	deleted, err := repo.DeleteLabels(r.Context(), req.UIDs)
	if err != nil {
		log.Printf("labels batch delete: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to delete labels")
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}
