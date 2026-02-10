package photoprism

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// GetPhotos retrieves all photos from PhotoPrism
// Optional quality parameter sets minimum quality score (1-7). PhotoPrism UI defaults to 3.
func (pp *PhotoPrism) GetPhotos(count int, offset int, quality ...int) ([]Photo, error) {
	return pp.GetPhotosWithQuery(count, offset, "", quality...)
}

// GetPhotosWithQuery retrieves photos from PhotoPrism with an optional search query
// Query examples: "person:jan-novak", "label:cat", "year:2024"
// Optional quality parameter sets minimum quality score (1-7). PhotoPrism UI defaults to 3.
func (pp *PhotoPrism) GetPhotosWithQuery(count int, offset int, query string, quality ...int) ([]Photo, error) {
	return pp.GetPhotosWithQueryAndOrder(count, offset, query, "", quality...)
}

// GetPhotosWithQueryAndOrder retrieves photos from PhotoPrism with optional search query and ordering
// Query examples: "person:jan-novak", "label:cat", "year:2024"
// Order examples: "newest", "oldest", "added", "edited", "name", "title", "size", "random"
// Optional quality parameter sets minimum quality score (1-7). PhotoPrism UI defaults to 3.
func (pp *PhotoPrism) GetPhotosWithQueryAndOrder(count int, offset int, query string, order string, quality ...int) ([]Photo, error) {
	endpoint := fmt.Sprintf("photos?count=%d&offset=%d", count, offset)
	if query != "" {
		endpoint += "&q=" + url.QueryEscape(query)
	}
	if order != "" {
		endpoint += "&order=" + url.QueryEscape(order)
	}
	if len(quality) > 0 && quality[0] > 0 {
		endpoint += fmt.Sprintf("&quality=%d", quality[0])
	}

	result, err := doGetJSON[[]Photo](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// EditPhoto updates photo metadata
func (pp *PhotoPrism) EditPhoto(photoUID string, updates PhotoUpdate) (*Photo, error) {
	return doPutJSON[Photo](pp, "photos/"+photoUID, updates)
}

// GetPhotoDetails retrieves full photo details including all metadata
func (pp *PhotoPrism) GetPhotoDetails(photoUID string) (map[string]any, error) {
	result, err := doGetJSON[map[string]any](pp, "photos/"+photoUID)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// IsPhotoDeleted checks if a photo details response indicates the photo has been soft-deleted.
// PhotoPrism sets DeletedAt to a non-empty timestamp string when a photo is archived/deleted.
func IsPhotoDeleted(details map[string]any) bool {
	deletedAt, ok := details["DeletedAt"]
	if !ok {
		return false
	}
	// DeletedAt is a string timestamp; empty or missing means not deleted
	if str, ok := deletedAt.(string); ok && str != "" {
		return true
	}
	return false
}

// GetPhotoDownload downloads the primary file content for a photo
// Returns the image data as bytes and the content type
//
// This function first retrieves the photo details to get the file hash,
// then downloads the file using the /dl/{hash} endpoint with the download token.
//
// Example usage:
//
//	data, contentType, err := pp.GetPhotoDownload("photo-uid-here")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Save to file
//	err = os.WriteFile("photo.jpg", data, 0644)
//
// findPrimaryFile finds the primary file map from the Files array in photo details.
func findPrimaryFile(files []any) map[string]any {
	for _, f := range files {
		file, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if mapBool(file, "Primary") {
			return file
		}
	}
	if first, ok := files[0].(map[string]any); ok {
		return first
	}
	return nil
}

// findPrimaryFileHash extracts the hash of the primary file from photo details.
func findPrimaryFileHash(details map[string]any) string {
	files, ok := details["Files"].([]any)
	if !ok || len(files) == 0 {
		return ""
	}
	primaryFile := findPrimaryFile(files)
	if primaryFile == nil {
		return ""
	}
	return mapString(primaryFile, "Hash")
}

func (pp *PhotoPrism) GetPhotoDownload(photoUID string) ([]byte, string, error) {
	// Get photo details to retrieve the file hash
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, "", fmt.Errorf("could not get photo details: %w", err)
	}

	// Extract the PRIMARY file hash (not just files[0])
	// Face detection coordinates are calculated relative to the primary file,
	// so we must download the same file to ensure coordinates match.
	fileHash := findPrimaryFileHash(details)
	if fileHash == "" {
		return nil, "", errors.New("could not find file hash for photo")
	}

	// Download using the file hash
	return pp.GetFileDownload(fileHash)
}

// GetPhotoThumbnail downloads a thumbnail for a photo
// size can be one of: tile_50, tile_100, left_224, right_224, tile_224, tile_500,
// fit_720, tile_1080, fit_1280, fit_1600, fit_1920, fit_2048, fit_2560, fit_3840, fit_4096, fit_7680
//
// Example usage:
//
//	// Get the hash from a Photo object's Hash field
//	data, contentType, err := pp.GetPhotoThumbnail(photo.Hash, "fit_1280")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Save thumbnail to file
//	err = os.WriteFile("thumbnail.jpg", data, 0644)
func (pp *PhotoPrism) GetPhotoThumbnail(thumbHash string, size string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/t/%s/%s/%s", pp.Url, thumbHash, pp.downloadToken, size)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("could not read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// GetFileDownload downloads a file using its hash via the /api/v1/dl/{hash} endpoint
// This endpoint may work differently than the photo download endpoint
func (pp *PhotoPrism) GetFileDownload(fileHash string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/dl/%s?t=%s", pp.Url, fileHash, pp.downloadToken)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not create request: %w", err)
	}

	// Try without Authorization header first, as this endpoint might use token in URL
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("could not read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// ArchivePhotos archives (soft-deletes) multiple photos by their UIDs
func (pp *PhotoPrism) ArchivePhotos(photoUIDs []string) error {
	if len(photoUIDs) == 0 {
		return nil
	}

	selection := struct {
		Photos []string `json:"photos"`
	}{
		Photos: photoUIDs,
	}

	return doRequestRaw(pp, http.MethodPost, "batch/photos/archive", selection)
}

// ApprovePhoto marks a photo in review as approved, allowing it to be downloaded
func (pp *PhotoPrism) ApprovePhoto(photoUID string) (*Photo, error) {
	return doPostJSON[Photo](pp, fmt.Sprintf("photos/%s/approve", photoUID), nil)
}

// GetPhotoFileUID extracts the primary file UID from photo details
func (pp *PhotoPrism) GetPhotoFileUID(photoUID string) (string, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return "", err
	}

	if files, ok := details["Files"].([]any); ok && len(files) > 0 {
		if file, ok := files[0].(map[string]any); ok {
			if uid, ok := file["UID"].(string); ok {
				return uid, nil
			}
		}
	}

	return "", fmt.Errorf("could not find file UID for photo %s", photoUID)
}
