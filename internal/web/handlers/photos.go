package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/trash"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// PhotosHandler handles photo-related endpoints.
//
// The native endpoints (List, Get, Thumbnail, Download, Update, BatchEdit,
// BatchArchive, BatchRestore, ListTrash, BatchPurge) read and write the
// local Postgres photos table plus the on-disk storage tree; they no longer
// proxy to PhotoPrism. The remaining endpoints (face/album/label batch ops,
// similarity, era estimation, etc.) still rely on the PhotoPrism client
// injected per request via middleware.MustGetPhotoPrism — they will be
// migrated in follow-up tasks.
type PhotosHandler struct {
	config          *config.Config
	sessionManager  *middleware.SessionManager
	embeddingReader database.EmbeddingReader

	// repo serves the native photos table for both GET and write endpoints.
	// May be nil in tests that do not exercise the native paths.
	repo database.PhotoWriter
	// store backs file-streaming endpoints (thumbnails, downloads). May be
	// nil in tests that do not exercise the file-streaming paths.
	store *storage.Storage
	// labels backs the bulk-label endpoint (POST /photos/batch/labels). May
	// be nil in tests that do not exercise the label paths.
	labels database.LabelWriter

	// trashStore bundles the dependencies needed to hard-delete a photo:
	// the photo writer, the embedding writer, the face writer, and the
	// on-disk Storage. It is populated lazily inside NewPhotosHandler from
	// the registered providers. When any dependency is missing (during the
	// PhotoPrism transition or in tests), the BatchPurge endpoint surfaces
	// a 503 rather than partially purging a photo.
	trashStore *trash.Store
}

// NewPhotosHandler creates a new photos handler. repo, store, and labels
// back the native endpoints (list/get/thumb/download/update/batch and bulk
// label assignment) and may be nil in environments where those paths are
// unused.
func NewPhotosHandler(
	cfg *config.Config, sm *middleware.SessionManager,
	repo database.PhotoWriter, store *storage.Storage,
	labels database.LabelWriter,
) *PhotosHandler {
	h := &PhotosHandler{
		config:         cfg,
		sessionManager: sm,
		repo:           repo,
		store:          store,
		labels:         labels,
	}

	// Try to get an embedding reader from PostgreSQL.
	if r, err := database.GetEmbeddingReader(context.Background()); err == nil {
		h.embeddingReader = r
	}

	// Resolve the trash dependencies (embedding writer + face writer). All
	// four pieces (photo writer, embedding writer, face writer, storage)
	// must be present for the BatchPurge endpoint to function; we leave
	// trashStore nil otherwise and the endpoint returns 503.
	if repo != nil && store != nil {
		if ew, err := database.GetEmbeddingWriter(context.Background()); err == nil {
			if fw, err := database.GetFaceWriter(context.Background()); err == nil {
				h.trashStore = &trash.Store{
					Photos:     repo,
					Embeddings: ew,
					Faces:      fw,
					Files:      store,
				}
			}
		}
	}

	return h
}

// TrashStore returns the configured trash store, or nil if the
// dependencies were not all available at startup. Exposed so the serve
// command can wire the auto-purge daemon against the same store the
// BatchPurge handler uses.
func (h *PhotosHandler) TrashStore() *trash.Store {
	return h.trashStore
}

// getEmbeddingReader returns the cached embedding reader, falling back to fetching from the database.
// On failure, sends an error response and returns (nil, false).
func (h *PhotosHandler) getEmbeddingReader(w http.ResponseWriter) (database.EmbeddingReader, bool) {
	if h.embeddingReader != nil {
		return h.embeddingReader, true
	}
	reader, err := database.GetEmbeddingReader(context.Background())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "embeddings not available")
		return nil, false
	}
	return reader, true
}

// RefreshReader reloads the embedding reader from the database.
// Called after processing or index rebuild to pick up new data.
func (h *PhotosHandler) RefreshReader() {
	if reader, err := database.GetEmbeddingReader(context.Background()); err == nil {
		h.embeddingReader = reader
	}
}

// PhotoResponse represents a photo in API responses. The shape mirrors the
// previous PhotoPrism passthrough so the frontend keeps working. The
// trailing block (keywords, panorama, scan, quality, time_zone,
// taken_at_offset, exif_*) carries the metadata gap-fix fields added in
// migration 036.
type PhotoResponse struct {
	UID           string   `json:"uid"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	TakenAt       string   `json:"taken_at"`
	Year          int      `json:"year"`
	Month         int      `json:"month"`
	Day           int      `json:"day"`
	Hash          string   `json:"hash"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Lat           float64  `json:"lat"`
	Lng           float64  `json:"lng"`
	Country       string   `json:"country"`
	Favorite      bool     `json:"favorite"`
	Private       bool     `json:"private"`
	Type          string   `json:"type"`
	OriginalName  string   `json:"original_name"`
	FileName      string   `json:"file_name"`
	CameraModel   string   `json:"camera_model"`
	Keywords      []string `json:"keywords"`
	Panorama      bool     `json:"panorama"`
	Scan          bool     `json:"scan"`
	Quality       int16    `json:"quality"`
	TimeZone      string   `json:"time_zone"`
	TakenAtOffset int      `json:"taken_at_offset"`
	ExifArtist    string   `json:"exif_artist"`
	ExifCopyright string   `json:"exif_copyright"`
	ExifLicense   string   `json:"exif_license"`
	ExifSoftware  string   `json:"exif_software"`
}

