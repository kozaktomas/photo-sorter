package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// SubjectResponse represents a subject (person) in API responses
type SubjectResponse struct {
	UID        string `json:"uid"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Thumb      string `json:"thumb"`
	PhotoCount int    `json:"photo_count"`
	Favorite   bool   `json:"favorite"`
	About      string `json:"about,omitempty"`
	Alias      string `json:"alias,omitempty"`
	Bio        string `json:"bio,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Hidden     bool   `json:"hidden"`
	Private    bool   `json:"private"`
	Excluded   bool   `json:"excluded"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

func subjectToResponse(s photoprism.Subject) SubjectResponse {
	return SubjectResponse{
		UID:        s.UID,
		Name:       s.Name,
		Slug:       s.Slug,
		Thumb:      s.Thumb,
		PhotoCount: s.PhotoCount,
		Favorite:   s.Favorite,
		About:      s.About,
		Alias:      s.Alias,
		Bio:        s.Bio,
		Notes:      s.Notes,
		Hidden:     s.Hidden,
		Private:    s.Private,
		Excluded:   s.Excluded,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
}

// ListSubjects returns all subjects (people)
func (h *FacesHandler) ListSubjects(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Parse query parameters
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = constants.DefaultHandlerPageSize
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	subjects, err := pp.GetSubjects(count, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get subjects")
		return
	}

	response := make([]SubjectResponse, len(subjects))
	for i := range subjects {
		response[i] = subjectToResponse(subjects[i])
	}

	respondJSON(w, http.StatusOK, response)
}

// GetSubject returns a single subject by UID
func (h *FacesHandler) GetSubject(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	subject, err := pp.GetSubject(uid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get subject")
		return
	}

	respondJSON(w, http.StatusOK, subjectToResponse(*subject))
}

// SubjectUpdateRequest represents the request body for updating a subject
type SubjectUpdateRequest struct {
	Name     *string `json:"name,omitempty"`
	About    *string `json:"about,omitempty"`
	Alias    *string `json:"alias,omitempty"`
	Bio      *string `json:"bio,omitempty"`
	Notes    *string `json:"notes,omitempty"`
	Favorite *bool   `json:"favorite,omitempty"`
	Hidden   *bool   `json:"hidden,omitempty"`
	Private  *bool   `json:"private,omitempty"`
	Excluded *bool   `json:"excluded,omitempty"`
}

// UpdateSubject updates a subject
func (h *FacesHandler) UpdateSubject(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	var req SubjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	update := photoprism.SubjectUpdate{
		Name:     req.Name,
		About:    req.About,
		Alias:    req.Alias,
		Bio:      req.Bio,
		Notes:    req.Notes,
		Favorite: req.Favorite,
		Hidden:   req.Hidden,
		Private:  req.Private,
		Excluded: req.Excluded,
	}

	subject, err := pp.UpdateSubject(uid, update)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update subject")
		return
	}

	respondJSON(w, http.StatusOK, subjectToResponse(*subject))
}
