package photoprism

import "fmt"

// GetAlbum retrieves a single album by UID
func (pp *PhotoPrism) GetAlbum(albumUID string) (*Album, error) {
	return doGetJSON[Album](pp, "albums/"+albumUID)
}

// GetAlbums retrieves albums from PhotoPrism
// albumType can be: "album" (manual albums), "folder", "moment", "month", "state", or "" for all
func (pp *PhotoPrism) GetAlbums(count int, offset int, order string, query string, albumType string) ([]Album, error) {
	endpoint := fmt.Sprintf("albums?count=%d&offset=%d", count, offset)
	if albumType != "" {
		endpoint += "&type=" + albumType
	}
	if order != "" {
		endpoint += "&order=" + order
	}
	if query != "" {
		endpoint += "&q=" + query
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

	return doRequestRaw(pp, "POST", fmt.Sprintf("albums/%s/photos", albumUID), selection)
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

	return doRequestRaw(pp, "DELETE", fmt.Sprintf("albums/%s/photos", albumUID), selection)
}

// GetAlbumPhotos retrieves photos from a specific album
// Optional quality parameter sets minimum quality score (1-7). PhotoPrism UI defaults to 3.
func (pp *PhotoPrism) GetAlbumPhotos(albumUID string, count int, offset int, quality ...int) ([]Photo, error) {
	endpoint := fmt.Sprintf("photos?count=%d&offset=%d&s=%s", count, offset, albumUID)
	if len(quality) > 0 && quality[0] > 0 {
		endpoint += fmt.Sprintf("&quality=%d", quality[0])
	}
	result, err := doGetJSON[[]Photo](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}
