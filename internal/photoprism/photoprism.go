package photoprism

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PhotoPrism struct {
	Url           string
	token         string
	downloadToken string
	userUID       string
	captureDir    string
}

// SetCaptureDir enables API response capturing to the specified directory.
// Pass an empty string to disable capturing.
func (pp *PhotoPrism) SetCaptureDir(dir string) error {
	if dir == "" {
		pp.captureDir = ""
		return nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("could not create capture directory: %w", err)
	}
	pp.captureDir = dir
	return nil
}

// captureResponse saves the API response body to a file if capturing is enabled.
// The filename is generated from the endpoint name and optional suffix.
func (pp *PhotoPrism) captureResponse(endpoint string, body []byte) {
	if pp.captureDir == "" {
		return
	}

	// Sanitize endpoint for filename
	filename := strings.ReplaceAll(endpoint, "/", "_")
	filename = strings.TrimPrefix(filename, "_")
	timestamp := time.Now().Format("20060102_150405")
	filename = fmt.Sprintf("%s_%s.json", filename, timestamp)

	filepath := filepath.Join(pp.captureDir, filename)

	// Pretty-print JSON if possible
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err == nil {
		body = prettyJSON.Bytes()
	}

	_ = os.WriteFile(filepath, body, 0644)
}

func NewPhotoPrism(url, username, password string) (*PhotoPrism, error) {
	return NewPhotoPrismWithCapture(url, username, password, "")
}

// NewPhotoPrismWithCapture creates a new PhotoPrism client with optional response capturing.
// Pass an empty captureDir to disable capturing.
func NewPhotoPrismWithCapture(url, username, password, captureDir string) (*PhotoPrism, error) {
	pp := &PhotoPrism{Url: url + "/api/v1"}
	if captureDir != "" {
		if err := pp.SetCaptureDir(captureDir); err != nil {
			return nil, err
		}
	}
	if err := pp.auth(username, password); err != nil {
		return nil, fmt.Errorf("could not authenticate: %w", err)
	}

	return pp, nil
}

func NewPhotoPrismFromToken(url, token, downloadToken string) (*PhotoPrism, error) {
	return &PhotoPrism{Url: url + "/api/v1", token: token, downloadToken: downloadToken}, nil
}

