package photoprism

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

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

	_, err := doRequestRaw(pp, "PUT", fmt.Sprintf("users/%s/upload/%s", pp.userUID, uploadToken), options, http.StatusOK)
	return err
}