// PhotoListResponse is the envelope returned by List.
type PhotoListResponse struct {
	Photos []PhotoResponse `json:"photos"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// requirePhotoReader returns the configured PhotoReader; on missing
// configuration it writes a 503 error response and returns nil.
func (h *PhotosHandler) requirePhotoReader(w http.ResponseWriter) database.PhotoReader {
	if h.repo != nil {
		return h.repo
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// requirePhotoWriter returns the configured PhotoWriter; on missing
// configuration it writes a 503 error response and returns nil.
func (h *PhotosHandler) requirePhotoWriter(w http.ResponseWriter) database.PhotoWriter {
	if h.repo != nil {
		return h.repo
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// requireStorage returns the configured Storage; on missing configuration
// it writes a 503 error response and returns nil.
func (h *PhotosHandler) requireStorage(w http.ResponseWriter) *storage.Storage {
	if h.store != nil {
		return h.store
	}
	respondError(w, http.StatusServiceUnavailable, "photo storage not available")
	return nil
}

// parseBool parses an optional boolean query parameter. The empty string
// returns (nil, true); "1"/"true"/"0"/"false" (case-insensitive) return a
// pointer. Any other value returns (nil, false) so the caller can 400.
func parseBool(s string) (*bool, bool) {
	if s == "" {
		return nil, true
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return nil, false
	}
	return &b, true
}

// parsePhotoFilter translates URL query params into a database.PhotoFilter.
// On invalid input it writes an error response and returns (filter, false).
func parsePhotoFilter(w http.ResponseWriter, r *http.Request) (database.PhotoFilter, bool) {
	q := r.URL.Query()
	var filter database.PhotoFilter
	filter.AlbumUID = q.Get("album_uid")
	filter.LabelUIDs = q["label_uid"]
	filter.SubjectUIDs = q["subject_uid"]
	filter.Search = q.Get("q")
	filter.SortBy = q.Get("sort")

	for key, set := range map[string]func(*bool){
		"favorite": func(v *bool) { filter.Favorite = v },
		"private":  func(v *bool) { filter.Private = v },
	} {
		v, ok := parseBool(q.Get(key))
		if !ok {
			respondError(w, http.StatusBadRequest, "invalid "+key)
			return filter, false
		}
		set(v)
	}

	// Archived has a default of false (exclude archived). An explicit "true"
	// flips to only archived; any other explicit value passes through.
	if raw := q.Get("archived"); raw != "" {
		v, ok := parseBool(raw)
		if !ok {
			respondError(w, http.StatusBadRequest, "invalid archived")
			return filter, false
		}
		filter.Archived = v
	}

	from, to, ok := parseTakenRange(w, q.Get("taken_from"), q.Get("taken_to"))
	if !ok {
		return filter, false
	}
	filter.TakenFrom, filter.TakenTo = from, to

	box, ok := parseBBox(w, q)
	if !ok {
		return filter, false
	}
	filter.BBox = box

	limit, offset, ok := parseLimitOffset(w, q.Get("limit"), q.Get("offset"))
	if !ok {
		return filter, false
	}
	filter.Limit, filter.Offset = limit, offset

	return filter, true
}

func parseTakenRange(w http.ResponseWriter, fromStr, toStr string) (*time.Time, *time.Time, bool) {
	var from, to *time.Time
	if fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid taken_from")
			return nil, nil, false
		}
		from = &t
	}
	if toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid taken_to")
			return nil, nil, false
		}
		to = &t
	}
	return from, to, true
}

// parseBBox returns the bbox filter when all four corner params are set, no
// bbox when none are set, and an error when only some are present.
func parseBBox(w http.ResponseWriter, q url.Values) (*database.BBox, bool) {
	keys := []string{"min_lat", "min_lng", "max_lat", "max_lng"}
	values := make([]string, len(keys))
	present := 0
	for i, k := range keys {
		values[i] = q.Get(k)
		if values[i] != "" {
			present++
		}
	}
	if present == 0 {
		return nil, true
	}
	if present != len(keys) {
		respondError(w, http.StatusBadRequest, "bbox filter requires all of min_lat, min_lng, max_lat, max_lng")
		return nil, false
	}
	nums := make([]float64, len(keys))
	for i, v := range values {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid "+keys[i])
			return nil, false
		}
		nums[i] = f
	}
	return &database.BBox{MinLat: nums[0], MinLng: nums[1], MaxLat: nums[2], MaxLng: nums[3]}, true
}

func parseLimitOffset(w http.ResponseWriter, limitStr, offsetStr string) (int, int, bool) {
	limit := 0
	if limitStr != "" {
		v, err := strconv.Atoi(limitStr)
		if err != nil || v < 0 {
			respondError(w, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		limit = v
	}
	offset := 0
	if offsetStr != "" {
		v, err := strconv.Atoi(offsetStr)
		if err != nil || v < 0 {
			respondError(w, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = v
	}
	return limit, offset, true
}

// nativePhotoToResponse maps a native database.Photo to the wire shape used
// by the existing PhotoResponse struct so the frontend contract stays stable.
func nativePhotoToResponse(p database.Photo) PhotoResponse {
	var (
		takenAtStr       string
		year, month, day int
	)
	if p.TakenAt != nil {
		takenAtStr = p.TakenAt.UTC().Format(time.RFC3339)
		year, month, day = p.TakenAt.Year(), int(p.TakenAt.Month()), p.TakenAt.Day()
	}
	lat, lng := 0.0, 0.0
	if p.Lat != nil {
		lat = *p.Lat
	}
	if p.Lng != nil {
		lng = *p.Lng
	}
	keywords := p.Keywords
	if keywords == nil {
		// A photo with no keywords renders as `"keywords": []` rather than
		// `"keywords": null`, which is what the frontend's type signatures
		// already expect.
		keywords = []string{}
	}
	return PhotoResponse{
		UID:           p.UID,
		Title:         p.Title,
		Description:   p.Description,
		TakenAt:       takenAtStr,
		Year:          year,
		Month:         month,
		Day:           day,
		Hash:          p.FileHash,
		Width:         p.FileWidth,
		Height:        p.FileHeight,
		Lat:           lat,
		Lng:           lng,
		Favorite:      p.Favorite,
		Private:       p.Private,
		Type:          photoTypeFromMime(p.FileMime),
		OriginalName:  p.FileName,
		FileName:      p.FileName,
		CameraModel:   p.CameraModel,
		Keywords:      keywords,
		Panorama:      p.Panorama,
		Scan:          p.Scan,
		Quality:       p.Quality,
		TimeZone:      p.TimeZone,
		TakenAtOffset: p.TakenAtOffset,
		ExifArtist:    p.ExifArtist,
		ExifCopyright: p.ExifCopyright,
		ExifLicense:   p.ExifLicense,
		ExifSoftware:  p.ExifSoftware,
	}
}

// photoTypeFromMime returns a coarse media type ("image" / "video") that
// mirrors the PhotoPrism Type field consumed by the frontend.
func photoTypeFromMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "video/"):
		return "video"
	default:
		return "image"
	}
}

// List returns photos filtered + paginated from the native photos table.
func (h *PhotosHandler) List(w http.ResponseWriter, r *http.Request) {
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	filter, ok := parsePhotoFilter(w, r)
	if !ok {
		return
	}

	photos, total, err := reader.ListPhotos(r.Context(), filter)
	if err != nil {
		log.Printf("photos list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}

	response := PhotoListResponse{
		Photos: make([]PhotoResponse, 0, len(photos)),
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}
	if response.Limit == 0 {
		response.Limit = constants.DefaultHandlerPageSize
	}
	for i := range photos {
		response.Photos = append(response.Photos, nativePhotoToResponse(photos[i]))
	}
	respondJSON(w, http.StatusOK, response)
}

// Get returns a single photo. Archived photos return 404 unless the caller
// passes ?include_archived=true.
func (h *PhotosHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}

	photo, err := reader.GetPhoto(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}
	if err != nil {
		log.Printf("photos get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photo")
		return
	}

	includeArchived, _ := parseBool(r.URL.Query().Get("include_archived"))
	if photo.ArchivedAt != nil && (includeArchived == nil || !*includeArchived) {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	respondJSON(w, http.StatusOK, nativePhotoToResponse(*photo))
}

// photoUpdateFields holds the parsed + validated update payload. Only the
// fields whose corresponding JSON key was present in the request are
// non-nil. Zero-value pointers (e.g. *string == "") are intentional empty
// strings, not "unset".
type photoUpdateFields struct {
	title       *string
	description *string
	notes       *string
	takenAt     *time.Time
	lat         *float64
	lng         *float64
	favorite    *bool
	private     *bool
}

// titleMaxLen caps the title field. Mirrors the PhotoPrism title column.
const titleMaxLen = 255

// minTakenYear / maxTakenYear bound taken_at to a plausible photographic
// range; values outside the window almost always indicate a parsing bug
// rather than a legitimate vintage photograph.
const (
	minTakenYear = 1900
	maxTakenYear = 2100
)

// parsePhotoUpdate decodes the JSON body into a map first so we can tell
// "key omitted" from "key set to a zero value". A bare numeric zero in
// lat/lng or empty string title is preserved as an intentional update.
func parsePhotoUpdate(r *http.Request) (photoUpdateFields, string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return photoUpdateFields{}, errInvalidRequestBody
	}
	out, msg := decodePhotoUpdateFields(raw)
	if msg != "" {
		return out, msg
	}
	if out.title != nil && len(*out.title) > titleMaxLen {
		return out, "title too long"
	}
	return out, ""
}

// decodePhotoUpdateFields fans out per-field decoding so parsePhotoUpdate
// stays under the cyclomatic-complexity limit.
func decodePhotoUpdateFields(raw map[string]json.RawMessage) (photoUpdateFields, string) {
	var out photoUpdateFields
	steps := []func() string{
		func() string { return decodeStringField(raw, "title", &out.title) },
		func() string { return decodeStringField(raw, "description", &out.description) },
		func() string { return decodeStringField(raw, "notes", &out.notes) },
		func() string { return decodeTakenAt(raw, &out.takenAt) },
		func() string { return decodeLatLng(raw, &out.lat, &out.lng) },
		func() string { return decodeBoolField(raw, "favorite", &out.favorite) },
		func() string { return decodeBoolField(raw, "private", &out.private) },
	}
	for _, step := range steps {
		if msg := step(); msg != "" {
			return out, msg
		}
	}
	return out, ""
}

// decodeStringField copies raw[key] into *dest when present.
func decodeStringField(raw map[string]json.RawMessage, key string, dest **string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "invalid " + key
	}
	*dest = &s
	return ""
}

// decodeBoolField copies raw[key] into *dest when present.
func decodeBoolField(raw map[string]json.RawMessage, key string, dest **bool) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return "invalid " + key
	}
	*dest = &b
	return ""
}

// decodeTakenAt parses an RFC3339 timestamp and enforces the [minTakenYear,
// maxTakenYear] window.
func decodeTakenAt(raw map[string]json.RawMessage, dest **time.Time) string {
	v, ok := raw["taken_at"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "invalid taken_at"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "invalid taken_at"
	}
	if t.Year() < minTakenYear || t.Year() > maxTakenYear {
		return "taken_at out of range"
	}
	*dest = &t
	return ""
}

// decodeLatLng requires lat and lng to be supplied together and enforces
// the geodetic ranges.
func decodeLatLng(raw map[string]json.RawMessage, latDest, lngDest **float64) string {
	latRaw, latOK := raw["lat"]
	lngRaw, lngOK := raw["lng"]
	if !latOK && !lngOK {
		return ""
	}
	if latOK != lngOK {
		return "lat and lng must be provided together"
	}
	var lat, lng float64
	if err := json.Unmarshal(latRaw, &lat); err != nil {
		return "invalid lat"
	}
	if err := json.Unmarshal(lngRaw, &lng); err != nil {
		return "invalid lng"
	}
	if lat < -90 || lat > 90 {
		return "lat out of range"
	}
	if lng < -180 || lng > 180 {
		return "lng out of range"
	}
	*latDest = &lat
	*lngDest = &lng
	return ""
}

// applyPhotoUpdate mutates the given photo with the supplied fields.
func applyPhotoUpdate(p *database.Photo, f photoUpdateFields) {
	if f.title != nil {
		p.Title = *f.title
	}
	if f.description != nil {
		p.Description = *f.description
	}
	if f.notes != nil {
		p.Notes = *f.notes
	}
	if f.takenAt != nil {
		t := *f.takenAt
		p.TakenAt = &t
	}
	if f.lat != nil && f.lng != nil {
		lat, lng := *f.lat, *f.lng
		p.Lat, p.Lng = &lat, &lng
	}
	if f.favorite != nil {
		p.Favorite = *f.favorite
	}
	if f.private != nil {
		p.Private = *f.private
	}
}

// Update mutates a photo row in the native photos table. Only keys present
// in the JSON body are written; zero-valued keys (e.g. "title": "") are
// honored, but omitted keys leave the existing value alone.
func (h *PhotosHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}

	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}

	fields, errMsg := parsePhotoUpdate(r)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}

	writer := h.requirePhotoWriter(w)
	if writer == nil {
		return
	}

	photo, err := writer.GetPhoto(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}
	if err != nil {
		log.Printf("photos update get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photo")
		return
	}
	if photo.ArchivedAt != nil {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	applyPhotoUpdate(photo, fields)

	if updateErr := writer.UpdatePhoto(r.Context(), photo); updateErr != nil {
		if errors.Is(updateErr, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "photo not found")
			return
		}
		log.Printf("photos update %s: %v", sanitizeForLog(uid), updateErr)
		respondError(w, http.StatusInternalServerError, "failed to update photo")
		return
	}

	respondJSON(w, http.StatusOK, nativePhotoToResponse(*photo))
}

// Thumbnail streams a cached thumbnail from the on-disk thumb tree. A
// missing on-disk file returns 404 — regeneration is the responsibility of
// the process job.
func (h *PhotosHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	size := chi.URLParam(r, "size")
	if uid == "" || size == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID or size")
		return
	}
	if _, ok := storage.ValidThumbSizes[size]; !ok {
		respondError(w, http.StatusBadRequest, "invalid size")
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	store := h.requireStorage(w)
	if store == nil {
		return
	}
	photo, ok := loadPhoto(w, r, reader, uid, "thumbnail")
	if !ok {
		return
	}
	if photo.FileHash == "" {
		respondError(w, http.StatusNotFound, "photo has no thumbnail")
		return
	}
	rel, err := storage.ThumbRelPath(photo.FileHash, size)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid thumb path: "+err.Error())
		return
	}
	serveThumb(w, r, store, photo.FileHash, size, rel)
}

// loadPhoto fetches a photo and writes the appropriate error response on
// failure. The boolean second return is false when the response has been
// written and the caller must return.
func loadPhoto(
	w http.ResponseWriter, r *http.Request, reader database.PhotoReader, uid, op string,
) (*database.Photo, bool) {
	photo, err := reader.GetPhoto(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondError(w, http.StatusNotFound, "photo not found")
		return nil, false
	}
	if err != nil {
		log.Printf("photos %s %s: %v", op, sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photo")
		return nil, false
	}
	return photo, true
}

// serveThumb opens the relative thumb path from the storage tree and streams
// it back to the client with the long-cache headers and ETag the spec asks
// for. It writes the appropriate error response on failure.
func serveThumb(
	w http.ResponseWriter, r *http.Request, store *storage.Storage, hash, size, rel string,
) {
	f, err := store.OpenThumb(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondError(w, http.StatusNotFound, "thumbnail not found")
			return
		}
		log.Printf("open thumb %q: %v", sanitizeForLog(rel), err)
		respondError(w, http.StatusInternalServerError, "failed to open thumbnail")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		log.Printf("stat thumb %q: %v", sanitizeForLog(rel), err)
		respondError(w, http.StatusInternalServerError, "failed to stat thumbnail")
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", fmt.Sprintf(`"sha:%s:%s"`, hash, size))
	http.ServeContent(w, r, "", stat.ModTime(), f)
}

// Download streams the original primary file for a photo with Range support.
func (h *PhotosHandler) Download(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	store := h.requireStorage(w)
	if store == nil {
		return
	}
	photo, ok := loadPhoto(w, r, reader, uid, "download")
	if !ok {
		return
	}
	rel, fileName, mime, err := resolvePrimaryFile(r.Context(), reader, photo)
	if err != nil {
		log.Printf("photos download %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to resolve photo file")
		return
	}
	if rel == "" {
		respondError(w, http.StatusNotFound, "primary file not found")
		return
	}
	serveOriginal(w, r, store, rel, fileName, mime)
}

// serveOriginal opens the relative original path and streams it back with
// the Content-Disposition: attachment header. Range requests are handled by
// http.ServeContent.
func serveOriginal(
	w http.ResponseWriter, r *http.Request, store *storage.Storage,
	rel, fileName, mime string,
) {
	f, err := store.OpenOriginal(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondError(w, http.StatusNotFound, "original not found")
			return
		}
		log.Printf("open original %q: %v", sanitizeForLog(rel), err)
		respondError(w, http.StatusInternalServerError, "failed to open original")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		log.Printf("stat original %q: %v", sanitizeForLog(rel), err)
		respondError(w, http.StatusInternalServerError, "failed to stat original")
		return
	}

	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, fileName))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, fileName, stat.ModTime(), f)
}

// resolvePrimaryFile returns the relative path, filename, and MIME type of
// the primary file for a photo. It prefers the photo_files row marked
// is_primary; if none exists, it falls back to the photo row's own
// file_path/file_name/file_mime.
func resolvePrimaryFile(
	ctx context.Context, reader database.PhotoReader, photo *database.Photo,
) (string, string, string, error) {
	files, err := reader.ListPhotoFiles(ctx, photo.UID)
	if err != nil {
		return "", "", "", fmt.Errorf("list photo files: %w", err)
	}
	for i := range files {
		if files[i].IsPrimary {
			return files[i].FilePath, baseName(files[i].FilePath, photo.FileName), files[i].FileMime, nil
		}
	}
	if photo.FilePath != "" {
		return photo.FilePath, baseName(photo.FilePath, photo.FileName), photo.FileMime, nil
	}
	return "", "", "", nil
}

// baseName returns name when set; otherwise the last segment of path.
func baseName(path, name string) string {
	if name != "" {
		return name
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// BatchAddLabelsRequest represents a request to add labels to multiple photos.
type BatchAddLabelsRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
	Label     string   `json:"label"`
}

// BatchAddLabelsResponse represents the response from batch adding labels.
type BatchAddLabelsResponse struct {
	Updated int      `json:"updated"`
	Errors  []string `json:"errors,omitempty"`
}

// BatchAddLabels adds a label to multiple photos via the native
// LabelWriter. The single label name is upserted with EnsureLabel once; for
// each photo the resulting label_uid is attached via AddPhotoLabel
// (idempotent — re-adding the same pair is a silent no-op). Per-photo
// errors are reported but do not abort the batch, matching the bulk action
// bar's "do as much as possible" expectation.
func (h *PhotosHandler) BatchAddLabels(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req BatchAddLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}

	if req.Label == "" {
		respondError(w, http.StatusBadRequest, "label is required")
		return
	}

	if h.labels == nil {
		respondError(w, http.StatusServiceUnavailable, "label storage not available")
		return
	}

	ensured, err := h.labels.EnsureLabel(r.Context(), req.Label)
	if err != nil {
		log.Printf("photos batch labels ensure %q: %v", sanitizeForLog(req.Label), err)
		respondError(w, http.StatusInternalServerError, "failed to ensure label")
		return
	}

	var errors []string
	updated := 0
	for _, photoUID := range req.PhotoUIDs {
		if err := h.labels.AddPhotoLabel(r.Context(), photoUID, ensured.UID, "manual", 0); err != nil {
			errors = append(errors, photoUID+": "+err.Error())
			log.Printf("photos batch labels %s: %v", sanitizeForLog(photoUID), err)
			continue
		}
		updated++
	}

	respondJSON(w, http.StatusOK, BatchAddLabelsResponse{
		Updated: updated,
		Errors:  errors,
	})
}

// BatchArchiveRequest represents a request to archive multiple photos.
type BatchArchiveRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// BatchPhotoError captures a per-photo failure inside a batch response.
type BatchPhotoError struct {
	PhotoUID string `json:"photo_uid"`
	Error    string `json:"error"`
}

// BatchResponse is the envelope shared by archive / restore / edit batch
// operations. Per-photo failures are surfaced via Errors while still
// allowing the operation to make progress for the rest of the batch.
type BatchResponse struct {
	Updated int               `json:"updated"`
	Errors  []BatchPhotoError `json:"errors,omitempty"`
}

// BatchArchive archives (soft-deletes) multiple photos via the native
// PhotoWriter. Per-photo errors are reported but do not abort the batch.
func (h *PhotosHandler) BatchArchive(w http.ResponseWriter, r *http.Request) {
	h.runBatchUIDOp(w, r, "archive", func(ctx context.Context, writer database.PhotoWriter, uid string) error {
		return writer.ArchivePhoto(ctx, uid)
	})
}

// BatchRestore clears archived_at for multiple photos. Mirror of
// BatchArchive with identical response shape.
func (h *PhotosHandler) BatchRestore(w http.ResponseWriter, r *http.Request) {
	h.runBatchUIDOp(w, r, "restore", func(ctx context.Context, writer database.PhotoWriter, uid string) error {
		return writer.RestorePhoto(ctx, uid)
	})
}

// ListTrash returns the archived photos page (the "trash" view). It shares
// the photo filter / sort / pagination logic with List, but force-overrides
// the archived flag to true so callers cannot accidentally ask for live
// photos via this route. Any role can read the trash.
func (h *PhotosHandler) ListTrash(w http.ResponseWriter, r *http.Request) {
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	filter, ok := parsePhotoFilter(w, r)
	if !ok {
		return
	}
	yes := true
	filter.Archived = &yes

	photos, total, err := reader.ListPhotos(r.Context(), filter)
	if err != nil {
		log.Printf("photos trash list: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to get trash")
		return
	}

	response := PhotoListResponse{
		Photos: make([]PhotoResponse, 0, len(photos)),
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}
	if response.Limit == 0 {
		response.Limit = constants.DefaultHandlerPageSize
	}
	for i := range photos {
		response.Photos = append(response.Photos, nativePhotoToResponse(photos[i]))
	}
	respondJSON(w, http.StatusOK, response)
}

// BatchPurgeRequest is the JSON envelope for POST /photos/batch/purge. It
// mirrors BatchArchiveRequest so the frontend can pass the same selection
// state to either endpoint.
type BatchPurgeRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// BatchPurgeResponse is the envelope returned by BatchPurge. Per-photo
// failures are surfaced via Errors while still allowing the operation to
// make progress for the rest of the batch (matching the BatchArchive /
// BatchRestore contract).
type BatchPurgeResponse struct {
	Purged int               `json:"purged"`
	Errors []BatchPhotoError `json:"errors,omitempty"`
}

// BatchPurge hard-deletes the listed photos. Photos that are not currently
// archived are skipped with an entry in Errors (the row keeps existing).
// Admin role is enforced at the router via RequireRole("admin"); this
// handler still calls requireWriteRole as a belt-and-braces guard against
// future route reshuffles.
func (h *PhotosHandler) BatchPurge(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}
	var req BatchPurgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}
	if h.trashStore == nil {
		respondError(w, http.StatusServiceUnavailable, "trash store not available")
		return
	}

	var batchErrors []BatchPhotoError
	purged := 0
	for _, uid := range req.PhotoUIDs {
		if err := trash.PurgePhoto(r.Context(), uid, h.trashStore); err != nil {
			batchErrors = append(batchErrors, BatchPhotoError{PhotoUID: uid, Error: err.Error()})
			log.Printf("photos batch purge %s: %v", sanitizeForLog(uid), err)
			continue
		}
		purged++
	}
	respondJSON(w, http.StatusOK, BatchPurgeResponse{Purged: purged, Errors: batchErrors})
}

// runBatchUIDOp decodes the standard {photo_uids:[...]} payload, enforces
// auth + non-empty list, and dispatches op once per UID. Per-photo errors
// (including database.ErrNotFound) are collected into the response rather
// than aborting the batch.
func (h *PhotosHandler) runBatchUIDOp(
	w http.ResponseWriter, r *http.Request, label string,
	op func(ctx context.Context, writer database.PhotoWriter, uid string) error,
) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}
	var req BatchArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}
	writer := h.requirePhotoWriter(w)
	if writer == nil {
		return
	}

	var batchErrors []BatchPhotoError
	updated := 0
	for _, uid := range req.PhotoUIDs {
		if err := op(r.Context(), writer, uid); err != nil {
			batchErrors = append(batchErrors, BatchPhotoError{PhotoUID: uid, Error: err.Error()})
			log.Printf("photos batch %s %s: %v", label, sanitizeForLog(uid), err)
			continue
		}
		updated++
	}
	respondJSON(w, http.StatusOK, BatchResponse{Updated: updated, Errors: batchErrors})
}

// SimilarRequest represents a similar photos search request.
type SimilarRequest struct {
	PhotoUID  string  `json:"photo_uid"`
	Limit     int     `json:"limit,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// SimilarPhotoResult represents a single similar photo result.
type SimilarPhotoResult struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`   // Cosine distance (0-2, lower = more similar)
	Similarity float64 `json:"similarity"` // 1 - distance (for easier interpretation)
}

// SimilarResponse represents the full similar photos response.
type SimilarResponse struct {
	SourcePhotoUID string               `json:"source_photo_uid"`
	Threshold      float64              `json:"threshold"`
	Results        []SimilarPhotoResult `json:"results"`
	Count          int                  `json:"count"`
}

// CollectionSimilarRequest represents a request to find similar photos to a collection.
type CollectionSimilarRequest struct {
	SourceType string  `json:"source_type"` // "label" or "album"
	SourceID   string  `json:"source_id"`   // label name or album UID
	Limit      int     `json:"limit,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// CollectionSimilarResult represents a single similar photo result with match count.
type CollectionSimilarResult struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`    // Cosine distance (0-2, lower = more similar)
	Similarity float64 `json:"similarity"`  // 1 - distance (for easier interpretation)
	MatchCount int     `json:"match_count"` // Number of source photos that matched
}

// CollectionSimilarResponse represents the full collection similar photos response.
type CollectionSimilarResponse struct {
	SourceType           string                    `json:"source_type"`
	SourceID             string                    `json:"source_id"`
	SourcePhotoCount     int                       `json:"source_photo_count"`
	SourceEmbeddingCount int                       `json:"source_embedding_count"`
	MinMatchCount        int                       `json:"min_match_count"`
	Threshold            float64                   `json:"threshold"`
	Results              []CollectionSimilarResult `json:"results"`
	Count                int                       `json:"count"`
}

// parseCollectionSimilarRequest parses and validates the request, returning an error message if invalid.
func parseCollectionSimilarRequest(r *http.Request) (CollectionSimilarRequest, string) {
	var req CollectionSimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, errInvalidRequestBody
	}
	if req.SourceType == "" {
		return req, "source_type is required"
	}
	if req.SourceID == "" {
		return req, "source_id is required"
	}
	if req.SourceType != "label" && req.SourceType != albumTypeFilterDefault {
		return req, "source_type must be 'label' or 'album'"
	}
	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSimilarityThreshold
	}
	return req, ""
}

// fetchSourcePhotoUIDs fetches all photo UIDs matching a query from PhotoPrism.
func fetchSourcePhotoUIDs(pp *photoprism.PhotoPrism, query string) (map[string]bool, error) {
	sourcePhotoUIDs := make(map[string]bool)
	offset := 0
	pageSize := constants.DefaultHandlerPageSize

	for {
		photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
		if err != nil {
			return nil, fmt.Errorf("fetching photos with query: %w", err)
		}
		for _, photo := range photos {
			sourcePhotoUIDs[photo.UID] = true
		}
		if len(photos) < pageSize {
			break
		}
		offset += pageSize
	}
	return sourcePhotoUIDs, nil
}

// collectionMatchCandidate tracks similarity match data for a candidate photo.
type collectionMatchCandidate struct {
	PhotoUID   string
	Distance   float64
	MatchCount int
}

// searchCollectionSimilar searches for similar photos across all source embeddings.
//
//nolint:gocognit // Collection similarity search with deduplication.
func searchCollectionSimilar(
	ctx context.Context, embRepo database.EmbeddingReader,
	sourcePhotoUIDs map[string]bool, limit int, threshold float64,
) (map[string]*collectionMatchCandidate, int) {
	candidateMap := make(map[string]*collectionMatchCandidate)
	sourceEmbeddingCount := 0

	for photoUID := range sourcePhotoUIDs {
		emb, err := embRepo.Get(ctx, photoUID)
		if err != nil || emb == nil {
			continue
		}
		sourceEmbeddingCount++

		similar, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, limit*10, threshold)
		if err != nil {
			continue
		}

		for i, sim := range similar {
			if sourcePhotoUIDs[sim.PhotoUID] {
				continue
			}
			if existing, ok := candidateMap[sim.PhotoUID]; ok {
				existing.MatchCount++
				if distances[i] < existing.Distance {
					existing.Distance = distances[i]
				}
			} else {
				candidateMap[sim.PhotoUID] = &collectionMatchCandidate{
					PhotoUID: sim.PhotoUID, Distance: distances[i], MatchCount: 1,
				}
			}
		}
	}
	return candidateMap, sourceEmbeddingCount
}

// filterAndSortCollectionResults filters by min match count, sorts, and applies limit.
func filterAndSortCollectionResults(
	candidateMap map[string]*collectionMatchCandidate,
	minMatchCount, limit int,
) []CollectionSimilarResult {
	results := make([]CollectionSimilarResult, 0, len(candidateMap))
	for _, candidate := range candidateMap {
		if candidate.MatchCount >= minMatchCount {
			results = append(results, CollectionSimilarResult{
				PhotoUID: candidate.PhotoUID, Distance: candidate.Distance,
				Similarity: 1 - candidate.Distance, MatchCount: candidate.MatchCount,
			})
		}
	}

	for i := range len(results) - 1 {
		for j := i + 1; j < len(results); j++ {
			if results[j].MatchCount > results[i].MatchCount ||
				(results[j].MatchCount == results[i].MatchCount && results[j].Distance < results[i].Distance) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// FindSimilarToCollection finds photos similar to all photos in a label or album.
func (h *PhotosHandler) FindSimilarToCollection(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseCollectionSimilarRequest(r)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}

	ctx := context.Background()
	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	var query string
	if req.SourceType == "label" {
		query = "label:" + req.SourceID
	} else {
		query = "album:" + req.SourceID
	}

	sourcePhotoUIDs, err := fetchSourcePhotoUIDs(pp, query)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photos: "+err.Error())
		return
	}

	if len(sourcePhotoUIDs) == 0 {
		respondJSON(w, http.StatusOK, CollectionSimilarResponse{
			SourceType: req.SourceType, SourceID: req.SourceID,
			Threshold: req.Threshold, Results: []CollectionSimilarResult{},
		})
		return
	}

	candidateMap, sourceEmbeddingCount := searchCollectionSimilar(ctx, embRepo, sourcePhotoUIDs, req.Limit, req.Threshold)
	minMatchCount := computeMinMatchCount(sourceEmbeddingCount, req.Threshold)
	results := filterAndSortCollectionResults(candidateMap, minMatchCount, req.Limit)

	respondJSON(w, http.StatusOK, CollectionSimilarResponse{
		SourceType:           req.SourceType,
		SourceID:             req.SourceID,
		SourcePhotoCount:     len(sourcePhotoUIDs),
		SourceEmbeddingCount: sourceEmbeddingCount,
		MinMatchCount:        minMatchCount,
		Threshold:            req.Threshold,
		Results:              results,
		Count:                len(results),
	})
}

// parseSimilarRequest parses and validates the similar photos request.
func parseSimilarRequest(r *http.Request) (SimilarRequest, string) {
	var req SimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, errInvalidRequestBody
	}
	if req.PhotoUID == "" {
		return req, "photo_uid is required"
	}
	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSimilarityThreshold
	}
	return req, ""
}

// FindSimilar finds similar photos based on image embeddings.
func (h *PhotosHandler) FindSimilar(w http.ResponseWriter, r *http.Request) {
	req, errMsg := parseSimilarRequest(r)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}

	ctx := context.Background()
	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	sourceEmb, err := embRepo.Get(ctx, req.PhotoUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get embedding")
		return
	}
	if sourceEmb == nil {
		respondError(w, http.StatusNotFound, "no embedding found for this photo. Run 'photo info --embedding' first")
		return
	}

	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, sourceEmb.Embedding, req.Limit+1, req.Threshold)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to find similar photos")
		return
	}

	var results []SimilarPhotoResult
	for i, emb := range similar {
		if emb.PhotoUID == req.PhotoUID {
			continue
		}
		results = append(results, SimilarPhotoResult{
			PhotoUID: emb.PhotoUID, Distance: distances[i], Similarity: 1 - distances[i],
		})
	}

	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	respondJSON(w, http.StatusOK, SimilarResponse{
		SourcePhotoUID: req.PhotoUID, Threshold: req.Threshold,
		Results: results, Count: len(results),
	})
}

// TextSearchRequest represents a text-to-image search request.
type TextSearchRequest struct {
	Text      string  `json:"text"`
	Limit     int     `json:"limit,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// TextSearchResponse represents the text search results.
type TextSearchResponse struct {
	Query            string               `json:"query"`
	TranslatedQuery  string               `json:"translated_query,omitempty"`
	TranslateCostUSD float64              `json:"translate_cost_usd,omitempty"`
	TranslateError   string               `json:"translate_error,omitempty"`
	Threshold        float64              `json:"threshold"`
	Results          []SimilarPhotoResult `json:"results"`
	Count            int                  `json:"count"`
}

// translateQueryResult holds the result of query translation.
type translateQueryResult struct {
	queryText       string
	translatedQuery string
	translateCost   float64
	translateError  string
}

// translateQueryForCLIP optionally translates the query text to CLIP-optimized English.
func translateQueryForCLIP(ctx context.Context, openAIToken, text string) translateQueryResult {
	result := translateQueryResult{queryText: text}
	if openAIToken == "" {
		return result
	}
	translated, err := ai.TranslateForCLIP(ctx, openAIToken, text)
	if err != nil {
		log.Printf("WARNING: CLIP translation failed: %v", err)
		result.translateError = err.Error()
		return result
	}
	if translated.Text != text {
		result.queryText = translated.Text
		result.translatedQuery = translated.Text
		result.translateCost = translated.Cost
	}
	return result
}

// SearchByText finds photos matching a text query using CLIP text embeddings.
func (h *PhotosHandler) SearchByText(w http.ResponseWriter, r *http.Request) {
	var req TextSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		respondError(w, http.StatusBadRequest, "text is required")
		return
	}

	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.5
	}

	ctx := context.Background()
	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	tr := translateQueryForCLIP(ctx, h.config.OpenAI.Token, req.Text)

	embClient, err := fingerprint.NewEmbeddingClient(h.config.Embedding.URL, "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "invalid embedding config: "+err.Error())
		return
	}
	textEmbedding, err := embClient.ComputeTextEmbedding(ctx, tr.queryText)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to compute text embedding: "+err.Error())
		return
	}

	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, textEmbedding, req.Limit, req.Threshold)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search photos")
		return
	}

	results := make([]SimilarPhotoResult, 0, len(similar))
	for i, emb := range similar {
		results = append(results, SimilarPhotoResult{
			PhotoUID: emb.PhotoUID, Distance: distances[i], Similarity: 1 - distances[i],
		})
	}

	respondJSON(w, http.StatusOK, TextSearchResponse{
		Query: req.Text, TranslatedQuery: tr.translatedQuery,
		TranslateCostUSD: tr.translateCost, TranslateError: tr.translateError,
		Threshold: req.Threshold,
		Results:   results, Count: len(results),
	})
}

