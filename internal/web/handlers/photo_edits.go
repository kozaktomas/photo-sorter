package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/imgedit"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/thumb"
)

// minCropPixels is the minimum size (in either dimension) the cropped
// output must have. Below this threshold the edit is rejected with 400.
const minCropPixels = 100

// photoEditsResponse is the wire shape returned by GET /photos/{uid}/edits
// and after a successful PUT. `Edits` is nil when no edits are stored.
type photoEditsResponse struct {
	Edits *photoEditsPayload `json:"edits"`
}

// photoEditsPayload is the JSON shape of a single edit row.
type photoEditsPayload struct {
	Crop       *photoEditsCropPayload `json:"crop"`
	Rotation   int                    `json:"rotation"`
	Brightness float64                `json:"brightness"`
	Contrast   float64                `json:"contrast"`
	UpdatedAt  string                 `json:"updated_at,omitempty"`
}

// photoEditsCropPayload is the JSON shape of the optional crop sub-object.
type photoEditsCropPayload struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// photoEditsRequest is the PUT body for /photos/{uid}/edits. All fields are
// required and ranges are validated by the handler.
type photoEditsRequest struct {
	Crop       *photoEditsCropPayload `json:"crop"`
	Rotation   int                    `json:"rotation"`
	Brightness float64                `json:"brightness"`
	Contrast   float64                `json:"contrast"`
}

// GetEdits returns the stored edit parameters for a photo. The envelope is
// `{ "edits": <payload> }` where payload is null when the photo has no
// stored edits, matching the spec's "no edits applied" sentinel.
func (h *PhotosHandler) GetEdits(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	reader := h.requirePhotoReader(w)
	if reader == nil {
		return
	}
	if _, ok := loadPhoto(w, r, reader, uid, "edits get"); !ok {
		return
	}

	editsRepo, err := database.GetPhotoEditsReader(r.Context())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "edits store not available")
		return
	}
	edits, err := editsRepo.GetPhotoEdits(r.Context(), uid)
	if errors.Is(err, database.ErrNotFound) {
		respondJSON(w, http.StatusOK, photoEditsResponse{Edits: nil})
		return
	}
	if err != nil {
		log.Printf("photo edits get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get edits")
		return
	}
	respondJSON(w, http.StatusOK, photoEditsResponse{Edits: editsToPayload(edits)})
}

// PutEdits upserts the edits row for a photo and synchronously
// invalidates + regenerates every cached thumbnail.
//
//nolint:cyclop,funlen // Orchestration handler chains validation, render, and thumb invalidation.
func (h *PhotosHandler) PutEdits(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}

	req, msg := parsePhotoEditsRequest(r)
	if msg != "" {
		respondError(w, http.StatusBadRequest, msg)
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
	photo, ok := loadPhoto(w, r, reader, uid, "edits put")
	if !ok {
		return
	}

	if photo.ArchivedAt != nil {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	editsRepo, err := database.GetPhotoEditsWriter(r.Context())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "edits store not available")
		return
	}

	edits := requestToDomain(uid, req)

	if cropErrMsg := validateCropPixels(photo, edits.Crop); cropErrMsg != "" {
		respondError(w, http.StatusBadRequest, cropErrMsg)
		return
	}

	if saveErr := editsRepo.SavePhotoEdits(r.Context(), edits); saveErr != nil {
		log.Printf("photo edits save %s: %v", sanitizeForLog(uid), saveErr)
		respondError(w, http.StatusInternalServerError, "failed to save edits")
		return
	}

	if regenErr := h.regenerateEditedThumbs(r.Context(), photo, edits); regenErr != nil {
		if errors.Is(regenErr, imgedit.ErrUnsupportedFormatNoDecoder) {
			// Roll back the save so the photo isn't left with edit
			// parameters the renderer can't honour.
			_ = editsRepo.DeletePhotoEdits(r.Context(), uid)
			respondError(w, http.StatusServiceUnavailable,
				"Editing this format requires heif-convert/dcraw on the server")
			return
		}
		log.Printf("photo edits regenerate thumbs %s: %v", sanitizeForLog(uid), regenErr)
		respondError(w, http.StatusInternalServerError, "failed to regenerate thumbnails")
		return
	}

	// Re-read so the response carries the database-set updated_at.
	fresh, err := editsRepo.GetPhotoEdits(r.Context(), uid)
	if err != nil {
		log.Printf("photo edits re-read %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to load edits")
		return
	}
	respondJSON(w, http.StatusOK, photoEditsResponse{Edits: editsToPayload(fresh)})
}

// DeleteEdits clears the edit row for a photo (revert-to-original) and
// regenerates the thumbnail cache from the unedited original. Returns 204
// when no row existed (idempotent revert).
func (h *PhotosHandler) DeleteEdits(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
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
	photo, ok := loadPhoto(w, r, reader, uid, "edits delete")
	if !ok {
		return
	}
	editsRepo, err := database.GetPhotoEditsWriter(r.Context())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "edits store not available")
		return
	}
	if !h.deleteEditsRow(w, r, editsRepo, uid) {
		return
	}
	h.regenerateOriginalThumbs(store, photo, uid)
	w.WriteHeader(http.StatusNoContent)
}

