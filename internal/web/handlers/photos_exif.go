package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/exif"
)

// exifEditFields holds the parsed, validated PUT /photos/{uid}/exif body.
// Pointer fields are nil when the corresponding JSON key was absent, so we
// only touch the photo columns and sidecar tags the caller actually
// supplied. The trailing block (keywords, artist, copyright, license,
// software, panorama, scan) was added by the metadata gap-fix task —
// see docs/specs/task-68fc8ca2-… for the rationale.
//
// `keywords` is the one tri-state field: nil means "absent in payload",
// while a non-nil zero-length slice means "clear all keywords".
type exifEditFields struct {
	takenAt     *time.Time
	lat         *float64
	lng         *float64
	altitude    *float64
	cameraMake  *string
	cameraModel *string
	lensModel   *string
	iso         *int
	aperture    *float64
	exposure    *string
	focalLength *float64
	title       *string
	description *string
	notes       *string

	keywords  []string // nil = absent, []string{} = clear, [...] = set
	hasKW     bool     // distinguishes nil-because-absent from nil-because-explicit
	artist    *string
	copyright *string
	license   *string
	software  *string
	panorama  *bool
	scan      *bool
}

// EditExif mutates the EXIF-style metadata on a photo and writes an XMP
// sidecar next to the original so the change survives a future re-import.
//
// PUT /api/v1/photos/{uid}/exif
//
// The DB row is the source of truth; if writing the sidecar fails (for
// example because exiftool is not installed) the request still succeeds —
// the error is logged but the API contract reflects what was persisted.
func (h *PhotosHandler) EditExif(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "write access required")
		return
	}

	fields, errMsg := parseExifEdit(r)
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
		log.Printf("photos exif edit get %s: %v", sanitizeForLog(uid), err)
		respondError(w, http.StatusInternalServerError, "failed to get photo")
		return
	}
	if photo.ArchivedAt != nil {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	applyExifEdit(photo, fields)

	if updateErr := writer.UpdatePhoto(r.Context(), photo); updateErr != nil {
		if errors.Is(updateErr, database.ErrNotFound) {
			respondError(w, http.StatusNotFound, "photo not found")
			return
		}
		log.Printf("photos exif edit %s: %v", sanitizeForLog(uid), updateErr)
		respondError(w, http.StatusInternalServerError, "failed to update photo")
		return
	}

	h.writeExifSidecar(r, photo, fields)

	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionPhotoExifEdit, audit.EntityPhoto, uid, nil,
	)
	respondJSON(w, http.StatusOK, nativePhotoToResponse(*photo))
}

// writeExifSidecar derives the sidecar path next to the original and asks
// the exif package to write it. Errors are logged but not propagated:
// the DB row is authoritative and the caller already got a successful
// response from EditExif.
func (h *PhotosHandler) writeExifSidecar(
	r *http.Request, photo *database.Photo, fields exifEditFields,
) {
	if h.store == nil || photo.FilePath == "" {
		return
	}
	sidecarRel := sidecarRelPath(photo.FilePath)
	if sidecarRel == "" {
		return
	}
	abs, err := h.store.AbsOriginal(sidecarRel)
	if err != nil {
		log.Printf("photos exif sidecar resolve %s: %v", sanitizeForLog(photo.UID), err)
		return
	}
	if writeErr := exif.WriteSidecar(r.Context(), abs, exifEditToSidecar(fields)); writeErr != nil {
		log.Printf("photos exif sidecar write %s: %v", sanitizeForLog(photo.UID), writeErr)
	}
}

// sidecarRelPath returns the XMP sidecar path that lives next to the
// original: same directory + same basename + ".xmp" extension. The input
// is the photo's storage-relative file path; the output stays relative to
// the same originals root.
func sidecarRelPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	ext := filepath.Ext(filePath)
	if ext == "" {
		return filePath + ".xmp"
	}
	return strings.TrimSuffix(filePath, ext) + ".xmp"
}

