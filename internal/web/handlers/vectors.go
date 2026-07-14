package handlers

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// Accepted values of the ?encoding= parameter on the vector feeds.
//
// json renders a vector as a JSON array of numbers. Go writes each float32
// with the shortest decimal string that round-trips *as a float32*, so parsing
// the array back into float32 reproduces the original bits exactly — the JSON
// form is lossless, just fat (a 768-dim CLIP vector is ≈9 KB).
//
// base64 renders the same vector as base64 (standard alphabet, padded) of its
// little-endian IEEE-754 float32 bytes: 4 bytes per component, ≈4 KB for the
// same vector, and no decimal conversion on either side. Bulk exporters should
// prefer it; the JSON form stays the default because it is the one you can
// read in a terminal.
const (
	vectorEncodingJSON   = "json"
	vectorEncodingBase64 = "base64"
)

// VectorsHandler serves the migration export feeds for the two vector tables:
// the 768-dim CLIP photo embeddings and the 512-dim face vectors.
//
// These are the only part of the library that inference cannot cheaply
// recreate — re-deriving 20k embeddings and 112k face vectors is hours of GPU
// time — so an API-only migration needs them verbatim. Both feeds are
// read-only, keyset-paginated, and mounted inside the authenticated group:
// the read-only `psat_` API token is the intended credential, and anonymous
// access is never permitted (the vectors are the whole library's fingerprint).
// They are deliberately absent from the public share routes.
type VectorsHandler struct {
	// embeddings/faces may be nil when the Postgres backend was not
	// registered (a partial wiring, or a very early boot); the endpoints then
	// return 503 rather than panicking.
	embeddings database.EmbeddingExportReader
	faces      database.FaceExportReader
}

// NewVectorsHandler creates the vector export handler. Either reader may be
// nil — the corresponding endpoints then surface a 503.
func NewVectorsHandler(
	embeddings database.EmbeddingExportReader, faces database.FaceExportReader,
) *VectorsHandler {
	return &VectorsHandler{embeddings: embeddings, faces: faces}
}

// EmbeddingItem is one photo embedding on the wire.
//
// model/pretrained/dim ride along on every row on purpose: an importer must be
// able to prove the vectors it stores came from the same model it will later
// query with, and reject the import outright when they did not. Exactly one of
// Embedding / EmbeddingB64 is populated, per the ?encoding= parameter.
type EmbeddingItem struct {
	PhotoUID     string    `json:"photo_uid"`
	Model        string    `json:"model"`
	Pretrained   string    `json:"pretrained"`
	Dim          int       `json:"dim"`
	CreatedAt    time.Time `json:"created_at"`
	Embedding    []float32 `json:"embedding,omitempty"`
	EmbeddingB64 string    `json:"embedding_b64,omitempty"`
}

// FaceItem is one row of the faces table on the wire — the stored columns
// verbatim, so an importer can rebuild the table without a second call.
//
// There is no pretrained field: unlike embeddings, the faces table stores no
// such column (the detector is identified by model + dim alone).
type FaceItem struct {
	ID           int64     `json:"id"`
	PhotoUID     string    `json:"photo_uid"`
	FaceIndex    int       `json:"face_index"`
	Model        string    `json:"model"`
	Dim          int       `json:"dim"`
	BBox         []float64 `json:"bbox"`
	DetScore     float64   `json:"det_score"`
	MarkerUID    string    `json:"marker_uid"`
	SubjectUID   string    `json:"subject_uid"`
	SubjectName  string    `json:"subject_name"`
	PhotoWidth   int       `json:"photo_width"`
	PhotoHeight  int       `json:"photo_height"`
	Orientation  int       `json:"orientation"`
	FileUID      string    `json:"file_uid"`
	CreatedAt    time.Time `json:"created_at"`
	Embedding    []float32 `json:"embedding,omitempty"`
	EmbeddingB64 string    `json:"embedding_b64,omitempty"`
}

// EmbeddingFeedResponse is the envelope of GET /api/v1/embeddings.
//
// NextAfter is the value to echo back as ?after= for the next page, or null
// when the walk is complete. Total ignores ?after= so it stays a usable
// progress denominator across the whole export.
type EmbeddingFeedResponse struct {
	Embeddings []EmbeddingItem `json:"embeddings"`
	Total      int             `json:"total"`
	Limit      int             `json:"limit"`
	Encoding   string          `json:"encoding"`
	NextAfter  *string         `json:"next_after"`
}