// deleteEditsRow looks up the existing edits row and deletes it. Returns
// false when the response has already been written (idempotent 204 or
// 500). The caller continues with thumbnail regeneration only when the
// returned bool is true.
func (h *PhotosHandler) deleteEditsRow(
	w http.ResponseWriter, r *http.Request,
	editsRepo database.PhotoEditsWriter, uid string,
) bool {
	existing, getErr := editsRepo.GetPhotoEdits(r.Context(), uid)
	if getErr != nil && !errors.Is(getErr, database.ErrNotFound) {
		log.Printf("photo edits delete get %s: %v", sanitizeForLog(uid), getErr)
		respondError(w, http.StatusInternalServerError, "failed to inspect edits")
		return false
	}
	if existing == nil {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	if delErr := editsRepo.DeletePhotoEdits(r.Context(), uid); delErr != nil {
		log.Printf("photo edits delete %s: %v", sanitizeForLog(uid), delErr)
		respondError(w, http.StatusInternalServerError, "failed to delete edits")
		return false
	}
	return true
}

// regenerateOriginalThumbs invalidates and rebuilds every registered
// thumbnail size from the un-edited original. Errors are logged but not
// surfaced — the photo row is already pointing at the original so the
// download path remains correct even if a thumb fails to write.
func (h *PhotosHandler) regenerateOriginalThumbs(
	store *storage.Storage, photo *database.Photo, uid string,
) {
	invalidateThumbs(store, photo.FileHash)
	if photo.FilePath == "" || photo.FileHash == "" {
		return
	}
	abs, absErr := store.AbsOriginal(photo.FilePath)
	if absErr != nil {
		log.Printf("photo edits delete abs %s: %v", sanitizeForLog(uid), absErr)
		return
	}
	if _, regenErr := thumb.GenerateAll(thumb.Source{
		Path:        abs,
		Orientation: photo.FileOrientation,
	}, store, photo.FileHash); regenErr != nil {
		log.Printf("photo edits delete regen %s: %v", sanitizeForLog(uid), regenErr)
	}
}

// parsePhotoEditsRequest decodes the PUT body and runs range validation.
//
//nolint:cyclop // Many independent range checks at the API boundary.
func parsePhotoEditsRequest(r *http.Request) (photoEditsRequest, string) {
	var req photoEditsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, errInvalidRequestBody
	}
	switch req.Rotation {
	case 0, 90, 180, 270:
	default:
		return req, "rotation must be 0, 90, 180, or 270"
	}
	if req.Brightness < -1 || req.Brightness > 1 {
		return req, "brightness must be between -1.0 and 1.0"
	}
	if req.Contrast < -1 || req.Contrast > 1 {
		return req, "contrast must be between -1.0 and 1.0"
	}
	if req.Crop != nil {
		c := req.Crop
		if c.X < 0 || c.X > 1 || c.Y < 0 || c.Y > 1 ||
			c.W <= 0 || c.W > 1 || c.H <= 0 || c.H > 1 {
			return req, "crop coordinates must be between 0.0 and 1.0 with positive width/height"
		}
		if c.X+c.W > 1.0001 || c.Y+c.H > 1.0001 {
			return req, "crop rectangle extends past image bounds"
		}
	}
	return req, ""
}

// validateCropPixels enforces the spec's "cropped output must be at least
// 100×100 px" rule. Returns "" when no crop is set or the crop is large
// enough; otherwise a user-facing error message.
func validateCropPixels(photo *database.Photo, crop *database.PhotoEditsCrop) string {
	if crop == nil {
		return ""
	}
	// Use rotated/display dimensions: EXIF orientations 5-8 swap them.
	w, h := photo.FileWidth, photo.FileHeight
	if photo.FileOrientation >= 5 && photo.FileOrientation <= 8 {
		w, h = h, w
	}
	if w == 0 || h == 0 {
		return ""
	}
	cw := int(crop.W * float64(w))
	ch := int(crop.H * float64(h))
	if cw < minCropPixels || ch < minCropPixels {
		return fmt.Sprintf(
			"cropped output is too small (%dx%d px); minimum is %dx%d px",
			cw, ch, minCropPixels, minCropPixels)
	}
	return ""
}

// requestToDomain converts the JSON wire shape to the database domain
// struct.
func requestToDomain(uid string, req photoEditsRequest) *database.PhotoEdits {
	edits := &database.PhotoEdits{
		PhotoUID:   uid,
		Rotation:   req.Rotation,
		Brightness: req.Brightness,
		Contrast:   req.Contrast,
	}
	if req.Crop != nil {
		edits.Crop = &database.PhotoEditsCrop{
			X: req.Crop.X, Y: req.Crop.Y, W: req.Crop.W, H: req.Crop.H,
		}
	}
	return edits
}

