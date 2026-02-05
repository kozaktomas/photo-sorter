package handlers

import (
	"context"
	"encoding/json"
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

// RefreshReader reloads the embedding reader from the database.
// Called after processing or index rebuild to pick up new data.
func (h *PhotosHandler) RefreshReader() {
	if reader, err := database.GetEmbeddingReader(context.Background()); err == nil {
		h.embeddingReader = reader
	}
}

// List returns all photos with optional filtering and sorting
func (h *PhotosHandler) List(w http.ResponseWriter, r *http.Request) {
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

	// Build search query from individual filters
	var queryParts []string
	if query != "" {
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

	finalQuery := ""
	if len(queryParts) > 0 {
		finalQuery = strings.Join(queryParts, " ")
	}

	photos, err := pp.GetPhotosWithQueryAndOrder(count, offset, finalQuery, order, constants.DefaultPhotoQuality)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photos")
		return
	}

	// Filter out soft-deleted (archived) photos
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
		respondError(w, http.StatusBadRequest, "invalid request body")
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

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
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
		respondError(w, http.StatusBadRequest, "invalid request body")
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
		respondError(w, http.StatusBadRequest, "invalid request body")
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

// FindSimilarToCollection finds photos similar to all photos in a label or album
func (h *PhotosHandler) FindSimilarToCollection(w http.ResponseWriter, r *http.Request) {
	var req CollectionSimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.SourceType == "" {
		respondError(w, http.StatusBadRequest, "source_type is required")
		return
	}
	if req.SourceID == "" {
		respondError(w, http.StatusBadRequest, "source_id is required")
		return
	}
	if req.SourceType != "label" && req.SourceType != "album" {
		respondError(w, http.StatusBadRequest, "source_type must be 'label' or 'album'")
		return
	}

	// Set defaults
	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSimilarityThreshold
	}

	ctx := context.Background()

	// Use cached embedding reader, fall back to fetching
	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available: run 'photo info --embedding' first")
			return
		}
	}

	// Get PhotoPrism client
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Build query based on source type
	var query string
	if req.SourceType == "label" {
		query = "label:" + req.SourceID
	} else {
		query = "album:" + req.SourceID
	}

	// Fetch all source photos
	sourcePhotoUIDs := make(map[string]bool)
	offset := 0
	pageSize := constants.DefaultHandlerPageSize

	for {
		photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get photos: "+err.Error())
			return
		}

		for _, photo := range photos {
			sourcePhotoUIDs[photo.UID] = true
		}

		if len(photos) < pageSize {
			break
		}
		offset += pageSize
	}

	if len(sourcePhotoUIDs) == 0 {
		respondJSON(w, http.StatusOK, CollectionSimilarResponse{
			SourceType:           req.SourceType,
			SourceID:             req.SourceID,
			SourcePhotoCount:     0,
			SourceEmbeddingCount: 0,
			MinMatchCount:        0,
			Threshold:            req.Threshold,
			Results:              []CollectionSimilarResult{},
			Count:                0,
		})
		return
	}

	// Track matches for each candidate photo
	type matchCandidate struct {
		PhotoUID   string
		Distance   float64 // Best (lowest) distance
		MatchCount int     // Number of source embeddings that matched
	}
	candidateMap := make(map[string]*matchCandidate)
	sourceEmbeddingCount := 0

	for photoUID := range sourcePhotoUIDs {
		emb, err := embRepo.Get(ctx, photoUID)
		if err != nil {
			continue // Skip on error
		}
		if emb == nil {
			continue // No embedding for this photo
		}
		sourceEmbeddingCount++

		// Find similar photos for this source
		similar, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, req.Limit*10, req.Threshold)
		if err != nil {
			continue // Skip on error
		}

		for i, sim := range similar {
			// Skip source photos
			if sourcePhotoUIDs[sim.PhotoUID] {
				continue
			}

			// Track match count and keep best (lowest) distance
			if existing, ok := candidateMap[sim.PhotoUID]; ok {
				existing.MatchCount++
				if distances[i] < existing.Distance {
					existing.Distance = distances[i]
				}
			} else {
				candidateMap[sim.PhotoUID] = &matchCandidate{
					PhotoUID:   sim.PhotoUID,
					Distance:   distances[i],
					MatchCount: 1,
				}
			}
		}
	}

	// Scale minMatchCount based on threshold
	// At threshold 0.5 (50% confidence), use 5% of sources (max 5)
	// At threshold 0.2 (80% confidence), use 1-2% of sources (min 1)
	// Lower threshold = stricter matching = fewer required votes
	thresholdFactor := req.Threshold / 0.5 * 0.05 // 0.05 at 0.5, 0.02 at 0.2
	if thresholdFactor < 0.01 {
		thresholdFactor = 0.01
	}
	if thresholdFactor > 0.05 {
		thresholdFactor = 0.05
	}
	minMatchCount := int(float64(sourceEmbeddingCount) * thresholdFactor)
	if minMatchCount < 1 {
		minMatchCount = 1
	}
	if minMatchCount > 5 {
		minMatchCount = 5
	}

	// Filter: only keep candidates that matched at least minMatchCount sources
	for photoUID, candidate := range candidateMap {
		if candidate.MatchCount < minMatchCount {
			delete(candidateMap, photoUID)
		}
	}

	// Convert to sorted results
	results := make([]CollectionSimilarResult, 0, len(candidateMap))
	for _, candidate := range candidateMap {
		results = append(results, CollectionSimilarResult{
			PhotoUID:   candidate.PhotoUID,
			Distance:   candidate.Distance,
			Similarity: 1 - candidate.Distance,
			MatchCount: candidate.MatchCount,
		})
	}

	// Sort by match count (desc), then by distance (asc)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].MatchCount > results[i].MatchCount ||
				(results[j].MatchCount == results[i].MatchCount && results[j].Distance < results[i].Distance) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Apply limit
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

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

