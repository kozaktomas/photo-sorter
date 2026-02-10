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

// AlbumsHandler handles album-related endpoints
type AlbumsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewAlbumsHandler creates a new albums handler
func NewAlbumsHandler(cfg *config.Config, sm *middleware.SessionManager) *AlbumsHandler {
	return &AlbumsHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// AlbumResponse represents an album in API responses
type AlbumResponse struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PhotoCount  int    `json:"photo_count"`
	Thumb       string `json:"thumb"`
	Type        string `json:"type"`
	Favorite    bool   `json:"favorite"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func albumToResponse(a photoprism.Album) AlbumResponse {
	return AlbumResponse{
		UID:         a.UID,
		Title:       a.Title,
		Description: a.Description,
		PhotoCount:  a.PhotoCount,
		Thumb:       a.Thumb,
		Type:        a.Type,
		Favorite:    a.Favorite,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

// List returns all albums
func (h *AlbumsHandler) List(w http.ResponseWriter, r *http.Request) {
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
	order := r.URL.Query().Get("order")
	query := r.URL.Query().Get("q")

	albums, err := pp.GetAlbums(count, offset, order, query, "album")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	response := make([]AlbumResponse, len(albums))
	for i := range albums {
		response[i] = albumToResponse(albums[i])
	}

	respondJSON(w, http.StatusOK, response)
}

// Get returns a single album
func (h *AlbumsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	album, err := pp.GetAlbum(uid)
	if err != nil {
		respondError(w, http.StatusNotFound, "album not found")
		return
	}

	respondJSON(w, http.StatusOK, albumToResponse(*album))
}

// CreateRequest represents a create album request
type CreateRequest struct {
	Title string `json:"title"`
}

// Create creates a new album
func (h *AlbumsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	album, err := pp.CreateAlbum(req.Title)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create album")
		return
	}

	respondJSON(w, http.StatusCreated, albumToResponse(*album))
}

// PhotoResponse represents a photo in API responses
type PhotoResponse struct {
	UID          string  `json:"uid"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	TakenAt      string  `json:"taken_at"`
	Year         int     `json:"year"`
	Month        int     `json:"month"`
	Day          int     `json:"day"`
	Hash         string  `json:"hash"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Country      string  `json:"country"`
	Favorite     bool    `json:"favorite"`
	Private      bool    `json:"private"`
	Type         string  `json:"type"`
	OriginalName string  `json:"original_name"`
	FileName     string  `json:"file_name"`
	CameraModel  string  `json:"camera_model"`
}

func photoToResponse(p photoprism.Photo) PhotoResponse {
	return PhotoResponse{
		UID:          p.UID,
		Title:        p.Title,
		Description:  p.Description,
		TakenAt:      p.TakenAt,
		Year:         p.Year,
		Month:        p.Month,
		Day:          p.Day,
		Hash:         p.Hash,
		Width:        p.Width,
		Height:       p.Height,
		Lat:          p.Lat,
		Lng:          p.Lng,
		Country:      p.Country,
		Favorite:     p.Favorite,
		Private:      p.Private,
		Type:         p.Type,
		OriginalName: p.OriginalName,
		FileName:     p.FileName,
		CameraModel:  p.CameraModel,
	}
}

// GetPhotos returns photos in an album
func (h *AlbumsHandler) GetPhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}

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

	photos, err := pp.GetAlbumPhotos(uid, count, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}

	response := make([]PhotoResponse, len(photos))
	for i, p := range photos {
		response[i] = photoToResponse(p)
	}

	respondJSON(w, http.StatusOK, response)
}

// albumPhotoRequest represents a request to add or remove photos from an album
type albumPhotoRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// parseAlbumPhotoRequest parses and validates an album photo modification request.
// Returns the album UID, photo UIDs, and PhotoPrism client; sends error response on failure.
func (h *AlbumsHandler) parseAlbumPhotoRequest(w http.ResponseWriter, r *http.Request) (string, []string, *photoprism.PhotoPrism) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return "", nil, nil
	}

	var req albumPhotoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return "", nil, nil
	}

	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return "", nil, nil
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return "", nil, nil
	}

	return uid, req.PhotoUIDs, pp
}

// AddPhotos adds photos to an album
func (h *AlbumsHandler) AddPhotos(w http.ResponseWriter, r *http.Request) {
	uid, photoUIDs, pp := h.parseAlbumPhotoRequest(w, r)
	if pp == nil {
		return
	}

	if err := pp.AddPhotosToAlbum(uid, photoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add photos to album")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"added": len(photoUIDs)})
}

// ClearPhotos removes all photos from an album
func (h *AlbumsHandler) ClearPhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing album UID")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Get all photos in album
	photos, err := pp.GetAlbumPhotos(uid, constants.MaxPhotosPerFetch, 0)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get album photos")
		return
	}

	if len(photos) == 0 {
		respondJSON(w, http.StatusOK, map[string]int{"removed": 0})
		return
	}

	// Collect photo UIDs
	photoUIDs := make([]string, len(photos))
	for i, p := range photos {
		photoUIDs[i] = p.UID
	}

	// Remove photos from album
	if err := pp.RemovePhotosFromAlbum(uid, photoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove photos from album")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"removed": len(photoUIDs)})
}

// RemovePhotos removes specific photos from an album
func (h *AlbumsHandler) RemovePhotos(w http.ResponseWriter, r *http.Request) {
	uid, photoUIDs, pp := h.parseAlbumPhotoRequest(w, r)
	if pp == nil {
		return
	}

	if err := pp.RemovePhotosFromAlbum(uid, photoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove photos from album")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"removed": len(photoUIDs)})
}