// exifEditToSidecar projects the request-level fields onto the sidecar
// writer's input struct. Only fields the caller supplied are forwarded.
func exifEditToSidecar(f exifEditFields) exif.SidecarFields {
	out := exif.SidecarFields{
		TakenAt:     f.takenAt,
		Lat:         f.lat,
		Lng:         f.lng,
		Altitude:    f.altitude,
		ISO:         f.iso,
		Aperture:    f.aperture,
		FocalLength: f.focalLength,
		Panorama:    f.panorama,
		Scan:        f.scan,
	}
	copySidecarStringFields(&out, f)
	if f.hasKW {
		// Pass the slice through (possibly empty) so the sidecar writer
		// can tell "clear" from "absent".
		if f.keywords == nil {
			out.Keywords = []string{}
		} else {
			out.Keywords = f.keywords
		}
	}
	return out
}

// copySidecarStringFields fans out the *string fields onto the sidecar
// projection. Split out of exifEditToSidecar so the parent stays under
// the cyclomatic complexity budget.
func copySidecarStringFields(out *exif.SidecarFields, f exifEditFields) {
	pairs := []struct {
		src *string
		dst *string
	}{
		{f.cameraMake, &out.CameraMake},
		{f.cameraModel, &out.CameraModel},
		{f.lensModel, &out.LensModel},
		{f.exposure, &out.Exposure},
		{f.title, &out.Title},
		{f.description, &out.Description},
		{f.notes, &out.Notes},
		{f.artist, &out.Artist},
		{f.copyright, &out.Copyright},
		{f.license, &out.License},
		{f.software, &out.Software},
	}
	for _, p := range pairs {
		if p.src != nil {
			*p.dst = *p.src
		}
	}
}

// applyExifEdit copies the fields the caller supplied onto the photo row.
// Missing keys leave existing values alone; explicit empty strings clear
// the corresponding column.
func applyExifEdit(p *database.Photo, f exifEditFields) {
	if f.takenAt != nil {
		t := *f.takenAt
		p.TakenAt = &t
	}
	applyExifGeo(p, f)
	applyExifCamera(p, f)
	applyExifText(p, f)
	applyExifTags(p, f)
}

// applyExifTags copies the gap-fix metadata fields (keywords, artist,
// copyright, license, software, panorama, scan) onto the photo row.
func applyExifTags(p *database.Photo, f exifEditFields) {
	if f.artist != nil {
		p.ExifArtist = *f.artist
	}
	if f.copyright != nil {
		p.ExifCopyright = *f.copyright
	}
	if f.license != nil {
		p.ExifLicense = *f.license
	}
	if f.software != nil {
		p.ExifSoftware = *f.software
	}
	if f.panorama != nil {
		p.Panorama = *f.panorama
	}
	if f.scan != nil {
		p.Scan = *f.scan
	}
	if f.hasKW {
		if f.keywords == nil {
			p.Keywords = []string{}
		} else {
			p.Keywords = append([]string(nil), f.keywords...)
		}
	}
}

// applyExifGeo lifts lat/lng/altitude onto the photo. lat and lng must be
// supplied together (the parser enforces this); altitude is independent.
func applyExifGeo(p *database.Photo, f exifEditFields) {
	if f.lat != nil && f.lng != nil {
		lat, lng := *f.lat, *f.lng
		p.Lat, p.Lng = &lat, &lng
	}
	if f.altitude != nil {
		alt := *f.altitude
		p.Altitude = &alt
	}
}

// applyExifCamera lifts camera / lens / exposure fields onto the photo.
func applyExifCamera(p *database.Photo, f exifEditFields) {
	if f.cameraMake != nil {
		p.CameraMake = *f.cameraMake
	}
	if f.cameraModel != nil {
		p.CameraModel = *f.cameraModel
	}
	if f.lensModel != nil {
		p.LensModel = *f.lensModel
	}
	if f.iso != nil {
		v := *f.iso
		p.ISO = &v
	}
	if f.aperture != nil {
		v := *f.aperture
		p.Aperture = &v
	}
	if f.focalLength != nil {
		v := *f.focalLength
		p.FocalLength = &v
	}
	if f.exposure != nil {
		p.Exposure = *f.exposure
	}
}

// applyExifText lifts title / description / notes onto the photo.
func applyExifText(p *database.Photo, f exifEditFields) {
	if f.title != nil {
		p.Title = *f.title
	}
	if f.description != nil {
		p.Description = *f.description
	}
	if f.notes != nil {
		p.Notes = *f.notes
	}
}

