package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// smartAlbumNameMaxLen caps the human-supplied name. The column is TEXT, so
// the database itself imposes no limit; this is a sanity bound only.
const smartAlbumNameMaxLen = 255

// smartAlbumAllowedFilterKeys lists every JSON key accepted in the saved
// filters blob. The validation step in parseSmartAlbumWriteRequest rejects
// any other key with 400 — extensions to this list must be coordinated
// with the matching change to parsePhotoFilter.
var smartAlbumAllowedFilterKeys = map[string]struct{}{
	"label_uids":   {},
	"subject_uids": {},
	"favorite":     {},
	"taken_from":   {},
	"taken_to":     {},
	"min_lat":      {},
	"min_lng":      {},
	"max_lat":      {},
	"max_lng":      {},
	"q":            {},
	"sort":         {},
}

// SmartAlbumsHandler serves the /api/v1/smart-albums/* endpoints. It owns a
// SmartAlbumWriter for the CRUD endpoints and a PhotoReader so the photos
// sub-endpoint can re-evaluate the saved filters against the live photos
// table without duplicating the existing filter+pagination logic.
type SmartAlbumsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager

	repo      database.SmartAlbumWriter
	photoRepo database.PhotoReader
}

// NewSmartAlbumsHandler creates a new smart albums handler. repo backs
// every CRUD endpoint and photoRepo backs the saved-search evaluation
// endpoint. Either may be nil in environments where those paths are
// unused; the handler then surfaces a 503.
func NewSmartAlbumsHandler(
	cfg *config.Config, sm *middleware.SessionManager,
	repo database.SmartAlbumWriter, photoRepo database.PhotoReader,
) *SmartAlbumsHandler {
	return &SmartAlbumsHandler{
		config:         cfg,
		sessionManager: sm,
		repo:           repo,
		photoRepo:      photoRepo,
	}
}

