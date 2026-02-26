package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// PhotosHandler handles photo-related endpoints
type PhotosHandler struct {
	config          *config.Config
	sessionManager  *middleware.SessionManager
	embeddingReader database.EmbeddingReader
}

// NewPhotosHandler creates a new photos handler
func NewPhotosHandler(cfg *config.Config, sm *middleware.SessionManager) *PhotosHandler {
	h := &PhotosHandler{
		config:         cfg,
		sessionManager: sm,
	}

	// Try to get an embedding reader from PostgreSQL
	if reader, err := database.GetEmbeddingReader(context.Background()); err == nil {
		h.embeddingReader = reader
	}

	return h
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

// buildPhotoQuery constructs a search query from URL query parameters
func buildPhotoQuery(r *http.Request) string {
	var queryParts []string
	if query := r.URL.Query().Get("q"); query != "" {
		queryParts = append(queryParts, query)
	}
	if year := r.URL.Query().Get("year"); year != "" {
		queryParts = append(queryParts, "year:"+year)
	}
	if label := r.URL.Query().Get("label"); label != "" {
		queryParts = append(queryParts, "label:"+label)
	}
	if album := r.URL.Query().Get("album"); album != "" {
		queryParts = append(queryParts, "album:"+album)
	}
	return strings.Join(queryParts, " ")
}

// List returns all photos with optional filtering and sorting
func (h *PhotosHandler) List(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count <= 0 {
		count = constants.DefaultHandlerPageSize
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	order := r.URL.Query().Get("order")
	finalQuery := buildPhotoQuery(r)

	photos, err := pp.GetPhotosWithQueryAndOrder(count, offset, finalQuery, order)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}

	response := make([]PhotoResponse, 0, len(photos))
	for _, p := range photos {
		if p.DeletedAt != "" {
			continue
		}
		response = append(response, photoToResponse(p))
	}

	respondJSON(w, http.StatusOK, response)
}

// Get returns a single photo
func (h *PhotosHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Use query to get photo as a typed struct
	photos, err := pp.GetPhotosWithQuery(1, 0, "uid:"+uid)
	if err != nil || len(photos) == 0 {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	respondJSON(w, http.StatusOK, photoToResponse(photos[0]))
}

// UpdateRequest represents a photo update request
type UpdateRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	TakenAt     *string  `json:"taken_at,omitempty"`
	Lat         *float64 `json:"lat,omitempty"`
	Lng         *float64 `json:"lng,omitempty"`
	Favorite    *bool    `json:"favorite,omitempty"`
	Private     *bool    `json:"private,omitempty"`
}

// Update updates a photo
func (h *PhotosHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if uid == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	update := photoprism.PhotoUpdate{
		Title:       req.Title,
		Description: req.Description,
		TakenAt:     req.TakenAt,
		Lat:         req.Lat,
		Lng:         req.Lng,
		Favorite:    req.Favorite,
		Private:     req.Private,
	}

	photo, err := pp.EditPhoto(uid, update)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update photo")
		return
	}

	respondJSON(w, http.StatusOK, photoToResponse(*photo))
}

// Thumbnail proxies a photo thumbnail
func (h *PhotosHandler) Thumbnail(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	size := chi.URLParam(r, "size")

	if uid == "" || size == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID or size")
		return
	}

	// Validate size
	validSizes := map[string]bool{
		"tile_50": true, "tile_100": true, "left_224": true, "right_224": true,
		"tile_224": true, "tile_500": true, "fit_720": true, "tile_1080": true,
		"fit_1280": true, "fit_1600": true, "fit_1920": true, "fit_2048": true,
		"fit_2560": true, "fit_3840": true, "fit_4096": true, "fit_7680": true,
	}
	if !validSizes[size] {
		respondError(w, http.StatusBadRequest, "invalid size")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Get photo to retrieve the hash
	photos, err := pp.GetPhotosWithQuery(1, 0, "uid:"+uid)
	if err != nil || len(photos) == 0 {
		respondError(w, http.StatusNotFound, "photo not found")
		return
	}

	hash := photos[0].Hash
	if hash == "" {
		respondError(w, http.StatusNotFound, "photo has no thumbnail")
		return
	}

	// Get thumbnail
	data, contentType, err := pp.GetPhotoThumbnail(hash, size)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get thumbnail")
		return
	}

	if !strings.HasPrefix(contentType, "image/") {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, bytes.NewReader(data))
}