// exifEditReadOnlyKeys are payload keys that are exposed via GET but must
// NEVER be modified through this endpoint. Quality is derived during
// processing; taken_at_offset and time_zone are set by the upload
// pipeline and migration. Rejecting them up front gives the caller a
// clear 400 instead of a silently dropped field.
var exifEditReadOnlyKeys = []string{"quality", "taken_at_offset", "time_zone"}

// parseExifEdit decodes the JSON body into exifEditFields and runs the
// per-field validation specified by the task: year in [1900, 2100], lat
// ∈ [-90, 90], lng ∈ [-180, 180], ISO > 0. Returns an error message
// suitable for a 400 response when validation fails.
func parseExifEdit(r *http.Request) (exifEditFields, string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return exifEditFields{}, errInvalidRequestBody
	}
	for _, k := range exifEditReadOnlyKeys {
		if _, present := raw[k]; present {
			return exifEditFields{}, k + " is read-only"
		}
	}
	out, msg := decodeExifEditFields(raw)
	if msg != "" {
		return out, msg
	}
	if out.title != nil && len(*out.title) > titleMaxLen {
		return out, "title too long"
	}
	return out, ""
}

// decodeExifEditFields fans out per-field decoding. Splitting the steps
// keeps each helper trivially testable and the parent under the lint
// cyclomatic-complexity budget.
func decodeExifEditFields(raw map[string]json.RawMessage) (exifEditFields, string) {
	var out exifEditFields
	steps := []func() string{
		func() string { return decodeStringField(raw, "title", &out.title) },
		func() string { return decodeStringField(raw, "description", &out.description) },
		func() string { return decodeStringField(raw, "notes", &out.notes) },
		func() string { return decodeStringField(raw, "camera_make", &out.cameraMake) },
		func() string { return decodeStringField(raw, "camera_model", &out.cameraModel) },
		func() string { return decodeStringField(raw, "lens_model", &out.lensModel) },
		func() string { return decodeStringField(raw, "exposure", &out.exposure) },
		func() string { return decodeTakenAt(raw, &out.takenAt) },
		func() string { return decodeLatLng(raw, &out.lat, &out.lng) },
		func() string { return decodeFloatField(raw, "altitude", &out.altitude) },
		func() string { return decodeISO(raw, &out.iso) },
		func() string { return decodeFloatField(raw, "aperture", &out.aperture) },
		func() string { return decodeFloatField(raw, "focal_length", &out.focalLength) },
		func() string { return decodeStringField(raw, "exif_artist", &out.artist) },
		func() string { return decodeStringField(raw, "exif_copyright", &out.copyright) },
		func() string { return decodeStringField(raw, "exif_license", &out.license) },
		func() string { return decodeStringField(raw, "exif_software", &out.software) },
		func() string { return decodeBoolField(raw, "panorama", &out.panorama) },
		func() string { return decodeBoolField(raw, "scan", &out.scan) },
		func() string { return decodeKeywords(raw, &out) },
	}
	for _, step := range steps {
		if msg := step(); msg != "" {
			return out, msg
		}
	}
	return out, ""
}

// decodeKeywords parses raw["keywords"] as a JSON array of strings. Each
// element is trimmed; empty / whitespace-only tokens are dropped; the
// resulting slice is deduplicated while preserving first-seen ordering.
// A non-array value (e.g. a plain string) returns a 400-friendly error.
func decodeKeywords(raw map[string]json.RawMessage, out *exifEditFields) string {
	v, ok := raw["keywords"]
	if !ok {
		return ""
	}
	var arr []string
	if err := json.Unmarshal(v, &arr); err != nil {
		return "invalid keywords"
	}
	cleaned := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for _, kw := range arr {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if _, dup := seen[kw]; dup {
			continue
		}
		seen[kw] = struct{}{}
		cleaned = append(cleaned, kw)
	}
	out.keywords = cleaned
	out.hasKW = true
	return ""
}

// decodeFloatField copies raw[key] into *dest when present.
func decodeFloatField(raw map[string]json.RawMessage, key string, dest **float64) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return "invalid " + key
	}
	*dest = &f
	return ""
}

// decodeISO parses raw["iso"] and enforces the > 0 constraint from the
// task spec.
func decodeISO(raw map[string]json.RawMessage, dest **int) string {
	v, ok := raw["iso"]
	if !ok {
		return ""
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		return "invalid iso"
	}
	if n <= 0 {
		return "iso must be positive"
	}
	*dest = &n
	return ""
}