// EraMatch represents a single era match result.
type EraMatch struct {
	EraSlug            string  `json:"era_slug"`
	EraName            string  `json:"era_name"`
	RepresentativeDate string  `json:"representative_date"`
	Similarity         float64 `json:"similarity"` // 0-1 (cosine similarity)
	Confidence         float64 `json:"confidence"` // 0-100 percentage
}

// EraEstimateResponse represents the era estimation result for a photo.
type EraEstimateResponse struct {
	PhotoUID   string     `json:"photo_uid"`
	BestMatch  *EraMatch  `json:"best_match"`
	TopMatches []EraMatch `json:"top_matches"`
}

func computeEraMatches(photoEmb []float32, eras []database.StoredEraEmbedding) []EraMatch {
	matches := make([]EraMatch, 0, len(eras))
	for i := range eras {
		era := &eras[i]
		sim := fingerprint.CosineSimilarity(photoEmb, era.Embedding)
		confidence := math.Max(0, math.Min(100, sim*100))
		matches = append(matches, EraMatch{
			EraSlug:            era.EraSlug,
			EraName:            era.EraName,
			RepresentativeDate: era.RepresentativeDate,
			Similarity:         sim,
			Confidence:         confidence,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})
	return matches
}

// EstimateEra estimates the era of a photo by comparing its CLIP image embedding.
// against pre-computed era text embedding centroids.
func (h *PhotosHandler) EstimateEra(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}

	ctx := context.Background()

	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	// Get the photo's image embedding.
	photoEmb, err := embRepo.Get(ctx, uid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get embedding")
		return
	}
	if photoEmb == nil {
		respondError(w, http.StatusNotFound, "no embedding found for this photo")
		return
	}

	// Get all era centroids.
	eraReader, err := database.GetEraEmbeddingReader(ctx)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "era embeddings not available")
		return
	}

	eras, err := eraReader.GetAllEras(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get era embeddings")
		return
	}

	if len(eras) == 0 {
		respondJSON(w, http.StatusOK, EraEstimateResponse{
			PhotoUID:   uid,
			BestMatch:  nil,
			TopMatches: []EraMatch{},
		})
		return
	}

	matches := computeEraMatches(photoEmb.Embedding, eras)

	respondJSON(w, http.StatusOK, EraEstimateResponse{
		PhotoUID:   uid,
		BestMatch:  &matches[0],
		TopMatches: matches,
	})
}