func (pp *PhotoPrism) auth(username, password string) error {
	input := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: username,
		Password: password,
	}

	inputBody, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("could not marshal input: %w", err)
	}

	req, err := http.NewRequest("POST", pp.Url+"/sessions", bytes.NewReader(inputBody))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse("sessions", body)

	var result struct {
		ID          string `json:"id"`
		AccessToken string `json:"access_token"`
		Config      struct {
			DownloadToken string `json:"downloadToken"`
			PreviewToken  string `json:"previewToken"`
		} `json:"config"`
		User struct {
			UID string `json:"UID"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("could not unmarshal response: %w", err)
	}

	pp.token = result.AccessToken
	pp.downloadToken = result.Config.DownloadToken
	pp.userUID = result.User.UID

	return nil
}

// Logout deletes the current session (logout)
func (pp *PhotoPrism) Logout() error {
	if pp.token == "" {
		return nil // Already logged out
	}

	req, err := http.NewRequest("DELETE", pp.Url+"/session", nil)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("logout failed with status %d: %s", resp.StatusCode, string(body))
	}

	pp.token = ""
	pp.downloadToken = ""

	return nil
}

// Album represents a PhotoPrism album
type Album struct {
	UID         string `json:"UID"`
	Title       string `json:"Title"`
	Description string `json:"Description"`
	Favorite    bool   `json:"Favorite"`
	PhotoCount  int    `json:"PhotoCount"`
	Thumb       string `json:"Thumb"`
	Type        string `json:"Type"`
	CreatedAt   string `json:"CreatedAt"`
	UpdatedAt   string `json:"UpdatedAt"`
}

// Label represents a PhotoPrism label/tag
type Label struct {
	UID         string `json:"UID"`
	Name        string `json:"Name"`
	Slug        string `json:"Slug"`
	Description string `json:"Description"`
	Notes       string `json:"Notes"`
	PhotoCount  int    `json:"PhotoCount"`
	Favorite    bool   `json:"Favorite"`
	Priority    int    `json:"Priority"`
	CreatedAt   string `json:"CreatedAt"`
}

// Photo represents a PhotoPrism photo
type Photo struct {
	UID          string  `json:"UID"`
	Title        string  `json:"Title"`
	Description  string  `json:"Description"`
	TakenAt      string  `json:"TakenAt"`
	TakenAtLocal string  `json:"TakenAtLocal"`
	Favorite     bool    `json:"Favorite"`
	Private      bool    `json:"Private"`
	Type         string  `json:"Type"`
	Lat          float64 `json:"Lat"`
	Lng          float64 `json:"Lng"`
	Caption      string  `json:"Caption"`
	Year         int     `json:"Year"`
	Month        int     `json:"Month"`
	Day          int     `json:"Day"`
	Country      string  `json:"Country"`
	Hash         string  `json:"Hash"`
	Width        int     `json:"Width"`
	Height       int     `json:"Height"`
	OriginalName string  `json:"OriginalName"` // Original filename when uploaded
	FileName     string  `json:"FileName"`     // Current filename
	Name         string  `json:"Name"`         // Internal name
	Path         string  `json:"Path"`         // File path
	CameraModel  string  `json:"CameraModel"`  // Camera model name
	Scan         bool    `json:"Scan"`         // True if photo was scanned
}

// PhotoDetails represents additional photo details like notes
type PhotoDetails struct {
	Notes *string `json:"Notes,omitempty"`
}

// PhotoUpdate represents fields that can be updated on a photo
type PhotoUpdate struct {
	Title          *string       `json:"Title,omitempty"`
	Description    *string       `json:"Description,omitempty"`
	DescriptionSrc *string       `json:"DescriptionSrc,omitempty"`
	TakenAt        *string       `json:"TakenAt,omitempty"`
	TakenAtLocal   *string       `json:"TakenAtLocal,omitempty"`
	Favorite       *bool         `json:"Favorite,omitempty"`
	Private        *bool         `json:"Private,omitempty"`
	Lat            *float64      `json:"Lat,omitempty"`
	Lng            *float64      `json:"Lng,omitempty"`
	Caption        *string       `json:"Caption,omitempty"`
	CaptionSrc     *string       `json:"CaptionSrc,omitempty"`
	Year           *int          `json:"Year,omitempty"`
	Month          *int          `json:"Month,omitempty"`
	Day            *int          `json:"Day,omitempty"`
	Country        *string       `json:"Country,omitempty"`
	Altitude       *int          `json:"Altitude,omitempty"`
	TimeZone       *string       `json:"TimeZone,omitempty"`
	Details        *PhotoDetails `json:"Details,omitempty"`
}

// PhotoLabel represents a label/tag that can be added to a photo
type PhotoLabel struct {
	Name        string `json:"Name"`
	LabelSrc    string `json:"LabelSrc,omitempty"`
	Description string `json:"Description,omitempty"`
	Favorite    bool   `json:"Favorite,omitempty"`
	Notes       string `json:"Notes,omitempty"`
	Priority    int    `json:"Priority,omitempty"`
	Uncertainty int    `json:"Uncertainty,omitempty"`
}

// GetAlbum retrieves a single album by UID
func (pp *PhotoPrism) GetAlbum(albumUID string) (*Album, error) {
	url := fmt.Sprintf("%s/albums/%s", pp.Url, albumUID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("albums/%s", albumUID), body)

	var album Album
	if err := json.Unmarshal(body, &album); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &album, nil
}

// GetAlbums retrieves albums from PhotoPrism
func (pp *PhotoPrism) GetAlbums(count int, offset int, order string, query string) ([]Album, error) {
	url := fmt.Sprintf("%s/albums?count=%d&offset=%d", pp.Url, count, offset)
	if order != "" {
		url += fmt.Sprintf("&order=%s", order)
	}
	if query != "" {
		url += fmt.Sprintf("&q=%s", query)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse("albums", body)

	var albums []Album
	if err := json.Unmarshal(body, &albums); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return albums, nil
}

// GetLabels retrieves labels from PhotoPrism
func (pp *PhotoPrism) GetLabels(count int, offset int, all bool) ([]Label, error) {
	url := fmt.Sprintf("%s/labels?count=%d&offset=%d", pp.Url, count, offset)
	if all {
		url += "&all=true"
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse("labels", body)

	var labels []Label
	if err := json.Unmarshal(body, &labels); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return labels, nil
}

// GetPhotos retrieves all photos from PhotoPrism
func (pp *PhotoPrism) GetPhotos(count int, offset int) ([]Photo, error) {
	return pp.GetPhotosWithQuery(count, offset, "")
}

// GetPhotosWithQuery retrieves photos from PhotoPrism with an optional search query
// Query examples: "person:tomas-kozak", "label:cat", "year:2024"
func (pp *PhotoPrism) GetPhotosWithQuery(count int, offset int, query string) ([]Photo, error) {
	url := fmt.Sprintf("%s/photos?count=%d&offset=%d", pp.Url, count, offset)
	if query != "" {
		url += fmt.Sprintf("&q=%s", query)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_offset_%d", offset), body)

	var photos []Photo
	if err := json.Unmarshal(body, &photos); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return photos, nil
}

// GetAlbumPhotos retrieves photos from a specific album
func (pp *PhotoPrism) GetAlbumPhotos(albumUID string, count int, offset int) ([]Photo, error) {
	url := fmt.Sprintf("%s/photos?count=%d&offset=%d&s=%s", pp.Url, count, offset, albumUID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_album_%s_offset_%d", albumUID, offset), body)

	var photos []Photo
	if err := json.Unmarshal(body, &photos); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return photos, nil
}

// EditPhoto updates photo metadata
func (pp *PhotoPrism) EditPhoto(photoUID string, updates PhotoUpdate) (*Photo, error) {
	updateBody, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("could not marshal updates: %w", err)
	}

	url := fmt.Sprintf("%s/photos/%s", pp.Url, photoUID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(updateBody))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_edit", photoUID), body)

	var photo Photo
	if err := json.Unmarshal(body, &photo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &photo, nil
}

// AddPhotoLabel adds a label/tag to a photo
func (pp *PhotoPrism) AddPhotoLabel(photoUID string, label PhotoLabel) (*Photo, error) {
	labelBody, err := json.Marshal(label)
	if err != nil {
		return nil, fmt.Errorf("could not marshal label: %w", err)
	}

	url := fmt.Sprintf("%s/photos/%s/label", pp.Url, photoUID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(labelBody))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_label_add", photoUID), body)

	var photo Photo
	if err := json.Unmarshal(body, &photo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &photo, nil
}

// RemovePhotoLabel removes a label/tag from a photo
func (pp *PhotoPrism) RemovePhotoLabel(photoUID string, labelID string) (*Photo, error) {
	url := fmt.Sprintf("%s/photos/%s/label/%s", pp.Url, photoUID, labelID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_label_%s_remove", photoUID, labelID), body)

	var photo Photo
	if err := json.Unmarshal(body, &photo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &photo, nil
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
func (pp *PhotoPrism) GetPhotoDownload(photoUID string) ([]byte, string, error) {
	// Get photo details to retrieve the file hash
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, "", fmt.Errorf("could not get photo details: %w", err)
	}

	// Extract the primary file hash
	var fileHash string
	if files, ok := details["Files"].([]interface{}); ok && len(files) > 0 {
		if file, ok := files[0].(map[string]interface{}); ok {
			if hash, ok := file["Hash"].(string); ok {
				fileHash = hash
			}
		}
	}

	if fileHash == "" {
		return nil, "", fmt.Errorf("could not find file hash for photo")
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
	url := fmt.Sprintf("%s/t/%s/%s/%s", pp.Url, thumbHash, pp.token, size)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("could not read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// UpdatePhotoLabel updates a label/tag on a photo (mainly used to change uncertainty)
func (pp *PhotoPrism) UpdatePhotoLabel(photoUID string, labelID string, label PhotoLabel) (*Photo, error) {
	labelBody, err := json.Marshal(label)
	if err != nil {
		return nil, fmt.Errorf("could not marshal label: %w", err)
	}

	url := fmt.Sprintf("%s/photos/%s/label/%s", pp.Url, photoUID, labelID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(labelBody))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_label_%s_update", photoUID, labelID), body)

	var photo Photo
	if err := json.Unmarshal(body, &photo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &photo, nil
}

// GetFileDownload downloads a file using its hash via the /api/v1/dl/{hash} endpoint
// This endpoint may work differently than the photo download endpoint
func (pp *PhotoPrism) GetFileDownload(fileHash string) ([]byte, string, error) {
	url := fmt.Sprintf("%s/dl/%s?t=%s", pp.Url, fileHash, pp.downloadToken)
	req, err := http.NewRequest("GET", url, nil)
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
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("could not read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// GetPhotoDetails retrieves full photo details including all metadata
func (pp *PhotoPrism) GetPhotoDetails(photoUID string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/photos/%s", pp.Url, photoUID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_details", photoUID), body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return result, nil
}

// RemoveAllPhotoLabels removes all labels/tags from a photo
// It first retrieves the photo details to get all label IDs, then removes each one
func (pp *PhotoPrism) RemoveAllPhotoLabels(photoUID string) error {
	// Get photo details to retrieve all labels
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return fmt.Errorf("could not get photo details: %w", err)
	}

	// Extract label IDs from the photo details
	var labelIDs []string
	if labels, ok := details["Labels"].([]interface{}); ok {
		for _, labelInterface := range labels {
			if label, ok := labelInterface.(map[string]interface{}); ok {
				// Label ID could be in different fields, try common ones
				if id, ok := label["LabelID"].(float64); ok {
					labelIDs = append(labelIDs, fmt.Sprintf("%.0f", id))
				} else if id, ok := label["ID"].(float64); ok {
					labelIDs = append(labelIDs, fmt.Sprintf("%.0f", id))
				} else if id, ok := label["LabelID"].(string); ok {
					labelIDs = append(labelIDs, id)
				} else if id, ok := label["ID"].(string); ok {
					labelIDs = append(labelIDs, id)
				}
			}
		}
	}

	// Remove each label
	for _, labelID := range labelIDs {
		_, err := pp.RemovePhotoLabel(photoUID, labelID)
		if err != nil {
			return fmt.Errorf("could not remove label %s: %w", labelID, err)
		}
	}

	return nil
}

// DeleteLabels deletes multiple labels by their UIDs
func (pp *PhotoPrism) DeleteLabels(labelUIDs []string) error {
	if len(labelUIDs) == 0 {
		return nil
	}

	selection := struct {
		Labels []string `json:"labels"`
	}{
		Labels: labelUIDs,
	}

	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("could not marshal selection: %w", err)
	}

	url := fmt.Sprintf("%s/batch/labels/delete", pp.Url)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ApprovePhoto marks a photo in review as approved, allowing it to be downloaded
func (pp *PhotoPrism) ApprovePhoto(photoUID string) (*Photo, error) {
	url := fmt.Sprintf("%s/photos/%s/approve", pp.Url, photoUID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("photos_%s_approve", photoUID), body)

	var photo Photo
	if err := json.Unmarshal(body, &photo); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &photo, nil
}

// UploadFile uploads a single file to the user's upload folder
// Returns the upload token used for processing
func (pp *PhotoPrism) UploadFile(filePath string) (string, error) {
	if pp.userUID == "" {
		return "", fmt.Errorf("user UID not available")
	}

	// Generate upload token (use current timestamp)
	uploadToken := fmt.Sprintf("%d", time.Now().UnixNano())

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file to form
	fileName := filepath.Base(filePath)
	part, err := writer.CreateFormFile("files", fileName)
	if err != nil {
		return "", fmt.Errorf("could not create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("could not copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("could not close writer: %w", err)
	}

	// Send request
	url := fmt.Sprintf("%s/users/%s/upload/%s", pp.Url, pp.userUID, uploadToken)
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return uploadToken, nil
}

// UploadFiles uploads multiple files to the user's upload folder
// Returns the upload token used for processing
func (pp *PhotoPrism) UploadFiles(filePaths []string) (string, error) {
	if pp.userUID == "" {
		return "", fmt.Errorf("user UID not available")
	}

	if len(filePaths) == 0 {
		return "", fmt.Errorf("no files to upload")
	}

	// Generate upload token (use current timestamp)
	uploadToken := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add all files to form
	for _, filePath := range filePaths {
		file, err := os.Open(filePath)
		if err != nil {
			return "", fmt.Errorf("could not open file %s: %w", filePath, err)
		}

		fileName := filepath.Base(filePath)
		part, err := writer.CreateFormFile("files", fileName)
		if err != nil {
			file.Close()
			return "", fmt.Errorf("could not create form file: %w", err)
		}

		if _, err := io.Copy(part, file); err != nil {
			file.Close()
			return "", fmt.Errorf("could not copy file data: %w", err)
		}

		file.Close()
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("could not close writer: %w", err)
	}

	// Send request
	url := fmt.Sprintf("%s/users/%s/upload/%s", pp.Url, pp.userUID, uploadToken)
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return uploadToken, nil
}

// ProcessUpload processes previously uploaded files and optionally adds them to albums
func (pp *PhotoPrism) ProcessUpload(uploadToken string, albumUIDs []string) error {
	if pp.userUID == "" {
		return fmt.Errorf("user UID not available")
	}

	options := struct {
		Albums []string `json:"albums,omitempty"`
	}{
		Albums: albumUIDs,
	}

	body, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("could not marshal options: %w", err)
	}

	url := fmt.Sprintf("%s/users/%s/upload/%s", pp.Url, pp.userUID, uploadToken)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("process upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateAlbum creates a new album with the given title
func (pp *PhotoPrism) CreateAlbum(title string) (*Album, error) {
	input := struct {
		Title string `json:"Title"`
	}{
		Title: title,
	}

	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("could not marshal input: %w", err)
	}

	req, err := http.NewRequest("POST", pp.Url+"/albums", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse("albums_create", respBody)

	var album Album
	if err := json.Unmarshal(respBody, &album); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &album, nil
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

	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("could not marshal selection: %w", err)
	}

	url := fmt.Sprintf("%s/albums/%s/photos", pp.Url, albumUID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Face represents a PhotoPrism face (face cluster with marker info)
type Face struct {
	ID              string  `json:"ID"`
	MarkerUID       string  `json:"MarkerUID"`
	FileUID         string  `json:"FileUID"`
	SubjUID         string  `json:"SubjUID"`
	Name            string  `json:"Name"`
	Src             string  `json:"Src"`
	SubjSrc         string  `json:"SubjSrc"`
	Hidden          bool    `json:"Hidden"`
	Size            int     `json:"Size"`
	Score           int     `json:"Score"`
	FaceDist        float64 `json:"FaceDist"`
	Samples         int     `json:"Samples"`
	SampleRadius    float64 `json:"SampleRadius"`
	Collisions      int     `json:"Collisions"`
	CollisionRadius float64 `json:"CollisionRadius"`
}

// Marker represents a face/subject region marker on a photo
type Marker struct {
	UID      string  `json:"UID"`
	FileUID  string  `json:"FileUID"`
	Type     string  `json:"Type"`
	Src      string  `json:"Src"`
	Name     string  `json:"Name"`
	SubjUID  string  `json:"SubjUID"`
	SubjSrc  string  `json:"SubjSrc"`
	FaceID   string  `json:"FaceID"`
	FaceDist float64 `json:"FaceDist"`
	X        float64 `json:"X"`  // Relative X position (0-1)
	Y        float64 `json:"Y"`  // Relative Y position (0-1)
	W        float64 `json:"W"`  // Relative width (0-1)
	H        float64 `json:"H"`  // Relative height (0-1)
	Size     int     `json:"Size"`
	Score    int     `json:"Score"`
	Invalid  bool    `json:"Invalid"`
	Review   bool    `json:"Review"`
}

// GetFaces retrieves faces from PhotoPrism
func (pp *PhotoPrism) GetFaces(count int, offset int) ([]Face, error) {
	url := fmt.Sprintf("%s/faces?count=%d&offset=%d&hidden=yes&unknown=yes", pp.Url, count, offset)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(fmt.Sprintf("faces_offset_%d", offset), body)

	var faces []Face
	if err := json.Unmarshal(body, &faces); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return faces, nil
}

// GetPhotoMarkers extracts markers from photo details
// Returns markers found in the photo's files
func (pp *PhotoPrism) GetPhotoMarkers(photoUID string) ([]Marker, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, err
	}

	var markers []Marker

	// Extract markers from Files
	if files, ok := details["Files"].([]interface{}); ok {
		for _, fileInterface := range files {
			if file, ok := fileInterface.(map[string]interface{}); ok {
				if fileMarkers, ok := file["Markers"].([]interface{}); ok {
					for _, markerInterface := range fileMarkers {
						if m, ok := markerInterface.(map[string]interface{}); ok {
							marker := Marker{}
							if v, ok := m["UID"].(string); ok {
								marker.UID = v
							}
							if v, ok := m["FileUID"].(string); ok {
								marker.FileUID = v
							}
							if v, ok := m["Type"].(string); ok {
								marker.Type = v
							}
							if v, ok := m["Src"].(string); ok {
								marker.Src = v
							}
							if v, ok := m["Name"].(string); ok {
								marker.Name = v
							}
							if v, ok := m["SubjUID"].(string); ok {
								marker.SubjUID = v
							}
							if v, ok := m["SubjSrc"].(string); ok {
								marker.SubjSrc = v
							}
							if v, ok := m["FaceID"].(string); ok {
								marker.FaceID = v
							}
							if v, ok := m["FaceDist"].(float64); ok {
								marker.FaceDist = v
							}
							if v, ok := m["X"].(float64); ok {
								marker.X = v
							}
							if v, ok := m["Y"].(float64); ok {
								marker.Y = v
							}
							if v, ok := m["W"].(float64); ok {
								marker.W = v
							}
							if v, ok := m["H"].(float64); ok {
								marker.H = v
							}
							if v, ok := m["Size"].(float64); ok {
								marker.Size = int(v)
							}
							if v, ok := m["Score"].(float64); ok {
								marker.Score = int(v)
							}
							if v, ok := m["Invalid"].(bool); ok {
								marker.Invalid = v
							}
							if v, ok := m["Review"].(bool); ok {
								marker.Review = v
							}
							markers = append(markers, marker)
						}
					}
				}
			}
		}
	}

	return markers, nil
}

// MarkerCreate represents the data needed to create a new marker
type MarkerCreate struct {
	FileUID string  `json:"FileUID"`
	Type    string  `json:"Type"`    // "face" for face markers
	X       float64 `json:"X"`       // Relative X position (0-1)
	Y       float64 `json:"Y"`       // Relative Y position (0-1)
	W       float64 `json:"W"`       // Relative width (0-1)
	H       float64 `json:"H"`       // Relative height (0-1)
	Name    string  `json:"Name"`    // Person name (optional)
	Src     string  `json:"Src"`     // Source: "manual", "image", etc.
	SubjSrc string  `json:"SubjSrc"` // Subject source: "manual" if user-assigned
}

// MarkerUpdate represents the data to update an existing marker
type MarkerUpdate struct {
	Name    string `json:"Name,omitempty"`    // Person name
	SubjSrc string `json:"SubjSrc,omitempty"` // Subject source: "manual" if user-assigned
}

// CreateMarker creates a new face marker on a photo
func (pp *PhotoPrism) CreateMarker(marker MarkerCreate) (*Marker, error) {
	body, err := json.Marshal(marker)
	if err != nil {
		return nil, fmt.Errorf("could not marshal marker: %w", err)
	}

	req, err := http.NewRequest("POST", pp.Url+"/markers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	pp.captureResponse("markers_create", respBody)

	var result Marker
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &result, nil
}

// UpdateMarker updates an existing marker (e.g., to assign a person)
func (pp *PhotoPrism) UpdateMarker(markerUID string, update MarkerUpdate) (*Marker, error) {
	body, err := json.Marshal(update)
	if err != nil {
		return nil, fmt.Errorf("could not marshal update: %w", err)
	}

	url := fmt.Sprintf("%s/markers/%s", pp.Url, markerUID)
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	pp.captureResponse(fmt.Sprintf("markers_%s_update", markerUID), respBody)

	var result Marker
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &result, nil
}

// GetPhotoFileUID extracts the primary file UID from photo details
func (pp *PhotoPrism) GetPhotoFileUID(photoUID string) (string, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return "", err
	}

	if files, ok := details["Files"].([]interface{}); ok && len(files) > 0 {
		if file, ok := files[0].(map[string]interface{}); ok {
			if uid, ok := file["UID"].(string); ok {
				return uid, nil
			}
		}
	}

	return "", fmt.Errorf("could not find file UID for photo %s", photoUID)
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

	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("could not marshal selection: %w", err)
	}

	url := fmt.Sprintf("%s/albums/%s/photos", pp.Url, albumUID)
	req, err := http.NewRequest("DELETE", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