// FindSimilar finds similar photos based on image embeddings
func (h *PhotosHandler) FindSimilar(w http.ResponseWriter, r *http.Request) {
	var req SimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PhotoUID == "" {
		respondError(w, http.StatusBadRequest, "photo_uid is required")
		return
	}

	// Set defaults
	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSimilarityThreshold
	}

	ctx := context.Background()

	// Use cached embedding reader, fall back to fetching
	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available: run 'photo info --embedding' first")
			return
		}
	}

	// Get the source photo's embedding
	sourceEmb, err := embRepo.Get(ctx, req.PhotoUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get embedding")
		return
	}
	if sourceEmb == nil {
		respondError(w, http.StatusNotFound, "no embedding found for this photo. Run 'photo info --embedding' first")
		return
	}

	// Search for similar photos (+1 to account for the source photo itself)
	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, sourceEmb.Embedding, req.Limit+1, req.Threshold)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to find similar photos")
		return
	}

	// Build results, excluding the source photo
	var results []SimilarPhotoResult
	for i, emb := range similar {
		if emb.PhotoUID == req.PhotoUID {
			continue // Skip the source photo
		}
		results = append(results, SimilarPhotoResult{
			PhotoUID:   emb.PhotoUID,
			Distance:   distances[i],
			Similarity: 1 - distances[i],
		})
	}

	// Apply limit after filtering
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	respondJSON(w, http.StatusOK, SimilarResponse{
		SourcePhotoUID: req.PhotoUID,
		Threshold:      req.Threshold,
		Results:        results,
		Count:          len(results),
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

// SearchByText finds photos matching a text query using CLIP text embeddings
func (h *PhotosHandler) SearchByText(w http.ResponseWriter, r *http.Request) {
	var req TextSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		respondError(w, http.StatusBadRequest, "text is required")
		return
	}

	// Set defaults
	if req.Limit <= 0 {
		req.Limit = constants.DefaultSimilarLimit
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.5 // Text search uses higher threshold
	}

	ctx := context.Background()

	// Use cached embedding reader, fall back to fetching
	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available: run 'photo process' first")
			return
		}
	}

	// Translate Czech text to CLIP-optimized English if OpenAI token is available
	queryText := req.Text
	var translatedQuery string
	var translateCost float64
	if h.config.OpenAI.Token != "" {
		result, err := ai.TranslateForCLIP(ctx, h.config.OpenAI.Token, req.Text)
		if err == nil && result.Text != req.Text {
			queryText = result.Text
			translatedQuery = result.Text
			translateCost = result.Cost
		}
	}

	// Compute text embedding using the (possibly translated) query phrase
	embClient := fingerprint.NewEmbeddingClient(h.config.Embedding.URL, "")
	textEmbedding, err := embClient.ComputeTextEmbedding(ctx, queryText)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to compute text embedding: "+err.Error())
		return
	}

	// Search for similar photos
	similar, distances, err := embRepo.FindSimilarWithDistance(ctx, textEmbedding, req.Limit, req.Threshold)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to search photos")
		return
	}

	// Build results
	results := make([]SimilarPhotoResult, 0, len(similar))
	for i, emb := range similar {
		results = append(results, SimilarPhotoResult{
			PhotoUID:   emb.PhotoUID,
			Distance:   distances[i],
			Similarity: 1 - distances[i],
		})
	}

	respondJSON(w, http.StatusOK, TextSearchResponse{
		Query:            req.Text,
		TranslatedQuery:  translatedQuery,
		TranslateCostUSD: translateCost,
		Threshold:        req.Threshold,
		Results:          results,
		Count:            len(results),
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

	// Use cached embedding reader, fall back to fetching
	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available")
			return
		}
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
	for _, era := range eras {
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
		respondError(w, http.StatusBadRequest, "invalid request body")
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

// FindDuplicates finds near-duplicate photos using CLIP embedding similarity
func (h *PhotosHandler) FindDuplicates(w http.ResponseWriter, r *http.Request) {
	var req DuplicatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultDuplicateThreshold
	}
	if req.Limit <= 0 {
		req.Limit = constants.DefaultDuplicateLimit
	}

	ctx := context.Background()

	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available")
			return
		}
	}

	// Get photo UIDs in scope
	var photoUIDs []string
	if req.AlbumUID != "" {
		pp := middleware.MustGetPhotoPrism(r.Context(), w)
		if pp == nil {
			return
		}
		offset := 0
		pageSize := constants.DefaultHandlerPageSize
		for {
			photos, err := pp.GetAlbumPhotos(req.AlbumUID, pageSize, offset)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to get album photos")
				return
			}
			for _, p := range photos {
				photoUIDs = append(photoUIDs, p.UID)
			}
			if len(photos) < pageSize {
				break
			}
			offset += pageSize
		}
	} else {
		var err error
		photoUIDs, err = embRepo.GetUniquePhotoUIDs(ctx)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get photo UIDs")
			return
		}
	}

	if len(photoUIDs) == 0 {
		respondJSON(w, http.StatusOK, DuplicatesResponse{
			TotalPhotosScanned: 0,
			DuplicateGroups:    []DuplicateGroup{},
			TotalGroups:        0,
			TotalDuplicates:    0,
		})
		return
	}

	// Build set for quick lookup
	uidSet := make(map[string]bool, len(photoUIDs))
	for _, uid := range photoUIDs {
		uidSet[uid] = true
	}

	// Union-Find data structure
	parent := make(map[string]string)
	rank := make(map[string]int)
	// Track best distance between pairs in same group
	pairDistances := make(map[string][]float64)

	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	union := func(x, y string) {
		px, py := find(x), find(y)
		if px == py {
			return
		}
		if rank[px] < rank[py] {
			px, py = py, px
		}
		parent[py] = px
		if rank[px] == rank[py] {
			rank[px]++
		}
	}

	// Initialize parent for all UIDs
	for _, uid := range photoUIDs {
		parent[uid] = uid
		rank[uid] = 0
	}

	// For each photo, find neighbors and union them
	for _, uid := range photoUIDs {
		emb, err := embRepo.Get(ctx, uid)
		if err != nil || emb == nil {
			continue
		}

		neighbors, distances, err := embRepo.FindSimilarWithDistance(ctx, emb.Embedding, 20, req.Threshold)
		if err != nil {
			continue
		}

		for i, neighbor := range neighbors {
			if neighbor.PhotoUID == uid {
				continue
			}
			if !uidSet[neighbor.PhotoUID] {
				continue
			}
			union(uid, neighbor.PhotoUID)
			// Track distance
			rootUID := find(uid)
			pairDistances[rootUID] = append(pairDistances[rootUID], distances[i])
		}
	}

	// Extract groups
	groups := make(map[string][]string)
	for _, uid := range photoUIDs {
		root := find(uid)
		groups[root] = append(groups[root], uid)
	}

	// Build result groups (size >= 2 only)
	var resultGroups []DuplicateGroup
	totalDuplicates := 0

	for root, members := range groups {
		if len(members) < 2 {
			continue
		}

		// Compute average distance for group
		distances := pairDistances[root]
		avgDist := 0.0
		if len(distances) > 0 {
			sum := 0.0
			for _, d := range distances {
				sum += d
			}
			avgDist = sum / float64(len(distances))
		}

		photos := make([]DuplicatePhoto, len(members))
		for i, uid := range members {
			// Find best distance for this photo within the group
			bestDist := avgDist
			photos[i] = DuplicatePhoto{
				PhotoUID: uid,
				Distance: bestDist,
			}
		}

		resultGroups = append(resultGroups, DuplicateGroup{
			Photos:     photos,
			AvgDistance: avgDist,
			PhotoCount: len(members),
		})
		totalDuplicates += len(members)
	}

	// Sort by group size descending, then avg distance ascending
	sort.Slice(resultGroups, func(i, j int) bool {
		if resultGroups[i].PhotoCount != resultGroups[j].PhotoCount {
			return resultGroups[i].PhotoCount > resultGroups[j].PhotoCount
		}
		return resultGroups[i].AvgDistance < resultGroups[j].AvgDistance
	})

	// Apply limit
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