// BatchEditRequest represents a request to edit multiple photos.
type BatchEditRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
	Favorite  *bool    `json:"favorite,omitempty"`
	Private   *bool    `json:"private,omitempty"`
}

// BatchEdit toggles favorite / private on multiple photos via the native
// PhotoWriter. Per-photo errors are reported but do not abort the batch.
func (h *PhotosHandler) BatchEdit(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}
	var req BatchEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}
	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}
	if req.Favorite == nil && req.Private == nil {
		respondError(w, http.StatusBadRequest, "at least one field (favorite, private) is required")
		return
	}
	writer := h.requirePhotoWriter(w)
	if writer == nil {
		return
	}

	var batchErrors []BatchPhotoError
	updated := 0
	for _, uid := range req.PhotoUIDs {
		if err := h.applyBatchEdit(r.Context(), writer, uid, req.Favorite, req.Private); err != nil {
			batchErrors = append(batchErrors, BatchPhotoError{PhotoUID: uid, Error: err.Error()})
			log.Printf("photos batch edit %s: %v", sanitizeForLog(uid), err)
			continue
		}
		updated++
	}
	respondJSON(w, http.StatusOK, BatchResponse{Updated: updated, Errors: batchErrors})
}

// applyBatchEdit applies favorite/private flips for one photo and writes
// it back. Loads the row first so the writer never clears unrelated fields.
func (h *PhotosHandler) applyBatchEdit(
	ctx context.Context, writer database.PhotoWriter, uid string, favorite, private *bool,
) error {
	photo, err := writer.GetPhoto(ctx, uid)
	if err != nil {
		return fmt.Errorf("get photo: %w", err)
	}
	if favorite != nil {
		photo.Favorite = *favorite
	}
	if private != nil {
		photo.Private = *private
	}
	if err := writer.UpdatePhoto(ctx, photo); err != nil {
		return fmt.Errorf("update photo: %w", err)
	}
	return nil
}