// SmartAlbumResponse is the wire shape returned by the smart albums
// endpoints. Filters round-trips back to the client exactly as it was
// stored so the edit modal can pre-fill its controls.
type SmartAlbumResponse struct {
	UID              string         `json:"uid"`
	Name             string         `json:"name"`
	Filters          map[string]any `json:"filters"`
	PhotoCount       int            `json:"photo_count"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
	CreatedByUserUID string         `json:"created_by_user_uid"`
}

// smartAlbumToResponse maps a database.SmartAlbum to the API wire shape.
// PhotoCount is filled separately by the list/get endpoints — pass 0 when
// the count is unknown.
func smartAlbumToResponse(a database.SmartAlbum, photoCount int) SmartAlbumResponse {
	filters := a.Filters
	if filters == nil {
		filters = map[string]any{}
	}
	return SmartAlbumResponse{
		UID:              a.UID,
		Name:             a.Name,
		Filters:          filters,
		PhotoCount:       photoCount,
		CreatedAt:        a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        a.UpdatedAt.UTC().Format(time.RFC3339),
		CreatedByUserUID: a.CreatedByUserUID,
	}
}

// requireSmartAlbumWriter returns the configured SmartAlbumWriter; on
// missing configuration it writes a 503 error response and returns nil.
func (h *SmartAlbumsHandler) requireSmartAlbumWriter(w http.ResponseWriter) database.SmartAlbumWriter {
	if h.repo != nil {
		return h.repo
	}
	respondError(w, http.StatusServiceUnavailable, "smart album storage not available")
	return nil
}

// requirePhotoReader returns the configured PhotoReader; on missing
// configuration it writes a 503 error response and returns nil.
func (h *SmartAlbumsHandler) requirePhotoReader(w http.ResponseWriter) database.PhotoReader {
	if h.photoRepo != nil {
		return h.photoRepo
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// List returns every smart album in the system. The photo count for each
// album is computed by re-running the saved filter as a count query
// against the photos table.
func (h *SmartAlbumsHandler) List(w http.ResponseWriter, r *http.Request) {
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}
	albums, err := repo.ListSmartAlbums(r.Context())
	if err != nil {
		log.Printf("smart albums list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list smart albums")
		return
	}

	resp := make([]SmartAlbumResponse, 0, len(albums))
	reader := h.photoRepo
	for i := range albums {
		count := 0
		if reader != nil {
			c, err := h.countMatches(r.Context(), reader, albums[i].Filters)
			if err == nil {
				count = c
			} else {
				log.Printf("smart albums list count %s: %v", sanitizeForLog(albums[i].UID), err)
			}
		}
		resp = append(resp, smartAlbumToResponse(albums[i], count))
	}
	respondJSON(w, http.StatusOK, resp)
}

// Get returns a single smart album by UID.
func (h *SmartAlbumsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing smart album UID")
		return
	}
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}
	album, err := repo.GetSmartAlbum(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "smart album not found")
		return
	}
	if err != nil {
		log.Printf("smart albums get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get smart album")
		return
	}
	count := 0
	if h.photoRepo != nil {
		if c, err := h.countMatches(r.Context(), h.photoRepo, album.Filters); err == nil {
			count = c
		} else {
			log.Printf("smart albums get count %s: %v", sanitizeForLog(uid), err)
		}
	}
	respondJSON(w, http.StatusOK, smartAlbumToResponse(*album, count))
}

// smartAlbumWriteRequest is the JSON body accepted by Create and Update.
type smartAlbumWriteRequest struct {
	Name    string         `json:"name"`
	Filters map[string]any `json:"filters"`
}

// parseSmartAlbumWriteRequest decodes the JSON body and rejects malformed
// payloads with the appropriate HTTP error. Unknown filter keys are
// rejected with 400 to keep the storage shape forward-compatible.
func parseSmartAlbumWriteRequest(r *http.Request) (smartAlbumWriteRequest, string) {
	var req smartAlbumWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, errInvalidRequestBody
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, "name is required"
	}
	if len(req.Name) > smartAlbumNameMaxLen {
		return req, "name too long"
	}
	if req.Filters == nil {
		req.Filters = map[string]any{}
	}
	if msg := validateSmartAlbumFilters(req.Filters); msg != "" {
		return req, msg
	}
	return req, ""
}

// validateSmartAlbumFilters rejects unknown filter keys and runs a light
// type check so that a malformed body fails at write time rather than at
// query time. The full validation lives in parsePhotoFilter, which runs
// on every Photos query — this helper duplicates only the keys-allowed
// check so callers cannot smuggle arbitrary JSON into the JSONB column.
func validateSmartAlbumFilters(filters map[string]any) string {
	for k := range filters {
		if _, ok := smartAlbumAllowedFilterKeys[k]; !ok {
			return "unknown filter key: " + k
		}
	}
	return ""
}

// Create inserts a new smart album. Requires write access.
func (h *SmartAlbumsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	req, msg := parseSmartAlbumWriteRequest(r)
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}

	album := &database.SmartAlbum{
		Name:    req.Name,
		Filters: req.Filters,
	}
	if session := middleware.GetSessionFromContext(r.Context()); session != nil {
		album.CreatedByUserUID = session.UserUID
	}
	if album.CreatedByUserUID == "" {
		respondError(w, http.StatusUnauthorized, "no session user")
		return
	}

	if err := repo.CreateSmartAlbum(r.Context(), album); err != nil {
		log.Printf("smart albums create: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create smart album")
		return
	}
	respondJSON(w, http.StatusCreated, smartAlbumToResponse(*album, 0))
}

// Update changes the name and/or filters of an existing smart album. The
// UID is immutable (renaming preserves bookmarks).
func (h *SmartAlbumsHandler) Update(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing smart album UID")
		return
	}
	req, msg := parseSmartAlbumWriteRequest(r)
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}

	album, err := repo.GetSmartAlbum(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "smart album not found")
		return
	}
	if err != nil {
		log.Printf("smart albums update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get smart album")
		return
	}

	album.Name = req.Name
	album.Filters = req.Filters
	if err := repo.UpdateSmartAlbum(r.Context(), album); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "smart album not found")
			return
		}
		log.Printf("smart albums update %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to update smart album")
		return
	}
	respondJSON(w, http.StatusOK, smartAlbumToResponse(*album, 0))
}

// Delete removes a smart album. Requires write access.
func (h *SmartAlbumsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing smart album UID")
		return
	}
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}
	if err := repo.DeleteSmartAlbum(r.Context(), uid); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "smart album not found")
			return
		}
		log.Printf("smart albums delete %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to delete smart album")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetPhotos resolves the saved filters into a live `GET /photos`
// evaluation. Query-string overrides of sort/limit/offset are honored so
// the detail page can paginate without rewriting the saved filter.
func (h *SmartAlbumsHandler) GetPhotos(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing smart album UID")
		return
	}
	repo := h.requireSmartAlbumWriter(w)
	if repo == nil {
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	album, err := repo.GetSmartAlbum(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "smart album not found")
		return
	}
	if err != nil {
		log.Printf("smart albums get photos %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get smart album")
		return
	}

	filter, ok := smartAlbumFilterToPhotoFilter(w, album.Filters, r.URL.Query())
	if !ok {
		return
	}

	photos, total, err := reader.ListPhotos(r.Context(), filter)
	if err != nil {
		log.Printf("smart albums list photos %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}

	resp := PhotoListResponse{
		Photos: make([]PhotoResponse, 0, len(photos)),
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}
	if resp.Limit == 0 {
		resp.Limit = constants.DefaultHandlerPageSize
	}
	for i := range photos {
		resp.Photos = append(resp.Photos, nativePhotoToResponse(photos[i]))
	}
	respondJSON(w, http.StatusOK, resp)
}

// countMatches evaluates the saved filters against the photos table and
// returns just the total count (it reuses ListPhotos with limit=1 so the
// existing filter+sort plumbing stays in one place).
func (h *SmartAlbumsHandler) countMatches(
	ctx context.Context, reader database.PhotoReader, filters map[string]any,
) (int, error) {
	filter := smartAlbumFiltersToFilter(filters)
	filter.Limit = 1
	filter.Offset = 0
	_, total, err := reader.ListPhotos(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("smart album count: %w", err)
	}
	return total, nil
}

// smartAlbumFilterToPhotoFilter converts the saved filter map into a
// database.PhotoFilter, honouring optional overrides from the request
// query string (sort/limit/offset). On a validation failure it writes a
// 400 to w and returns ok=false.
func smartAlbumFilterToPhotoFilter(
	w http.ResponseWriter, filters map[string]any, q url.Values,
) (database.PhotoFilter, bool) {
	filter := smartAlbumFiltersToFilter(filters)

	if v := q.Get("sort"); v != "" {
		filter.SortBy = v
	}
	limit, offset, ok := parseLimitOffset(w, q.Get("limit"), q.Get("offset"))
	if !ok {
		return filter, false
	}
	if limit > 0 {
		filter.Limit = limit
	}
	if offset > 0 {
		filter.Offset = offset
	}
	return filter, true
}

// smartAlbumFiltersToFilter is the pure conversion: no HTTP response, no
// query-string overrides. Unknown / malformed values are silently dropped
// (per the spec: filters referencing deleted entities skip silently and
// we log a warning at the call site).
func smartAlbumFiltersToFilter(filters map[string]any) database.PhotoFilter {
	var filter database.PhotoFilter

	filter.LabelUIDs = asStringSlice(filters["label_uids"])
	filter.SubjectUIDs = asStringSlice(filters["subject_uids"])
	if b, ok := asBool(filters["favorite"]); ok {
		v := b
		filter.Favorite = &v
	}
	if s, ok := filters["q"].(string); ok && s != "" {
		filter.Search = s
	}
	if s, ok := filters["sort"].(string); ok && s != "" {
		filter.SortBy = s
	}
	if t, ok := asTime(filters["taken_from"]); ok {
		filter.TakenFrom = &t
	}
	if t, ok := asTime(filters["taken_to"]); ok {
		filter.TakenTo = &t
	}
	if box, ok := asBBox(filters); ok {
		filter.BBox = box
	}
	return filter
}

// asStringSlice returns the value as a []string. JSON unmarshals arrays
// into []any, so we expect that shape and convert. Single strings are
// silently coerced into a 1-element slice for forwards compatibility
// with handwritten payloads.
func asStringSlice(v any) []string {
	switch val := v.(type) {
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return val
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	}
	return nil
}

// asBool returns the value as a bool when convertible.
func asBool(v any) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return false, false
		}
		return b, true
	}
	return false, false
}

// asTime parses an RFC3339 timestamp string. Numeric values are not
// supported — the API contract documents RFC3339.
func asTime(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// asBBox extracts a bounding box from the saved filter map. All four
// corners are required; otherwise we return ok=false and the geo filter
// is silently dropped.
func asBBox(filters map[string]any) (*database.BBox, bool) {
	corners := []string{"min_lat", "min_lng", "max_lat", "max_lng"}
	nums := make([]float64, len(corners))
	for i, key := range corners {
		f, ok := asFloat(filters[key])
		if !ok {
			return nil, false
		}
		nums[i] = f
	}
	return &database.BBox{MinLat: nums[0], MinLng: nums[1], MaxLat: nums[2], MaxLng: nums[3]}, true
}

// asFloat coerces a JSON number (or numeric string) into float64.
func asFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