// FaceFeedResponse is the envelope of GET /api/v1/faces. NextAfter carries the
// id of the last row on the page (null when the walk is complete).
type FaceFeedResponse struct {
	Faces     []FaceItem `json:"faces"`
	Total     int        `json:"total"`
	Limit     int        `json:"limit"`
	Encoding  string     `json:"encoding"`
	NextAfter *int64     `json:"next_after"`
}

// encodeVectorB64 renders a float32 slice as base64 of its little-endian
// IEEE-754 bytes. This is the exact memory layout of a float32 array on every
// platform this runs on, so a consumer decodes it with one memcpy — and, more
// to the point, the bits survive the trip untouched.
func encodeVectorB64(v []float32) string {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// parseVectorEncoding reads ?encoding=, defaulting to json. An unknown value
// is a 400 rather than a silent fallback: a client that misspells "base64" and
// gets number arrays back would decode 9 KB of digits as if they were bytes.
func parseVectorEncoding(w http.ResponseWriter, r *http.Request) (string, bool) {
	switch enc := r.URL.Query().Get("encoding"); enc {
	case "", vectorEncodingJSON:
		return vectorEncodingJSON, true
	case vectorEncodingBase64:
		return vectorEncodingBase64, true
	default:
		respondError(w, http.StatusBadRequest, "invalid encoding (want json or base64)")
		return "", false
	}
}

// parseVectorLimit reads ?limit=. A non-numeric value is a 400; an
// out-of-range one is clamped by the repository (and reported back in the
// response's limit field), matching how GET /photos treats its limit.
func parseVectorLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid limit")
		return 0, false
	}
	return limit, true
}

// parseFaceAfter reads the face feed's ?after=<id> keyset parameter. Faces are
// keyed by a BIGSERIAL id, so the cursor is numeric and must be >= 0.
func parseFaceAfter(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, true
	}
	after, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || after < 0 {
		respondError(w, http.StatusBadRequest, "invalid after (want a face id >= 0)")
		return 0, false
	}
	return after, true
}

// applyVectorEncoding writes a vector into whichever of the two wire fields
// the requested encoding selected.
func applyVectorEncoding(encoding string, vec []float32, jsonOut *[]float32, b64Out *string) {
	if encoding == vectorEncodingBase64 {
		*b64Out = encodeVectorB64(vec)
		return
	}
	*jsonOut = vec
}

// toEmbeddingItem converts a stored embedding to its wire form.
func toEmbeddingItem(emb database.StoredEmbedding, encoding string) EmbeddingItem {
	item := EmbeddingItem{
		PhotoUID:   emb.PhotoUID,
		Model:      emb.Model,
		Pretrained: emb.Pretrained,
		Dim:        emb.Dim,
		CreatedAt:  emb.CreatedAt,
	}
	applyVectorEncoding(encoding, emb.Embedding, &item.Embedding, &item.EmbeddingB64)
	return item
}

// toFaceItem converts a stored face to its wire form.
func toFaceItem(face database.StoredFace, encoding string) FaceItem {
	bbox := face.BBox
	if bbox == nil {
		bbox = []float64{}
	}
	item := FaceItem{
		ID:          face.ID,
		PhotoUID:    face.PhotoUID,
		FaceIndex:   face.FaceIndex,
		Model:       face.Model,
		Dim:         face.Dim,
		BBox:        bbox,
		DetScore:    face.DetScore,
		MarkerUID:   face.MarkerUID,
		SubjectUID:  face.SubjectUID,
		SubjectName: face.SubjectName,
		PhotoWidth:  face.PhotoWidth,
		PhotoHeight: face.PhotoHeight,
		Orientation: face.Orientation,
		FileUID:     face.FileUID,
		CreatedAt:   face.CreatedAt,
	}
	applyVectorEncoding(encoding, face.Embedding, &item.Embedding, &item.EmbeddingB64)
	return item
}