// DuplicatesRequest represents a request to find duplicate photos.
type DuplicatesRequest struct {
	AlbumUID  string  `json:"album_uid,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance
	Limit     int     `json:"limit,omitempty"`     // Max number of groups to return
}

// DuplicatePhoto represents a single photo in a duplicate group.
type DuplicatePhoto struct {
	PhotoUID string  `json:"photo_uid"`
	Distance float64 `json:"distance"` // Distance from group representative
}

// DuplicateGroup represents a group of similar/duplicate photos.
type DuplicateGroup struct {
	Photos      []DuplicatePhoto `json:"photos"`
	AvgDistance float64          `json:"avg_distance"`
	PhotoCount  int              `json:"photo_count"`
}

// DuplicatesResponse represents the full duplicates response.
type DuplicatesResponse struct {
	TotalPhotosScanned int              `json:"total_photos_scanned"`
	DuplicateGroups    []DuplicateGroup `json:"duplicate_groups"`
	TotalGroups        int              `json:"total_groups"`
	TotalDuplicates    int              `json:"total_duplicates"`
}

// duplicateUnionFind implements union-find for grouping duplicate photos.
type duplicateUnionFind struct {
	parent        map[string]string
	rank          map[string]int
	pairDistances map[string][]float64
}

func newDuplicateUnionFind(photoUIDs []string) *duplicateUnionFind {
	uf := &duplicateUnionFind{
		parent:        make(map[string]string, len(photoUIDs)),
		rank:          make(map[string]int, len(photoUIDs)),
		pairDistances: make(map[string][]float64),
	}
	for _, uid := range photoUIDs {
		uf.parent[uid] = uid
	}
	return uf
}

func (uf *duplicateUnionFind) find(x string) string {
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *duplicateUnionFind) union(x, y string, distance float64) {
	px, py := uf.find(x), uf.find(y)
	if px == py {
		return
	}
	if uf.rank[px] < uf.rank[py] {
		px, py = py, px
	}
	uf.parent[py] = px
	if uf.rank[px] == uf.rank[py] {
		uf.rank[px]++
	}
	uf.pairDistances[px] = append(uf.pairDistances[px], distance)
}

// fetchDuplicatePhotoUIDs retrieves photo UIDs to scan, either from an album or from all embeddings.
func fetchDuplicatePhotoUIDs(
	ctx context.Context, r *http.Request, w http.ResponseWriter,
	albumUID string, embRepo database.EmbeddingReader,
) ([]string, bool) {
	if albumUID != "" {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return nil, false
		}
		var photoUIDs []string
		offset := 0
		pageSize := constants.DefaultHandlerPageSize
		for {
			photos, err := pp.GetAlbumPhotos(albumUID, pageSize, offset)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to get album photos")
				return nil, false
			}
			for _, p := range photos {
				photoUIDs = append(photoUIDs, p.UID)
			}
			if len(photos) < pageSize {
				break
			}
			offset += pageSize
		}
		return photoUIDs, true
	}
	photoUIDs, err := embRepo.GetUniquePhotoUIDs(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photo UIDs")
		return nil, false
	}
	return photoUIDs, true
}

// populateUnionFind searches for neighbors for each photo and unions them into the UF structure.
func populateUnionFind(
	ctx context.Context, embRepo database.EmbeddingReader,
	photoUIDs []string, uidSet map[string]bool,
	threshold float64, uf *duplicateUnionFind,
) {
	for _, uid := range photoUIDs {
		emb, err := embRepo.Get(ctx, uid)
		if err != nil || emb == nil {
			continue
		}
		neighbors, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, 20, threshold)
		if err != nil {
			continue
		}
		for i, neighbor := range neighbors {
			if neighbor.PhotoUID != uid && uidSet[neighbor.PhotoUID] {
				uf.union(uid, neighbor.PhotoUID, distances[i])
			}
		}
	}
}

// extractDuplicateGroups extracts groups of size >= 2 from the union-find structure.
func extractDuplicateGroups(uf *duplicateUnionFind, photoUIDs []string) ([]DuplicateGroup, int) {
	groups := make(map[string][]string)
	for _, uid := range photoUIDs {
		root := uf.find(uid)
		groups[root] = append(groups[root], uid)
	}

	var resultGroups []DuplicateGroup
	totalDuplicates := 0

	for root, members := range groups {
		if len(members) < 2 {
			continue
		}
		avgDist := averageDistance(uf.pairDistances[root])
		photos := make([]DuplicatePhoto, len(members))
		for i, uid := range members {
			photos[i] = DuplicatePhoto{PhotoUID: uid, Distance: avgDist}
		}
		resultGroups = append(resultGroups, DuplicateGroup{
			Photos: photos, AvgDistance: avgDist, PhotoCount: len(members),
		})
		totalDuplicates += len(members)
	}

	sort.Slice(resultGroups, func(i, j int) bool {
		if resultGroups[i].PhotoCount != resultGroups[j].PhotoCount {
			return resultGroups[i].PhotoCount > resultGroups[j].PhotoCount
		}
		return resultGroups[i].AvgDistance < resultGroups[j].AvgDistance
	})

	return resultGroups, totalDuplicates
}

// buildDuplicateGroups clusters photos into duplicate groups using union-find.
func buildDuplicateGroups(
	ctx context.Context, embRepo database.EmbeddingReader,
	photoUIDs []string, threshold float64,
) ([]DuplicateGroup, int) {
	uidSet := make(map[string]bool, len(photoUIDs))
	for _, uid := range photoUIDs {
		uidSet[uid] = true
	}

	uf := newDuplicateUnionFind(photoUIDs)
	populateUnionFind(ctx, embRepo, photoUIDs, uidSet, threshold, uf)
	return extractDuplicateGroups(uf, photoUIDs)
}

// averageDistance computes the mean of a float64 slice.
func averageDistance(distances []float64) float64 {
	if len(distances) == 0 {
		return 0
	}
	sum := 0.0
	for _, d := range distances {
		sum += d
	}
	return sum / float64(len(distances))
}

// FindDuplicates finds near-duplicate photos using CLIP embedding similarity.
func (h *PhotosHandler) FindDuplicates(w http.ResponseWriter, r *http.Request) {
	var req DuplicatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultDuplicateThreshold
	}
	if req.Limit <= 0 {
		req.Limit = constants.DefaultDuplicateLimit
	}

	ctx := context.Background()
	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	photoUIDs, ok := fetchDuplicatePhotoUIDs(ctx, r, w, req.AlbumUID, embRepo)
	if !ok {
		return
	}

	if len(photoUIDs) == 0 {
		respondJSON(w, http.StatusOK, DuplicatesResponse{
			DuplicateGroups: []DuplicateGroup{},
		})
		return
	}

	resultGroups, totalDuplicates := buildDuplicateGroups(ctx, embRepo, photoUIDs, req.Threshold)

	if len(resultGroups) > req.Limit {
		resultGroups = resultGroups[:req.Limit]
	}

	respondJSON(w, http.StatusOK, DuplicatesResponse{
		TotalPhotosScanned: len(photoUIDs),
		DuplicateGroups:    resultGroups,
		TotalGroups:        len(resultGroups),
		TotalDuplicates:    totalDuplicates,
	})
}

// SuggestAlbumsRequest represents a request to find photos missing from existing albums.
type SuggestAlbumsRequest struct {
	Threshold float64 `json:"threshold,omitempty"` // Min cosine similarity (0-1)
	TopK      int     `json:"top_k,omitempty"`     // Max photos suggested per album
}

// AlbumPhotoSuggestion represents a single photo's suggestion for an album.
type AlbumPhotoSuggestion struct {
	PhotoUID   string  `json:"photo_uid"`
	Similarity float64 `json:"similarity"`
}

// AlbumSuggestion represents a suggested album with matching photos.
type AlbumSuggestion struct {
	AlbumUID   string                 `json:"album_uid"`
	AlbumTitle string                 `json:"album_title"`
	Photos     []AlbumPhotoSuggestion `json:"photos"`
}

// SuggestAlbumsResponse represents the full album suggestion response.
type SuggestAlbumsResponse struct {
	AlbumsAnalyzed int               `json:"albums_analyzed"`
	PhotosAnalyzed int               `json:"photos_analyzed"`
	Skipped        int               `json:"skipped"` // Albums skipped (no embeddings)
	Suggestions    []AlbumSuggestion `json:"suggestions"`
}

// suggestAlbumResult holds the result of processing a single album for suggestions.
type suggestAlbumResult struct {
	suggestion *AlbumSuggestion
	skipped    bool
}

// fetchAlbumMemberSet fetches all photo UIDs in an album via paginated API calls.
func fetchAlbumMemberSet(pp *photoprism.PhotoPrism, albumUID string) (map[string]bool, error) {
	albumMemberSet := make(map[string]bool)
	offset := 0
	pageSize := constants.DefaultHandlerPageSize
	for {
		photos, err := pp.GetAlbumPhotos(albumUID, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("fetching album photos: %w", err)
		}
		for _, p := range photos {
			albumMemberSet[p.UID] = true
		}
		if len(photos) < pageSize {
			break
		}
		offset += pageSize
	}
	return albumMemberSet, nil
}

// memberUIDsSlice converts the album member set into a UID slice — pgvector's
// AVG aggregate takes the list in a single SQL round trip.
func memberUIDsSlice(memberSet map[string]bool) []string {
	uids := make([]string, 0, len(memberSet))
	for uid := range memberSet {
		uids = append(uids, uid)
	}
	return uids
}

// filterSuggestedPhotos filters out album members and returns non-member photos as suggestions.
func filterSuggestedPhotos(
	similar []database.StoredEmbedding, distances []float64,
	albumMemberSet map[string]bool, topK int,
) []AlbumPhotoSuggestion {
	var photos []AlbumPhotoSuggestion
	for i, emb := range similar {
		if albumMemberSet[emb.PhotoUID] {
			continue
		}
		photos = append(photos, AlbumPhotoSuggestion{
			PhotoUID: emb.PhotoUID, Similarity: 1.0 - distances[i],
		})
		if len(photos) >= topK {
			break
		}
	}
	return photos
}

// processAlbumForSuggestions analyzes a single album and returns suggestions for missing photos.
// Centroid + similarity are computed in two SQL round trips: one AVG()
// across the album's embeddings, one cosine-distance ORDER BY against the
// pgvector HNSW index.
func processAlbumForSuggestions(
	ctx context.Context, pp *photoprism.PhotoPrism,
	embRepo database.EmbeddingReader, album photoprism.Album,
	topK int, maxDistance float64,
) suggestAlbumResult {
	albumMemberSet, err := fetchAlbumMemberSet(pp, album.UID)
	if err != nil || len(albumMemberSet) < constants.MinAlbumPhotosForCentroid {
		return suggestAlbumResult{}
	}

	centroid, err := embRepo.GetCentroid(ctx, memberUIDsSlice(albumMemberSet))
	if err != nil || len(centroid) == 0 {
		return suggestAlbumResult{skipped: true}
	}

	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, centroid, topK*3, maxDistance)
	if err != nil {
		return suggestAlbumResult{}
	}

	photos := filterSuggestedPhotos(similar, distances, albumMemberSet, topK)
	if len(photos) == 0 {
		return suggestAlbumResult{}
	}

	return suggestAlbumResult{suggestion: &AlbumSuggestion{
		AlbumUID: album.UID, AlbumTitle: album.Title, Photos: photos,
	}}
}

// processAlbumsInParallel processes candidate albums concurrently and returns suggestions and skip count.
func processAlbumsInParallel(
	ctx context.Context, pp *photoprism.PhotoPrism,
	embRepo database.EmbeddingReader, candidateAlbums []photoprism.Album,
	topK int, maxDistance float64,
) ([]AlbumSuggestion, int) {
	var mu sync.Mutex
	var suggestions []AlbumSuggestion
	skipped := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i := range candidateAlbums {
		wg.Add(1)
		go func(a photoprism.Album) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := processAlbumForSuggestions(ctx, pp, embRepo, a, topK, maxDistance)
			mu.Lock()
			if result.skipped {
				skipped++
			}
			if result.suggestion != nil {
				suggestions = append(suggestions, *result.suggestion)
			}
			mu.Unlock()
		}(candidateAlbums[i])
	}
	wg.Wait()

	sort.Slice(suggestions, func(i, j int) bool {
		return len(suggestions[i].Photos) > len(suggestions[j].Photos)
	})
	return suggestions, skipped
}

// countUniqueSuggestedPhotos counts unique photos across all suggestions.
func countUniqueSuggestedPhotos(suggestions []AlbumSuggestion) int {
	uniquePhotos := make(map[string]bool)
	for _, s := range suggestions {
		for _, p := range s.Photos {
			uniquePhotos[p.PhotoUID] = true
		}
	}
	return len(uniquePhotos)
}

// SuggestAlbums finds photos that belong in existing albums but aren't there yet (album completion).
func (h *PhotosHandler) SuggestAlbums(w http.ResponseWriter, r *http.Request) {
	var req SuggestAlbumsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSuggestAlbumThreshold
	}
	if req.TopK <= 0 {
		req.TopK = constants.DefaultSuggestAlbumTopK
	}

	ctx := context.Background()
	embRepo, ok := h.getEmbeddingReader(w)
	if !ok {
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	albums, err := pp.GetAlbums(constants.MaxPhotosPerFetch, 0, "", "", "album")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	var candidateAlbums []photoprism.Album
	for i := range albums {
		if albums[i].PhotoCount >= constants.MinAlbumPhotosForCentroid {
			candidateAlbums = append(candidateAlbums, albums[i])
		}
	}

	suggestions, skipped := processAlbumsInParallel(ctx, pp, embRepo, candidateAlbums, req.TopK, 1.0-req.Threshold)

	respondJSON(w, http.StatusOK, SuggestAlbumsResponse{
		AlbumsAnalyzed: len(candidateAlbums) - skipped,
		PhotosAnalyzed: countUniqueSuggestedPhotos(suggestions),
		Skipped:        skipped,
		Suggestions:    suggestions,
	})
}
