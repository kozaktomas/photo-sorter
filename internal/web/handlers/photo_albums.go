package handlers

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AlbumMembershipResponse represents an album that contains a given photo.
type AlbumMembershipResponse struct {
	UID        string `json:"uid"`
	Title      string `json:"title"`
	PhotoCount int    `json:"photo_count"`
}

// GetPhotoAlbums handles GET /api/v1/photos/:uid/albums. It returns the
// list of albums that contain the given photo using a single indexed
// lookup against the native album_photos table.
func (h *AlbumsHandler) GetPhotoAlbums(w http.ResponseWriter, r *http.Request) {
	photoUID := chi.URLParam(r, "uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "missing photo UID")
		return
	}
	repo := h.requireAlbumWriter(w)
	if repo == nil {
		return
	}

	albums, err := repo.ListAlbumsForPhoto(r.Context(), photoUID)
	if err != nil {
		log.Printf("photo albums %s: %v", sanitizeForLog(photoUID), err)
		respondError(w, http.StatusInternalServerError, "failed to get albums")
		return
	}

	result := make([]AlbumMembershipResponse, 0, len(albums))
	for _, a := range albums {
		result = append(result, AlbumMembershipResponse{
			UID:        a.UID,
			Title:      a.Title,
			PhotoCount: a.PhotoCount,
		})
	}
	respondJSON(w, http.StatusOK, result)
}
