package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

	photos, err := pp.GetPhotosWithQueryAndOrder(count, offset, finalQuery, order)
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
