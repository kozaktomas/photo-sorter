package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// albumTypeFilterDefault is the default value of the `type` query
// parameter for List. The native albums table also accepts folder, moment,
// state, and month rows; we hide those from the default listing because
// the UI lists those separately.
const albumTypeFilterDefault = "album"

// AlbumsHandler handles album-related endpoints. It reads and writes the
// native albums / album_photos tables via AlbumWriter and delegates
// album-filtered photo listings to PhotoReader so we do not reimplement
// the existing filtering and pagination logic.
type AlbumsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager

	// albumRepo backs every native album endpoint. May be nil in tests
	// that exercise paths which do not touch albums.
	albumRepo database.AlbumWriter
	// photoRepo serves the album/{uid}/photos listing endpoint. May be nil
	// in tests that do not exercise that path.
	photoRepo database.PhotoReader
}

// NewAlbumsHandler creates a new albums handler. albumRepo and photoRepo
// back the native endpoints; either may be nil in environments where those
// paths are unused.
func NewAlbumsHandler(
	cfg *config.Config, sm *middleware.SessionManager,
	albumRepo database.AlbumWriter, photoRepo database.PhotoReader,
) *AlbumsHandler {
	return &AlbumsHandler{
		config:         cfg,
		sessionManager: sm,
		albumRepo:      albumRepo,
		photoRepo:      photoRepo,
	}
}