// BatchAddLabelsRequest represents a request to add labels to multiple photos
type BatchAddLabelsRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
	Label     string   `json:"label"`
}

// BatchAddLabelsResponse represents the response from batch adding labels
type BatchAddLabelsResponse struct {
	Updated int      `json:"updated"`
	Errors  []string `json:"errors,omitempty"`
}

// BatchAddLabels adds a label to multiple photos
func (h *PhotosHandler) BatchAddLabels(w http.ResponseWriter, r *http.Request) {
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

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	label := photoprism.PhotoLabel{
		Name:     req.Label,
		LabelSrc: "manual",
	}

	var errors []string
	updated := 0

	for _, photoUID := range req.PhotoUIDs {
		_, err := pp.AddPhotoLabel(photoUID, label)
		if err != nil {
			errors = append(errors, photoUID+": "+err.Error())
		} else {
			updated++
		}
	}

	respondJSON(w, http.StatusOK, BatchAddLabelsResponse{
		Updated: updated,
		Errors:  errors,
	})
}

// BatchArchiveRequest represents a request to archive multiple photos
type BatchArchiveRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// BatchArchive archives (soft-deletes) multiple photos
func (h *PhotosHandler) BatchArchive(w http.ResponseWriter, r *http.Request) {
	var req BatchArchiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if len(req.PhotoUIDs) == 0 {
		respondError(w, http.StatusBadRequest, "photo_uids is required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	if err := pp.ArchivePhotos(req.PhotoUIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to archive photos")
		return
	}

	respondJSON(w, http.StatusOK, map[string]int{"archived": len(req.PhotoUIDs)})
}

// SimilarRequest represents a similar photos search request
type SimilarRequest struct {
	PhotoUID  string  `json:"photo_uid"`
	Limit     int     `json:"limit,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// SimilarPhotoResult represents a single similar photo result
type SimilarPhotoResult struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`   // Cosine distance (0-2, lower = more similar)
	Similarity float64 `json:"similarity"` // 1 - distance (for easier interpretation)
}

// SimilarResponse represents the full similar photos response
type SimilarResponse struct {
	SourcePhotoUID string               `json:"source_photo_uid"`
	Threshold      float64              `json:"threshold"`
	Results        []SimilarPhotoResult `json:"results"`
	Count          int                  `json:"count"`
}

// CollectionSimilarRequest represents a request to find similar photos to a collection
type CollectionSimilarRequest struct {
	SourceType string  `json:"source_type"` // "label" or "album"
	SourceID   string  `json:"source_id"`   // label name or album UID
	Limit      int     `json:"limit,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// CollectionSimilarResult represents a single similar photo result with match count
type CollectionSimilarResult struct {
	PhotoUID   string  `json:"photo_uid"`
	Distance   float64 `json:"distance"`    // Cosine distance (0-2, lower = more similar)
	Similarity float64 `json:"similarity"`  // 1 - distance (for easier interpretation)
	MatchCount int     `json:"match_count"` // Number of source photos that matched
}

// CollectionSimilarResponse represents the full collection similar photos response
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

// parseCollectionSimilarRequest parses and validates the request, returning an error message if invalid
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
	if req.SourceType != "label" && req.SourceType != "album" {
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

// fetchSourcePhotoUIDs fetches all photo UIDs matching a query from PhotoPrism
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

// collectionMatchCandidate tracks similarity match data for a candidate photo
type collectionMatchCandidate struct {
	PhotoUID   string
	Distance   float64
	MatchCount int
}

// searchCollectionSimilar searches for similar photos across all source embeddings
func searchCollectionSimilar(ctx context.Context, embRepo database.EmbeddingReader, sourcePhotoUIDs map[string]bool, limit int, threshold float64) (map[string]*collectionMatchCandidate, int) {
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

// filterAndSortCollectionResults filters by min match count, sorts, and applies limit
func filterAndSortCollectionResults(candidateMap map[string]*collectionMatchCandidate, minMatchCount, limit int) []CollectionSimilarResult {
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

// FindSimilarToCollection finds photos similar to all photos in a label or album
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

// parseSimilarRequest parses and validates the similar photos request
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

// FindSimilar finds similar photos based on image embeddings
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

// TextSearchRequest represents a text-to-image search request
type TextSearchRequest struct {
	Text      string  `json:"text"`
	Limit     int     `json:"limit,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance (lower = more similar)
}