// SuggestAlbums finds photos that belong in existing albums but aren't there yet (album completion)
func (h *PhotosHandler) SuggestAlbums(w http.ResponseWriter, r *http.Request) {
	var req SuggestAlbumsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Threshold <= 0 {
		req.Threshold = constants.DefaultSuggestAlbumThreshold
	}
	if req.TopK <= 0 {
		req.TopK = constants.DefaultSuggestAlbumTopK
	}

	ctx := context.Background()

	embRepo := h.embeddingReader
	if embRepo == nil {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			respondError(w, http.StatusServiceUnavailable, "embeddings not available")
			return
		}
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Fetch all albums
	albums, err := pp.GetAlbums(constants.MaxPhotosPerFetch, 0, "", "", "album")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	// Filter albums with enough photos
	var candidateAlbums []photoprism.Album
	for _, album := range albums {
		if album.PhotoCount >= constants.MinAlbumPhotosForCentroid {
			candidateAlbums = append(candidateAlbums, album)
		}
	}

	// For each album: compute centroid, HNSW search, filter members
	type albumResult struct {
		suggestion AlbumSuggestion
		skipped    bool
	}

	var mu sync.Mutex
	var results []albumResult
	skipped := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit concurrency to 10

	maxDistance := 1.0 - req.Threshold // Convert similarity threshold to distance

	for _, album := range candidateAlbums {
		wg.Add(1)
		go func(a photoprism.Album) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Fetch album photo UIDs (paginated)
			albumMemberSet := make(map[string]bool)
			offset := 0
			pageSize := constants.DefaultHandlerPageSize
			for {
				photos, err := pp.GetAlbumPhotos(a.UID, pageSize, offset)
				if err != nil {
					return
				}
				for _, p := range photos {
					albumMemberSet[p.UID] = true
				}
				if len(photos) < pageSize {
					break
				}
				offset += pageSize
			}

			if len(albumMemberSet) < constants.MinAlbumPhotosForCentroid {
				return
			}

			// Get embeddings for album photos
			var embeddings [][]float32
			for uid := range albumMemberSet {
				emb, err := embRepo.Get(ctx, uid)
				if err != nil || emb == nil {
					continue
				}
				embeddings = append(embeddings, emb.Embedding)
			}

			if len(embeddings) < constants.MinAlbumPhotosForCentroid {
				mu.Lock()
				skipped++
				mu.Unlock()
				return
			}

			// Compute centroid
			centroid := computeCentroid(embeddings)
			if centroid == nil {
				mu.Lock()
				skipped++
				mu.Unlock()
				return
			}

			// HNSW search for similar photos (request extra to account for album members being filtered out)
			similar, distances, err := embRepo.FindSimilarWithDistance(ctx, centroid, req.TopK*3, maxDistance)
			if err != nil {
				return
			}

			// Filter out photos already in the album, convert to suggestions
			var photos []AlbumPhotoSuggestion
			for i, emb := range similar {
				if albumMemberSet[emb.PhotoUID] {
					continue
				}
				similarity := 1.0 - distances[i]
				photos = append(photos, AlbumPhotoSuggestion{
					PhotoUID:   emb.PhotoUID,
					Similarity: similarity,
				})
				if len(photos) >= req.TopK {
					break
				}
			}

			if len(photos) > 0 {
				mu.Lock()
				results = append(results, albumResult{
					suggestion: AlbumSuggestion{
						AlbumUID:   a.UID,
						AlbumTitle: a.Title,
						Photos:     photos,
					},
				})
				mu.Unlock()
			}
		}(album)
	}
	wg.Wait()

	// Sort suggestions by photo count descending
	sort.Slice(results, func(i, j int) bool {
		return len(results[i].suggestion.Photos) > len(results[j].suggestion.Photos)
	})

	suggestions := make([]AlbumSuggestion, 0, len(results))
	uniquePhotos := make(map[string]bool)
	for _, r := range results {
		suggestions = append(suggestions, r.suggestion)
		for _, p := range r.suggestion.Photos {
			uniquePhotos[p.PhotoUID] = true
		}
	}

	respondJSON(w, http.StatusOK, SuggestAlbumsResponse{
		AlbumsAnalyzed: len(candidateAlbums) - skipped,
		PhotosAnalyzed: len(uniquePhotos),
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

// cosineSimilarity computes the cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