// AlbumResponse represents an album in API responses. The shape mirrors the
// previous PhotoPrism passthrough so the frontend keeps working.
// Location / Category / Notes / Filter / Order are populated from the
// native columns added by migration 037 (task 332a727c). Filter is the
// raw smart-album DSL string from PhotoPrism's album_filter — it is
// informational-only today, but exposed on the API so a future smart-
// album feature can consume it and so the operator can audit which
// migrated albums were originally smart-filtered.
type AlbumResponse struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PhotoCount  int    `json:"photo_count"`
	Thumb       string `json:"thumb"`
	Type        string `json:"type"`
	Favorite    bool   `json:"favorite"`
	Location    string `json:"location"`
	Category    string `json:"category"`
	Notes       string `json:"notes"`
	Filter      string `json:"filter"`
	Order       string `json:"order"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// albumToResponse maps a native database.Album to the wire shape. The
// Thumb field is populated with the cover photo UID — the frontend uses it
// only as a fallback hint and renders icons when no thumbnail is available.
func albumToResponse(a database.Album) AlbumResponse {
	return AlbumResponse{
		UID:         a.UID,
		Title:       a.Title,
		Description: a.Description,
		PhotoCount:  a.PhotoCount,
		Thumb:       a.CoverPhotoUID,
		Type:        a.Type,
		Favorite:    a.Favorite,
		Location:    a.Location,
		Category:    a.Category,
		Notes:       a.Notes,
		Filter:      a.Filter,
		Order:       a.Order,
		CreatedAt:   a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// Validation limits for album text fields added by task 332a727c. Notes
// is capped at 8 KiB to match the spec; the others are not bounded here
// because the underlying TEXT column has no size restriction and
// PhotoPrism does not impose one either.
const albumNotesMaxBytes = 8 * 1024

// requireAlbumWriter returns the configured AlbumWriter; on missing
// configuration it writes a 503 error response and returns nil.
func (h *AlbumsHandler) requireAlbumWriter(w http.ResponseWriter) database.AlbumWriter {
	if h.albumRepo != nil {
		return h.albumRepo
	}
	respondError(w, http.StatusServiceUnavailable, "album storage not available")
	return nil
}

// requirePhotoReader returns the configured PhotoReader; on missing
// configuration it writes a 503 error response and returns nil.
func (h *AlbumsHandler) requirePhotoReader(w http.ResponseWriter) database.PhotoReader {
	if h.photoRepo != nil {
		return h.photoRepo
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// List returns the albums matching the supplied filter+pagination params.
// Supported query params: q (search), type, favorite, sort, count/limit, offset.
// "count" is preserved for backward compatibility with the previous handler.
func (h *AlbumsHandler) List(w http.ResponseWriter, r *http.Request) {
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}

	q := r.URL.Query()
	query := database.AlbumQuery{
		Type:   firstNonEmpty(q.Get("type"), albumTypeFilterDefault),
		Search: q.Get("q"),
		SortBy: q.Get("sort"),
	}
	fav, ok := parseBool(q.Get("favorite"))
	if !ok {
		respondError(w, http.StatusBadRequest, "invalid favorite")
		return
	}
	query.Favorite = fav

	limit, offset, ok := parseAlbumLimitOffset(w, q)
	if !ok {
		return
	}
	query.Limit, query.Offset = limit, offset

	albums, err := repo.ListAlbums(r.Context(), query)
	if err != nil {
		log.Printf("albums list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	response := make([]AlbumResponse, 0, len(albums))
	for i := range albums {
		response = append(response, albumToResponse(albums[i]))
	}
	respondJSON(w, http.StatusOK, response)
}

// Get returns a single album.
func (h *AlbumsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}
	album, err := repo.GetAlbum(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "album not found")
		return
	}
	if err != nil {
		log.Printf("albums get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get album")
		return
	}
	respondJSON(w, http.StatusOK, albumToResponse(*album))
}

// albumWriteRequest is the JSON body shared by Create and Update. The
// Location / Category / Notes / Filter / Order fields map onto the
// native columns added by migration 037 (task 332a727c).
type albumWriteRequest struct {
	Title         string  `json:"title"`
	Description   *string `json:"description,omitempty"`
	Type          *string `json:"type,omitempty"`
	Favorite      *bool   `json:"favorite,omitempty"`
	Private       *bool   `json:"private,omitempty"`
	OrderBy       *string `json:"order_by,omitempty"`
	CoverPhotoUID *string `json:"cover_photo_uid,omitempty"`
	Location      *string `json:"location,omitempty"`
	Category      *string `json:"category,omitempty"`
	Notes         *string `json:"notes,omitempty"`
	Filter        *string `json:"filter,omitempty"`
	Order         *string `json:"order,omitempty"`
}

// applyAlbumWriteFields copies the supplied request fields into the target
// album. The handler uses pointers in the request struct to distinguish
// "key omitted" from "explicit zero value"; this helper centralises that
// switch so Create/Update stay below the cyclomatic-complexity limit.
// Field handling is split between two helpers so each stays inside the
// per-function complexity budget.
func applyAlbumWriteFields(album *database.Album, req albumWriteRequest) {
	applyAlbumCoreFields(album, req)
	applyAlbumExtraFields(album, req)
}

// applyAlbumCoreFields copies the long-standing albums columns from the
// request into the album record.
func applyAlbumCoreFields(album *database.Album, req albumWriteRequest) {
	if req.Description != nil {
		album.Description = *req.Description
	}
	if req.Type != nil {
		album.Type = *req.Type
	}
	if req.Favorite != nil {
		album.Favorite = *req.Favorite
	}
	if req.Private != nil {
		album.Private = *req.Private
	}
	if req.OrderBy != nil {
		album.OrderBy = *req.OrderBy
	}
	if req.CoverPhotoUID != nil {
		album.CoverPhotoUID = *req.CoverPhotoUID
	}
}

// applyAlbumExtraFields copies the task-332a727c gap-fix columns
// (location/category/notes/filter/order) from the request into the
// album record.
func applyAlbumExtraFields(album *database.Album, req albumWriteRequest) {
	if req.Location != nil {
		album.Location = *req.Location
	}
	if req.Category != nil {
		album.Category = *req.Category
	}
	if req.Notes != nil {
		album.Notes = *req.Notes
	}
	if req.Filter != nil {
		album.Filter = *req.Filter
	}
	if req.Order != nil {
		album.Order = *req.Order
	}
}

// validateAlbumWrite enforces the spec-defined cap on the new notes
// column (task 332a727c). Returns a 400-suitable message or empty.
func validateAlbumWrite(req albumWriteRequest) string {
	if req.Notes != nil && len(*req.Notes) > albumNotesMaxBytes {
		return "notes exceeds 8 KiB limit"
	}
	return ""
}

// Create creates a new album.
func (h *AlbumsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req albumWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}
	if msg := validateAlbumWrite(req); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}

	album := &database.Album{Title: req.Title}
	applyAlbumWriteFields(album, req)
	if session := middleware.GetSessionFromContext(r.Context()); session != nil {
		album.CreatedBy = session.UserUID
	}

	if err := repo.CreateAlbum(r.Context(), album); err != nil {
		log.Printf("albums create: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumCreate, audit.EntityAlbum, album.UID,
		map[string]any{"title": album.Title},
	)
	respondJSON(w, http.StatusCreated, albumToResponse(*album))
}

// Update applies a partial update to an album. Omitted JSON keys leave the
// existing column untouched; explicit zero values (empty strings, false)
// are honored.
func (h *AlbumsHandler) Update(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	var req albumWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if msg := validateAlbumWrite(req); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}

	album, err := repo.GetAlbum(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "album not found")
		return
	}
	if err != nil {
		log.Printf("albums update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get album")
		return
	}

	if req.Title != "" {
		album.Title = req.Title
		// Clear the slug so the writer regenerates it from the new title.
		album.Slug = ""
	}
	applyAlbumWriteFields(album, req)

	if err := repo.UpdateAlbum(r.Context(), album); err != nil {
		log.Printf("albums update %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to update album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumUpdate, audit.EntityAlbum, album.UID,
		map[string]any{"title": album.Title},
	)
	respondJSON(w, http.StatusOK, albumToResponse(*album))
}

// Delete hard-deletes an album.
//
//nolint:dupl // intentionally mirrors SmartAlbumsHandler.Delete shape; merging would mix unrelated repos
func (h *AlbumsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}
	if err := repo.DeleteAlbum(r.Context(), uid); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "album not found")
			return
		}
		log.Printf("albums delete %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to delete album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumDelete, audit.EntityAlbum, uid, nil,
	)
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetPhotos returns the photos belonging to an album, applying the same
// filter+pagination grammar as GET /api/v1/photos.
func (h *AlbumsHandler) GetPhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	filter, ok := parsePhotoFilter(w, r)
	if !ok {
		return
	}
	filter.AlbumUID = uid

	photos, _, err := reader.ListPhotos(r.Context(), filter)
	if err != nil {
		log.Printf("albums get photos %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}
	response := make([]PhotoResponse, 0, len(photos))
	for i := range photos {
		response = append(response, nativePhotoToResponse(photos[i]))
	}
	respondJSON(w, http.StatusOK, response)
}

// albumPhotoRequest represents a request to add or remove photos from an album.
type albumPhotoRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// parseAlbumPhotoMutation extracts the album UID + photo UID list shared by
// AddPhotos and RemovePhotos. On any validation failure it writes the
// response and returns ok=false.
func (h *AlbumsHandler) parseAlbumPhotoMutation(
	w http.ResponseWriter, r *http.Request,
) (string, []string, database.AlbumWriter, bool) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return "", nil, nil, false
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return "", nil, nil, false
	}
	var req albumPhotoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return "", nil, nil, false
	}
	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return "", nil, nil, false
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return "", nil, nil, false
	}
	return uid, req.PhotoUIDs, repo, true
}

// AddPhotos adds photos to an album. Re-adding an existing UID is a silent
// no-op enforced by the writer (idempotent).
func (h *AlbumsHandler) AddPhotos(w http.ResponseWriter, r *http.Request) {
	uid, photoUIDs, repo, ok := h.parseAlbumPhotoMutation(w, r)
	if !ok {
		return
	}
	if err := repo.AddPhotos(r.Context(), uid, photoUIDs); err != nil {
		log.Printf("albums add photos %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to add photos to album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumPhotosAdd, audit.EntityAlbum, uid,
		map[string]any{"count": len(photoUIDs)},
	)
	respondJSON(w, http.StatusOK, map[string]int{"added": len(photoUIDs)})
}

// ClearPhotos removes all photos from an album. An optional body
// `{ photo_uids: [...] }` narrows the scope, matching the contract the
// frontend already relies on: an empty body == clear all, an explicit list
// == remove just those.
func (h *AlbumsHandler) ClearPhotos(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}

	// Body is optional; an empty body means "clear all".
	var req albumPhotoRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, errInvalidRequestBody)
			return
		}
	}

	uids := req.PhotoUIDs
	if len(uids) == 0 {
		all, err := repo.ListAlbumPhotoUIDs(r.Context(), uid)
		if err != nil {
			log.Printf("albums clear list %s: %v", sanitizeForLog(uid), err)
			respondError(w, http.StatusInternalServerError, "failed to get album photos")
			return
		}
		uids = all
	}
	if len(uids) == 0 {
		respondJSON(w, http.StatusOK, map[string]int{"removed": 0})
		return
	}
	if err := repo.RemovePhotos(r.Context(), uid, uids); err != nil {
		log.Printf("albums clear remove %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to remove photos from album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumPhotosRemove, audit.EntityAlbum, uid,
		map[string]any{"count": len(uids)},
	)
	respondJSON(w, http.StatusOK, map[string]int{"removed": len(uids)})
}

// RemovePhotos removes specific photos from an album.
func (h *AlbumsHandler) RemovePhotos(w http.ResponseWriter, r *http.Request) {
	uid, photoUIDs, repo, ok := h.parseAlbumPhotoMutation(w, r)
	if !ok {
		return
	}
	if err := repo.RemovePhotos(r.Context(), uid, photoUIDs); err != nil {
		log.Printf("albums remove photos %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to remove photos from album")
		return
	}
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionAlbumPhotosRemove, audit.EntityAlbum, uid,
		map[string]any{"count": len(photoUIDs)},
	)
	respondJSON(w, http.StatusOK, map[string]int{"removed": len(photoUIDs)})
}

// firstNonEmpty returns the first non-empty string from the supplied
// values, or "" when all of them are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseAlbumLimitOffset extracts limit (or its legacy alias "count") and
// offset from the query string. On invalid input it writes a 400 and
// returns ok=false.
func parseAlbumLimitOffset(w http.ResponseWriter, q map[string][]string) (int, int, bool) {
	limit, ok := parseNonNegativeQueryInt(w, q, "count", 0)
	if !ok {
		return 0, 0, false
	}
	if limit2, ok := parseNonNegativeQueryInt(w, q, "limit", limit); ok {
		limit = limit2
	} else {
		return 0, 0, false
	}
	if limit == 0 {
		limit = constants.DefaultHandlerPageSize
	}
	offset, ok := parseNonNegativeQueryInt(w, q, "offset", 0)
	if !ok {
		return 0, 0, false
	}
	return limit, offset, true
}

// parseNonNegativeQueryInt looks up `key` in q, returning `fallback` when
// the key is absent or empty. Non-integer or negative values write a 400
// and return ok=false.
func parseNonNegativeQueryInt(
	w http.ResponseWriter, q map[string][]string, key string, fallback int,
) (int, bool) {
	vals := q[key]
	if len(vals) == 0 || vals[0] == "" {
		return fallback, true
	}
	v, err := strconv.Atoi(vals[0])
	if err != nil || v < 0 {
		respondError(w, http.StatusBadRequest, "invalid "+key)
		return 0, false
	}
	return v, true
}