// TextSearchResponse represents the text search results
type TextSearchResponse struct {
	Query            string               `json:"query"`
	TranslatedQuery  string               `json:"translated_query,omitempty"`
	TranslateCostUSD float64              `json:"translate_cost_usd,omitempty"`
	Threshold        float64              `json:"threshold"`
	Results          []SimilarPhotoResult `json:"results"`
	Count            int                  `json:"count"`
}

// translateQueryResult holds the result of query translation
type translateQueryResult struct {
	queryText       string
	translatedQuery string
	translateCost   float64
}

// translateQueryForCLIP optionally translates the query text to CLIP-optimized English
func translateQueryForCLIP(ctx context.Context, openAIToken, text string) translateQueryResult {
	result := translateQueryResult{queryText: text}
	if openAIToken == "" {
		return result
	}
	translated, err := ai.TranslateForCLIP(ctx, openAIToken, text)
	if err == nil && translated.Text != text {
		result.queryText = translated.Text
		result.translatedQuery = translated.Text
		result.translateCost = translated.Cost
	}
	return result
}

// SearchByText finds photos matching a text query using CLIP text embeddings
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
		TranslateCostUSD: tr.translateCost, Threshold: req.Threshold,
		Results: results, Count: len(results),
	})
}

// EraMatch represents a single era match result
type EraMatch struct {
	EraSlug            string  `json:"era_slug"`
	EraName            string  `json:"era_name"`
	RepresentativeDate string  `json:"representative_date"`
	Similarity         float64 `json:"similarity"`  // 0-1 (cosine similarity)
	Confidence         float64 `json:"confidence"`   // 0-100 percentage
}

// EraEstimateResponse represents the era estimation result for a photo
type EraEstimateResponse struct {
	PhotoUID   string    `json:"photo_uid"`
	BestMatch  *EraMatch `json:"best_match"`
	TopMatches []EraMatch `json:"top_matches"`
}

// EstimateEra estimates the era of a photo by comparing its CLIP image embedding
// against pre-computed era text embedding centroids
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

	// Get the photo's image embedding
	photoEmb, err := embRepo.Get(ctx, uid)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get embedding")
		return
	}
	if photoEmb == nil {
		respondError(w, http.StatusNotFound, "no embedding found for this photo")
		return
	}

	// Get all era centroids
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

	// Compute cosine similarity for each era
	matches := make([]EraMatch, 0, len(eras))
	for i := range eras {
		era := &eras[i]
		sim := fingerprint.CosineSimilarity(photoEmb.Embedding, era.Embedding)
		confidence := math.Max(0, math.Min(100, sim*100))
		matches = append(matches, EraMatch{
			EraSlug:            era.EraSlug,
			EraName:            era.EraName,
			RepresentativeDate: era.RepresentativeDate,
			Similarity:         sim,
			Confidence:         confidence,
		})
	}

	// Sort by similarity descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	respondJSON(w, http.StatusOK, EraEstimateResponse{
		PhotoUID:   uid,
		BestMatch:  &matches[0],
		TopMatches: matches,
	})
}

// BatchEditRequest represents a request to edit multiple photos
type BatchEditRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
	Favorite  *bool    `json:"favorite,omitempty"`
	Private   *bool    `json:"private,omitempty"`
}

