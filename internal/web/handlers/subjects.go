package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// SubjectResponse represents a subject (person) in API responses. The wire
// shape mirrors the previous PhotoPrism passthrough so the frontend contract
// stays stable. Fields that the native subjects table does not yet store
// (Thumb, About, Alias, Bio, Hidden, Excluded) are kept on the wire for
// backwards compatibility and always serialise as their zero value.
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

func subjectToResponse(s database.Subject) SubjectResponse {
	return SubjectResponse{
		UID:        s.UID,
		Name:       s.Name,
		Slug:       s.Slug,
		PhotoCount: s.PhotoCount,
		Favorite:   s.Favorite,
		Notes:      s.Notes,
		Private:    s.Private,
		CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// requireSubjectRepo returns the configured SubjectWriter; on missing
// configuration it writes a 503 error response and returns nil.
func (h *FacesHandler) requireSubjectRepo(w http.ResponseWriter) database.SubjectWriter {
	if h.subjectRepo != nil {
		return h.subjectRepo
	}
	respondError(w, http.StatusServiceUnavailable, "subject storage not available")
	return nil
}

// ListSubjects returns all subjects (people). Supported query parameters:
// count (limit, defaults to constants.DefaultHandlerPageSize) and offset.
func (h *FacesHandler) ListSubjects(w http.ResponseWriter, r *http.Request) {
	repo := h.requireSubjectRepo(w)
	if repo == nil {
		return
	}

	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = constants.DefaultHandlerPageSize
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	subjects, err := repo.ListSubjects(r.Context(), database.SubjectQuery{
		Limit:  count,
		Offset: offset,
	})
	if err != nil {
		log.Printf("subjects list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get subjects")
		return
	}

	response := make([]SubjectResponse, len(subjects))
	for i := range subjects {
		response[i] = subjectToResponse(subjects[i])
	}

	respondJSON(w, http.StatusOK, response)
}

// GetSubject returns a single subject by UID.
func (h *FacesHandler) GetSubject(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	repo := h.requireSubjectRepo(w)
	if repo == nil {
		return
	}

	subject, err := repo.GetSubject(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "subject not found")
		return
	}
	if err != nil {
		log.Printf("subjects get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get subject")
		return
	}

	respondJSON(w, http.StatusOK, subjectToResponse(*subject))
}

// SubjectUpdateRequest represents the request body for updating a subject.
// Fields that the native subjects table does not yet store (About, Alias,
// Bio, Hidden, Excluded) are accepted on the wire for backwards
// compatibility but silently ignored.
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

// applySubjectUpdateFields copies the supplied request fields into the
// target subject. A non-empty name change clears the slug so the writer
// regenerates a fresh slug (with collision suffix if needed).
func applySubjectUpdateFields(s *database.Subject, req SubjectUpdateRequest) {
	if req.Name != nil && *req.Name != "" {
		s.Name = *req.Name
		s.Slug = ""
	}
	if req.Notes != nil {
		s.Notes = *req.Notes
	}
	if req.Favorite != nil {
		s.Favorite = *req.Favorite
	}
	if req.Private != nil {
		s.Private = *req.Private
	}
}

// UpdateSubject updates a subject.
func (h *FacesHandler) UpdateSubject(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "uid is required")
		return
	}

	var req SubjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	repo := h.requireSubjectRepo(w)
	if repo == nil {
		return
	}

	subject, err := repo.GetSubject(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "subject not found")
		return
	}
	if err != nil {
		log.Printf("subjects update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get subject")
		return
	}

	applySubjectUpdateFields(subject, req)

	if err := repo.UpdateSubject(r.Context(), subject); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "subject not found")
			return
		}
		log.Printf("subjects update %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to update subject")
		return
	}

	respondJSON(w, http.StatusOK, subjectToResponse(*subject))
}