// editsToPayload converts the domain struct to the JSON wire shape.
func editsToPayload(e *database.PhotoEdits) *photoEditsPayload {
	if e == nil {
		return nil
	}
	out := &photoEditsPayload{
		Rotation:   e.Rotation,
		Brightness: e.Brightness,
		Contrast:   e.Contrast,
	}
	if !e.UpdatedAt.IsZero() {
		out.UpdatedAt = e.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if e.Crop != nil {
		out.Crop = &photoEditsCropPayload{
			X: e.Crop.X, Y: e.Crop.Y, W: e.Crop.W, H: e.Crop.H,
		}
	}
	return out
}

// regenerateEditedThumbs invalidates the cached thumbnails and rewrites
// every registered size from the post-edit pixel data.
func (h *PhotosHandler) regenerateEditedThumbs(
	ctx context.Context, photo *database.Photo, edits *database.PhotoEdits,
) error {
	if h.store == nil || photo.FilePath == "" || photo.FileHash == "" {
		return nil
	}
	abs, err := h.store.AbsOriginal(photo.FilePath)
	if err != nil {
		return fmt.Errorf("resolve original: %w", err)
	}
	img, err := imgedit.DecodeAndApply(ctx, abs, photo.FileOrientation, domainToImgedit(edits))
	if err != nil {
		// Preserve the sentinel so callers can errors.Is against
		// imgedit.ErrUnsupportedFormatNoDecoder for a 503 response.
		return err //nolint:wrapcheck
	}
	invalidateThumbs(h.store, photo.FileHash)
	_, err = thumb.GenerateSizesFromImage(img, thumb.SizeNames(), h.store, photo.FileHash)
	if err != nil {
		return fmt.Errorf("regenerate thumbs: %w", err)
	}
	return nil
}

// invalidateThumbs deletes every cached thumb size for a hash. Errors are
// logged but not returned — a missing file is fine, and a regen pass will
// overwrite stale bytes anyway.
func invalidateThumbs(store *storage.Storage, fileHash string) {
	for _, size := range thumb.SizeNames() {
		rel, err := storage.ThumbRelPath(fileHash, size)
		if err != nil {
			continue
		}
		if delErr := store.DeleteThumb(rel); delErr != nil {
			log.Printf("invalidate thumb %s: %v", sanitizeForLog(rel), delErr)
		}
	}
}

// tryServeEditedDownload checks whether the photo has stored edits and,
// if so, writes a freshly-rendered JPEG to w. Returns true when the
// handler wrote the response so the caller skips the original-file
// fallback. Errors (decode/encode/converter-missing) cause the function
// to log and return false so the original is served — the alternative is
// to surface an opaque 500 for what the user perceives as "just
// download the photo".
func (h *PhotosHandler) tryServeEditedDownload(
	w http.ResponseWriter, r *http.Request, store *storage.Storage,
	photo *database.Photo, rel, fileName string,
) bool {
	editsRepo, err := database.GetPhotoEditsReader(r.Context())
	if err != nil {
		return false
	}
	edits, err := editsRepo.GetPhotoEdits(r.Context(), photo.UID)
	if errors.Is(err, database.ErrNotFound) {
		return false
	}
	if err != nil {
		log.Printf("photo edited download lookup %s: %v",
			sanitizeForLog(photo.UID), err)
		return false
	}
	if edits == nil {
		return false
	}
	abs, err := store.AbsOriginal(rel)
	if err != nil {
		log.Printf("photo edited download abs %s: %v",
			sanitizeForLog(photo.UID), err)
		return false
	}
	img, err := imgedit.DecodeAndApply(
		r.Context(), abs, photo.FileOrientation, domainToImgedit(edits))
	if err != nil {
		log.Printf("photo edited download render %s: %v",
			sanitizeForLog(photo.UID), err)
		return false
	}
	data, err := imgedit.EncodeJPEG(img, downloadJPEGQuality)
	if err != nil {
		log.Printf("photo edited download encode %s: %v",
			sanitizeForLog(photo.UID), err)
		return false
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename=%q`, fileName))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("ETag", fmt.Sprintf(`"edited:%s:%d"`,
		photo.FileHash, edits.UpdatedAt.UnixNano()))
	w.WriteHeader(http.StatusOK)
	//nolint:gosec // G705: data is a freshly-encoded JPEG byte slice, not user input.
	if _, err := w.Write(data); err != nil {
		log.Printf("photo edited download write %s: %v",
			sanitizeForLog(photo.UID), err)
	}
	return true
}

// downloadJPEGQuality is the JPEG quality used when streaming the
// edited-download response. Matches the spec's "quality 92" call-out.
const downloadJPEGQuality = 92

// domainToImgedit converts the database domain struct to the imgedit
// parameter struct used by the rendering pipeline.
func domainToImgedit(e *database.PhotoEdits) imgedit.PhotoEdits {
	if e == nil {
		return imgedit.PhotoEdits{}
	}
	out := imgedit.PhotoEdits{
		Rotation:   e.Rotation,
		Brightness: e.Brightness,
		Contrast:   e.Contrast,
	}
	if e.Crop != nil {
		out.Crop = &imgedit.CropRect{
			X: e.Crop.X, Y: e.Crop.Y, W: e.Crop.W, H: e.Crop.H,
		}
	}
	return out
}
