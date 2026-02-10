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

func (c *statsCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = nil
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

// InvalidateCache clears the cached stats so the next request fetches fresh data
func (h *StatsHandler) InvalidateCache() {
	h.cache.invalidate()
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

// fetchAllPhotoUIDs fetches all photo UIDs from PhotoPrism with pagination
func fetchAllPhotoUIDs(pp *photoprism.PhotoPrism) []string {
	var allPhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0
	for {
		photos, err := pp.GetPhotos(pageSize, offset, constants.DefaultPhotoQuality)
		if err != nil {
			break
		}
		allPhotos = append(allPhotos, photos...)
		if len(photos) < pageSize {
			break
		}
		offset += pageSize
	}
	photoUIDs := make([]string, len(allPhotos))
	for i, p := range allPhotos {
		photoUIDs[i] = p.UID
	}
	return photoUIDs
}

// fetchDBStats retrieves embedding and face statistics from PostgreSQL
func fetchDBStats(ctx context.Context, photoUIDs []string) (photosWithEmbed, totalEmbeddings, photosWithFaces, totalFaces int) {
	if embRepo, err := database.GetEmbeddingReader(ctx); err == nil {
		if count, err := embRepo.CountByUIDs(ctx, photoUIDs); err == nil {
			totalEmbeddings = count
			photosWithEmbed = count
		}
	}
	if faceRepo, err := database.GetFaceReader(ctx); err == nil {
		if count, err := faceRepo.CountByUIDs(ctx, photoUIDs); err == nil {
			totalFaces = count
		}
		if count, err := faceRepo.CountPhotosByUIDs(ctx, photoUIDs); err == nil {
			photosWithFaces = count
		}
	}
	return
}

// Get returns statistics about photos and embeddings
func (h *StatsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if cached, ok := h.cache.get(); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	photoUIDs := fetchAllPhotoUIDs(pp)
	photosWithEmbed, totalEmbeddings, photosWithFaces, totalFaces := fetchDBStats(context.Background(), photoUIDs)

	photosProcessed := max(photosWithEmbed, photosWithFaces)

	stats := &StatsResponse{
		TotalPhotos: len(photoUIDs), PhotosProcessed: photosProcessed,
		PhotosWithEmbed: photosWithEmbed, PhotosWithFaces: photosWithFaces,
		TotalFaces: totalFaces, TotalEmbeddings: totalEmbeddings,
	}

	h.cache.set(stats)
	respondJSON(w, http.StatusOK, stats)
}
