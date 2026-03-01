package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// LabelsHandler handles label-related endpoints.
type LabelsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewLabelsHandler creates a new labels handler.
func NewLabelsHandler(cfg *config.Config, sm *middleware.SessionManager) *LabelsHandler {
	return &LabelsHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// LabelResponse represents a label in API responses.
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

func labelToResponse(l photoprism.Label) LabelResponse {
	return LabelResponse{
		UID:         l.UID,
		Name:        l.Name,
		Slug:        l.Slug,
		Description: l.Description,
		Notes:       l.Notes,
		PhotoCount:  l.PhotoCount,
		Favorite:    l.Favorite,
		Priority:    l.Priority,
		CreatedAt:   l.CreatedAt,
	}
}

// List returns all labels.
func (h *LabelsHandler) List(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Parse query parameters.
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = constants.DefaultLabelCount
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	all := r.URL.Query().Get("all") == "true"

	labels, err := pp.GetLabels(count, offset, all)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get labels")
		return
	}

	response := make([]LabelResponse, len(labels))
	for i, l := range labels {
		response[i] = labelToResponse(l)
	}

	respondJSON(w, http.StatusOK, response)
}

// BatchDeleteRequest represents a batch delete request.
type BatchDeleteRequest struct {
	UIDs []string `json:"uids"`
}

// Get returns a single label by UID.
func (h *LabelsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// PhotoPrism has no single-label GET endpoint, so fetch all and find by UID.
	labels, err := pp.GetLabels(5000, 0, true)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get labels")
		return
	}

	for _, l := range labels {
		if l.UID == uid {
			respondJSON(w, http.StatusOK, labelToResponse(l))
			return
		}
	}

	respondError(w, http.StatusNotFound, "label not found")
}

// LabelUpdateRequest represents the request body for updating a label.
type LabelUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	Favorite    *bool   `json:"favorite,omitempty"`
}

// Update updates a label.
func (h *LabelsHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	var req LabelUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	update := photoprism.LabelUpdate{
		Name:        req.Name,
		Description: req.Description,
		Notes:       req.Notes,
		Priority:    req.Priority,
		Favorite:    req.Favorite,
	}

	label, err := pp.UpdateLabel(uid, update)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update label")
		return
	}

	respondJSON(w, http.StatusOK, labelToResponse(*label))
}

// BatchDelete deletes multiple labels.
func (h *LabelsHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	var req BatchDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if len(req.UIDs) == 0 {
		respondError(w, http.StatusBadRequest, "no labels specified")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	if err := pp.DeleteLabels(req.UIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete labels")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"deleted": len(req.UIDs)})
}
