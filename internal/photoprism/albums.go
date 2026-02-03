package photoprism

import (
	"fmt"
	"net/http"
)

// GetAlbum retrieves a single album by UID
func (pp *PhotoPrism) GetAlbum(albumUID string) (*Album, error) {
	return doGetJSON[Album](pp, fmt.Sprintf("albums/%s", albumUID))
}

// GetAlbums retrieves albums from PhotoPrism
// albumType can be: "album" (manual albums), "folder", "moment", "month", "state", or "" for all
func (pp *PhotoPrism) GetAlbums(count int, offset int, order string, query string, albumType string) ([]Album, error) {
	endpoint := fmt.Sprintf("albums?count=%d&offset=%d", count, offset)
	if albumType != "" {
		endpoint += fmt.Sprintf("&type=%s", albumType)
	}
	if order != "" {
		endpoint += fmt.Sprintf("&order=%s", order)
	}
	if query != "" {
		endpoint += fmt.Sprintf("&q=%s", query)
	}

	result, err := doGetJSON[[]Album](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// CreateAlbum creates a new album with the given title
func (pp *PhotoPrism) CreateAlbum(title string) (*Album, error) {
	input := struct {
		Title string `json:"Title"`
	}{
		Title: title,
	}

	return doPostJSON[Album](pp, "albums", input)
}

// AddPhotosToAlbum adds photos to an album
func (pp *PhotoPrism) AddPhotosToAlbum(albumUID string, photoUIDs []string) error {
	if len(photoUIDs) == 0 {
		return nil
	}

	selection := struct {
		Photos []string `json:"photos"`
	}{
		Photos: photoUIDs,
	}

	_, err := doRequestRaw(pp, "POST", fmt.Sprintf("albums/%s/photos", albumUID), selection, http.StatusOK)
	return err
}

// RemovePhotosFromAlbum removes photos from an album (keeps them in library)
func (pp *PhotoPrism) RemovePhotosFromAlbum(albumUID string, photoUIDs []string) error {
	if len(photoUIDs) == 0 {
		return nil
	}

	selection := struct {
		Photos []string `json:"photos"`
	}{
		Photos: photoUIDs,
	}

	_, err := doRequestRaw(pp, "DELETE", fmt.Sprintf("albums/%s/photos", albumUID), selection, http.StatusOK)
	return err
}

// GetAlbumPhotos retrieves photos from a specific album
func (pp *PhotoPrism) GetAlbumPhotos(albumUID string, count int, offset int) ([]Photo, error) {
	endpoint := fmt.Sprintf("photos?count=%d&offset=%d&s=%s", count, offset, albumUID)
	result, err := doGetJSON[[]Photo](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}