// ListEmbeddings serves GET /api/v1/embeddings — the keyset-paginated feed of
// every photo embedding, ordered by photo_uid so an interrupted export resumes
// with ?after=<last photo_uid>.
func (h *VectorsHandler) ListEmbeddings(w http.ResponseWriter, r *http.Request) {
	if h.embeddings == nil {
		respondError(w, http.StatusServiceUnavailable, "embeddings not available")
		return
	}
	encoding, ok := parseVectorEncoding(w, r)
	if !ok {
		return
	}
	limit, ok := parseVectorLimit(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	after := r.URL.Query().Get("after")
	rows, err := h.embeddings.ListEmbeddingsAfter(ctx, after, limit)
	if err != nil {
		log.Printf("embeddings feed: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list embeddings")
		return
	}
	total, err := h.totalEmbeddings(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to count embeddings")
		return
	}

	effective := database.ClampEmbeddingExportLimit(limit)
	items := make([]EmbeddingItem, 0, len(rows))
	for i := range rows {
		items = append(items, toEmbeddingItem(rows[i], encoding))
	}

	var next *string
	if isFullPage(len(rows), effective) {
		last := rows[len(rows)-1].PhotoUID
		next = &last
	}

	respondJSON(w, http.StatusOK, EmbeddingFeedResponse{
		Embeddings: items,
		Total:      total,
		Limit:      effective,
		Encoding:   encoding,
		NextAfter:  next,
	})
}

// ListFaces serves GET /api/v1/faces — the keyset-paginated feed of every row
// in the faces table, ordered by id so an interrupted export resumes with
// ?after=<last id>.
func (h *VectorsHandler) ListFaces(w http.ResponseWriter, r *http.Request) {
	if h.faces == nil {
		respondError(w, http.StatusServiceUnavailable, "face data not available")
		return
	}
	encoding, ok := parseVectorEncoding(w, r)
	if !ok {
		return
	}
	limit, ok := parseVectorLimit(w, r)
	if !ok {
		return
	}
	after, ok := parseFaceAfter(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	rows, err := h.faces.ListFacesAfter(ctx, after, limit)
	if err != nil {
		log.Printf("faces feed: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list faces")
		return
	}
	total, err := h.totalFaces(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to count faces")
		return
	}

	effective := database.ClampFaceExportLimit(limit)
	items := make([]FaceItem, 0, len(rows))
	for i := range rows {
		items = append(items, toFaceItem(rows[i], encoding))
	}

	var next *int64
	if isFullPage(len(rows), effective) {
		last := rows[len(rows)-1].ID
		next = &last
	}

	respondJSON(w, http.StatusOK, FaceFeedResponse{
		Faces:     items,
		Total:     total,
		Limit:     effective,
		Encoding:  encoding,
		NextAfter: next,
	})
}

// GetPhotoEmbedding serves GET /api/v1/photos/{uid}/embedding — the single-row
// spot check that lets an importer diff one photo's vector against what it
// stored, without walking the feed.
func (h *VectorsHandler) GetPhotoEmbedding(w http.ResponseWriter, r *http.Request) {
	if h.embeddings == nil {
		respondError(w, http.StatusServiceUnavailable, "embeddings not available")
		return
	}
	encoding, ok := parseVectorEncoding(w, r)
	if !ok {
		return
	}
	photoUID := chi.URLParam(r, "uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "photo uid is required")
		return
	}

	emb, err := h.embeddings.Get(r.Context(), photoUID)
	if err != nil {
		log.Printf("embedding %s: %v", sanitizeForLog(photoUID), err)
		respondError(w, http.StatusInternalServerError, "failed to get embedding")
		return
	}
	// A photo with no embedding is a 404, not an empty vector: "not embedded
	// yet" and "embedded to all zeros" must not look the same to an importer.
	if emb == nil {
		respondError(w, http.StatusNotFound, "embedding not found")
		return
	}

	respondJSON(w, http.StatusOK, toEmbeddingItem(*emb, encoding))
}

// totalEmbeddings returns the row count of the embeddings table, logging (and
// wrapping) any failure.
func (h *VectorsHandler) totalEmbeddings(ctx context.Context) (int, error) {
	total, err := h.embeddings.Count(ctx)
	if err != nil {
		log.Printf("embeddings feed: count: %v", err)
		return 0, errors.New("count embeddings")
	}
	return total, nil
}

// totalFaces returns the row count of the faces table, logging (and wrapping)
// any failure.
func (h *VectorsHandler) totalFaces(ctx context.Context) (int, error) {
	total, err := h.faces.Count(ctx)
	if err != nil {
		log.Printf("faces feed: count: %v", err)
		return 0, errors.New("count faces")
	}
	return total, nil
}

// isFullPage reports whether a page came back full, i.e. whether there may be
// more rows after it.
//
// A short page means the server had nothing more to give, and minting a cursor
// there would invite a client to poll forever. The converse — a full *final*
// page yielding a cursor whose next fetch returns zero rows — is not a bug: it
// terminates the loop one request later. "Full" is measured against the
// *effective* limit the repository applied, not the one the client asked for;
// a client requesting limit=10000 gets the cap back, and comparing against
// 10000 would read that full page as short and end the export on page one.
func isFullPage(got, effectiveLimit int) bool {
	return got > 0 && got >= effectiveLimit
}