// BatchEditResponse represents the response from batch editing photos
type BatchEditResponse struct {
	Updated int      `json:"updated"`
	Errors  []string `json:"errors,omitempty"`
}

// BatchEdit edits multiple photos at once (favorite, private)
func (h *PhotosHandler) BatchEdit(w http.ResponseWriter, r *http.Request) {
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

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	var errors []string
	updated := 0

	for _, photoUID := range req.PhotoUIDs {
		update := photoprism.PhotoUpdate{
			Favorite: req.Favorite,
			Private:  req.Private,
		}
		_, err := pp.EditPhoto(photoUID, update)
		if err != nil {
			errors = append(errors, photoUID+": "+err.Error())
		} else {
			updated++
		}
	}

	respondJSON(w, http.StatusOK, BatchEditResponse{
		Updated: updated,
		Errors:  errors,
	})
}

// DuplicatesRequest represents a request to find duplicate photos
type DuplicatesRequest struct {
	AlbumUID  string  `json:"album_uid,omitempty"`
	Threshold float64 `json:"threshold,omitempty"` // Max cosine distance
	Limit     int     `json:"limit,omitempty"`     // Max number of groups to return
}

// DuplicatePhoto represents a single photo in a duplicate group
type DuplicatePhoto struct {
	PhotoUID string  `json:"photo_uid"`
	Distance float64 `json:"distance"` // Distance from group representative
}

// DuplicateGroup represents a group of similar/duplicate photos
type DuplicateGroup struct {
	Photos      []DuplicatePhoto `json:"photos"`
	AvgDistance  float64          `json:"avg_distance"`
	PhotoCount  int              `json:"photo_count"`
}

// DuplicatesResponse represents the full duplicates response
type DuplicatesResponse struct {
	TotalPhotosScanned int              `json:"total_photos_scanned"`
	DuplicateGroups    []DuplicateGroup `json:"duplicate_groups"`
	TotalGroups        int              `json:"total_groups"`
	TotalDuplicates    int              `json:"total_duplicates"`
}

// duplicateUnionFind implements union-find for grouping duplicate photos
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

