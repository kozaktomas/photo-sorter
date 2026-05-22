package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// maxGeoPoints caps GET /photos/geo-points. The frontend uses these for
// client-side clustering with supercluster — beyond 50k points the browser
// starts to struggle. The cap matches the spec.
const maxGeoPoints = 50_000

// histogramBucketMonth / histogramBucketYear are the two date_trunc units
// accepted by GET /photos/histogram. Centralised here so the default
// fallback, the validation switch, and the test fixtures all reference
// the same string.
const (
	histogramBucketMonth = "month"
	histogramBucketYear  = "year"
)

// HistogramBucketResponse mirrors database.HistogramBucket on the wire. The
// time fields are RFC3339 (UTC) strings so the frontend can pass them
// straight to new Date() without any parsing.
type HistogramBucketResponse struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Count int    `json:"count"`
}

// HistogramResponse is the envelope for GET /photos/histogram. Bucket is
// echoed back so the client can confirm which bucketing was applied — the
// server may still pick "month" when the caller passed an empty string.
type HistogramResponse struct {
	Bucket      string                    `json:"bucket"`
	Buckets     []HistogramBucketResponse `json:"buckets"`
	Total       int                       `json:"total"`
	NoDateCount int                       `json:"no_date_count"`
	NoGPSCount  int                       `json:"no_gps_count"`
}

// GeoPointResponse mirrors database.GeoPoint on the wire.
type GeoPointResponse struct {
	UID string  `json:"uid"`
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// GeoPointsResponse is the envelope for GET /photos/geo-points. Truncated
// is true when the result set was capped at the server-side limit and the
// UI should warn the user that points are missing.
type GeoPointsResponse struct {
	Points    []GeoPointResponse `json:"points"`
	Total     int                `json:"total"`
	Truncated bool               `json:"truncated"`
	Cap       int                `json:"cap"`
}

// resolveBrowseReader returns a PhotoBrowseReader or writes a 503 to w.
// Honours a pre-injected reader on the handler (tests) before falling back
// to the registered Postgres backend.
func (h *PhotosHandler) resolveBrowseReader(w http.ResponseWriter) database.PhotoBrowseReader {
	if h.browseReader != nil {
		return h.browseReader
	}
	reader, err := database.GetPhotoBrowseReader(context.Background())
	if err != nil {
		log.Printf("photos browse: %v", err)
		respondError(w, http.StatusServiceUnavailable, "photo storage not available")
		return nil
	}
	h.browseReader = reader
	return reader
}

// Histogram serves GET /api/v1/photos/histogram. It accepts every filter
// query parameter that the regular photos list accepts (label_uid,
// subject_uid, favorite, taken_from, taken_to, the geo bbox, q) plus a
// `bucket` parameter that selects between "month" (default) and "year".
// Pagination params (limit/offset) are read by parsePhotoFilter but
// ignored — the histogram always reflects the full matching set.
func (h *PhotosHandler) Histogram(w http.ResponseWriter, r *http.Request) {
	filter, ok := parsePhotoFilter(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = histogramBucketMonth
	}
	if bucket != histogramBucketMonth && bucket != histogramBucketYear {
		respondError(w, http.StatusBadRequest, "bucket must be 'month' or 'year'")
		return
	}

	reader := h.resolveBrowseReader(w)
	if reader == nil {
		return
	}

	result, err := reader.Histogram(r.Context(), filter, bucket)
	if err != nil {
		log.Printf("photos histogram: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to compute histogram")
		return
	}

	resp := HistogramResponse{
		Bucket:      bucket,
		Buckets:     make([]HistogramBucketResponse, 0, len(result.Buckets)),
		Total:       result.Total,
		NoDateCount: result.NoDateCount,
		NoGPSCount:  result.NoGPSCount,
	}
	for _, b := range result.Buckets {
		resp.Buckets = append(resp.Buckets, HistogramBucketResponse{
			Start: b.Start.UTC().Format(time.RFC3339),
			End:   b.End.UTC().Format(time.RFC3339),
			Count: b.Count,
		})
	}
	respondJSON(w, http.StatusOK, resp)
}

// GeoPoints serves GET /api/v1/photos/geo-points. Same filter contract as
// Histogram; the response is just (uid, lat, lng) triples capped at
// maxGeoPoints. Photos with NULL lat/lng are silently excluded — callers
// can use the histogram endpoint to learn how many such photos exist.
func (h *PhotosHandler) GeoPoints(w http.ResponseWriter, r *http.Request) {
	filter, ok := parsePhotoFilter(w, r)
	if !ok {
		return
	}
	reader := h.resolveBrowseReader(w)
	if reader == nil {
		return
	}

	points, truncated, err := reader.ListGeoPoints(r.Context(), filter, maxGeoPoints)
	if err != nil {
		log.Printf("photos geo points: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list geo points")
		return
	}

	resp := GeoPointsResponse{
		Points:    make([]GeoPointResponse, 0, len(points)),
		Total:     len(points),
		Truncated: truncated,
		Cap:       maxGeoPoints,
	}
	for _, p := range points {
		resp.Points = append(resp.Points, GeoPointResponse{
			UID: p.PhotoUID,
			Lat: p.Lat,
			Lng: p.Lng,
		})
	}
	respondJSON(w, http.StatusOK, resp)
}
