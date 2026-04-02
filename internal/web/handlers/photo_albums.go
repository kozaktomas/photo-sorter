package handlers

import (
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// AlbumMembershipResponse represents an album that contains a given photo.
type AlbumMembershipResponse struct {
	UID        string `json:"uid"`
	Title      string `json:"title"`
	PhotoCount int    `json:"photo_count"`
}

// GetPhotoAlbums handles GET /api/v1/photos/:uid/albums.
// Returns the list of albums that contain the given photo.
func (h *AlbumsHandler) GetPhotoAlbums(w http.ResponseWriter, r *http.Request) {
	photoUID := chi.URLParam(r, "uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	albums, err := pp.GetAlbums(500, 0, "", "", "album")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	// Check each album concurrently.
	type hit struct {
		album photoprism.Album
		found bool
	}
	hits := make([]hit, len(albums))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // limit concurrency
	for i, a := range albums {
		wg.Add(1)
		go func(idx int, album photoprism.Album) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			found, err := pp.IsPhotoInAlbum(photoUID, album.UID)
			if err == nil && found {
				hits[idx] = hit{album: album, found: true}
			}
		}(i, a)
	}
	wg.Wait()

	result := make([]AlbumMembershipResponse, 0)
	for _, h := range hits {
		if h.found {
			result = append(result, AlbumMembershipResponse{
				UID:        h.album.UID,
				Title:      h.album.Title,
				PhotoCount: h.album.PhotoCount,
			})
		}
	}

	respondJSON(w, http.StatusOK, result)
}