// fetchDuplicatePhotoUIDs retrieves photo UIDs to scan, either from an album or from all embeddings
func fetchDuplicatePhotoUIDs(ctx context.Context, r *http.Request, w http.ResponseWriter, albumUID string, embRepo database.EmbeddingReader) ([]string, bool) {
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

// populateUnionFind searches for neighbors for each photo and unions them into the UF structure
func populateUnionFind(ctx context.Context, embRepo database.EmbeddingReader, photoUIDs []string, uidSet map[string]bool, threshold float64, uf *duplicateUnionFind) {
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

// extractDuplicateGroups extracts groups of size >= 2 from the union-find structure
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

// buildDuplicateGroups clusters photos into duplicate groups using union-find
func buildDuplicateGroups(ctx context.Context, embRepo database.EmbeddingReader, photoUIDs []string, threshold float64) ([]DuplicateGroup, int) {
	uidSet := make(map[string]bool, len(photoUIDs))
	for _, uid := range photoUIDs {
		uidSet[uid] = true
	}

	uf := newDuplicateUnionFind(photoUIDs)
	populateUnionFind(ctx, embRepo, photoUIDs, uidSet, threshold, uf)
	return extractDuplicateGroups(uf, photoUIDs)
}

// averageDistance computes the mean of a float64 slice
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

// FindDuplicates finds near-duplicate photos using CLIP embedding similarity
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

// SuggestAlbumsRequest represents a request to find photos missing from existing albums
type SuggestAlbumsRequest struct {
	Threshold float64 `json:"threshold,omitempty"` // Min cosine similarity (0-1)
	TopK      int     `json:"top_k,omitempty"`     // Max photos suggested per album
}

// AlbumPhotoSuggestion represents a single photo's suggestion for an album
type AlbumPhotoSuggestion struct {
	PhotoUID   string  `json:"photo_uid"`
	Similarity float64 `json:"similarity"`
}

// AlbumSuggestion represents a suggested album with matching photos
type AlbumSuggestion struct {
	AlbumUID   string                 `json:"album_uid"`
	AlbumTitle string                 `json:"album_title"`
	Photos     []AlbumPhotoSuggestion `json:"photos"`
}

// SuggestAlbumsResponse represents the full album suggestion response
type SuggestAlbumsResponse struct {
	AlbumsAnalyzed  int               `json:"albums_analyzed"`
	PhotosAnalyzed  int               `json:"photos_analyzed"`
	Skipped         int               `json:"skipped"` // Albums skipped (no embeddings)
	Suggestions     []AlbumSuggestion `json:"suggestions"`
}

// suggestAlbumResult holds the result of processing a single album for suggestions
type suggestAlbumResult struct {
	suggestion *AlbumSuggestion
	skipped    bool
}

// fetchAlbumMemberSet fetches all photo UIDs in an album via paginated API calls
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

// collectAlbumEmbeddings fetches embeddings for all photos in the member set
func collectAlbumEmbeddings(ctx context.Context, embRepo database.EmbeddingReader, memberSet map[string]bool) [][]float32 {
	var embeddings [][]float32
	for uid := range memberSet {
		emb, err := embRepo.Get(ctx, uid)
		if err != nil || emb == nil {
			continue
		}
		embeddings = append(embeddings, emb.Embedding)
	}
	return embeddings
}

// filterSuggestedPhotos filters out album members and returns non-member photos as suggestions
func filterSuggestedPhotos(similar []database.StoredEmbedding, distances []float64, albumMemberSet map[string]bool, topK int) []AlbumPhotoSuggestion {
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

// processAlbumForSuggestions analyzes a single album and returns suggestions for missing photos
func processAlbumForSuggestions(ctx context.Context, pp *photoprism.PhotoPrism, embRepo database.EmbeddingReader, album photoprism.Album, topK int, maxDistance float64) suggestAlbumResult {
	albumMemberSet, err := fetchAlbumMemberSet(pp, album.UID)
	if err != nil || len(albumMemberSet) < constants.MinAlbumPhotosForCentroid {
		return suggestAlbumResult{}
	}

	embeddings := collectAlbumEmbeddings(ctx, embRepo, albumMemberSet)
	if len(embeddings) < constants.MinAlbumPhotosForCentroid {
		return suggestAlbumResult{skipped: true}
	}

	centroid := computeCentroid(embeddings)
	if centroid == nil {
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

// processAlbumsInParallel processes candidate albums concurrently and returns suggestions and skip count
func processAlbumsInParallel(ctx context.Context, pp *photoprism.PhotoPrism, embRepo database.EmbeddingReader, candidateAlbums []photoprism.Album, topK int, maxDistance float64) ([]AlbumSuggestion, int) {
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

// countUniqueSuggestedPhotos counts unique photos across all suggestions
func countUniqueSuggestedPhotos(suggestions []AlbumSuggestion) int {
	uniquePhotos := make(map[string]bool)
	for _, s := range suggestions {
		for _, p := range s.Photos {
			uniquePhotos[p.PhotoUID] = true
		}
	}
	return len(uniquePhotos)
}

// SuggestAlbums finds photos that belong in existing albums but aren't there yet (album completion)
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

// computeCentroid computes the mean of a set of embeddings and L2-normalizes it
func computeCentroid(embeddings [][]float32) []float32 {
	if len(embeddings) == 0 {
		return nil
	}
	dim := len(embeddings[0])
	centroid := make([]float32, dim)
	for _, emb := range embeddings {
		for i, v := range emb {
			centroid[i] += v
		}
	}
	n := float32(len(embeddings))
	for i := range centroid {
		centroid[i] /= n
	}
	// L2 normalize
	var norm float64
	for _, v := range centroid {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range centroid {
			centroid[i] = float32(float64(centroid[i]) / norm)
		}
	}
	return centroid
}

