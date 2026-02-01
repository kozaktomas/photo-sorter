package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

const statsCacheTTL = 10 * time.Minute

// statsCache holds cached stats with expiry
type statsCache struct {
	mu        sync.RWMutex
	data      *StatsResponse
	expiresAt time.Time
}

func (c *statsCache) get() (*StatsResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data == nil || time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.data, true
}

func (c *statsCache) set(data *StatsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.expiresAt = time.Now().Add(statsCacheTTL)
}

// StatsHandler handles statistics endpoints
type StatsHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	cache          statsCache
}

// NewStatsHandler creates a new stats handler
func NewStatsHandler(cfg *config.Config, sm *middleware.SessionManager) *StatsHandler {
	return &StatsHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// StatsResponse represents the statistics response
type StatsResponse struct {
	TotalPhotos      int `json:"total_photos"`
	PhotosProcessed  int `json:"photos_processed"`
	PhotosWithEmbed  int `json:"photos_with_embeddings"`
	PhotosWithFaces  int `json:"photos_with_faces"`
	TotalFaces       int `json:"total_faces"`
	TotalEmbeddings  int `json:"total_embeddings"`
}

// Get returns statistics about photos and embeddings
func (h *StatsHandler) Get(w http.ResponseWriter, r *http.Request) {
	// Check cache first
	if cached, ok := h.cache.get(); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	ctx := context.Background()

	// Get total photos from PhotoPrism
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Fetch all photos from PhotoPrism
	var allPhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0
	for {
		photos, err := pp.GetPhotos(pageSize, offset)
		if err != nil {
			break
		}
		allPhotos = append(allPhotos, photos...)
		if len(photos) < pageSize {
			break
		}
		offset += pageSize
	}
	totalPhotos := len(allPhotos)

	// Get embedding and face stats from PostgreSQL
	var photosProcessed, photosWithEmbed, photosWithFaces, totalFaces, totalEmbeddings int

	if embRepo, err := database.GetEmbeddingReader(ctx); err == nil {
		if count, err := embRepo.Count(ctx); err == nil {
			totalEmbeddings = count
			photosWithEmbed = count
		}
	}
	if faceRepo, err := database.GetFaceReader(ctx); err == nil {
		if count, err := faceRepo.Count(ctx); err == nil {
			totalFaces = count
		}
		if count, err := faceRepo.CountPhotos(ctx); err == nil {
			photosWithFaces = count
		}
	}

	// Count processed photos: a photo is "processed" if it has an embedding or faces
	// For PostgreSQL, we use the counts from the database
	photosProcessed = photosWithEmbed
	if photosWithFaces > photosProcessed {
		photosProcessed = photosWithFaces
	}

	stats := &StatsResponse{
		TotalPhotos:     totalPhotos,
		PhotosProcessed: photosProcessed,
		PhotosWithEmbed: photosWithEmbed,
		PhotosWithFaces: photosWithFaces,
		TotalFaces:      totalFaces,
		TotalEmbeddings: totalEmbeddings,
	}

	// Cache the result
	h.cache.set(stats)

	respondJSON(w, http.StatusOK, stats)
}
