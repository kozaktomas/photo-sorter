package photoprism

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// UploadFile uploads a single file to the user's upload folder
// Returns the upload token used for processing
func (pp *PhotoPrism) UploadFile(filePath string) (string, error) {
	if pp.userUID == "" {
		return "", errors.New("user UID not available")
	}

	// Generate upload token (use current timestamp)
	uploadToken := strconv.FormatInt(time.Now().UnixNano(), 10)

	// Open the file
	file, err := os.Open(filePath) //nolint:gosec // user-provided file path for upload
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
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)
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

// addFileToMultipart opens a file and writes it to the multipart writer.
func addFileToMultipart(writer *multipart.Writer, filePath string) error {
	file, err := os.Open(filePath) //nolint:gosec // user-provided file path for upload
	if err != nil {
		return fmt.Errorf("could not open file %s: %w", filePath, err)
	}
	defer file.Close()

	fileName := filepath.Base(filePath)
	part, err := writer.CreateFormFile("files", fileName)
	if err != nil {
		return fmt.Errorf("could not create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("could not copy file data: %w", err)
	}
	return nil
}

// UploadFiles uploads multiple files to the user's upload folder
// Returns the upload token used for processing
func (pp *PhotoPrism) UploadFiles(filePaths []string) (string, error) {
	if pp.userUID == "" {
		return "", errors.New("user UID not available")
	}

	if len(filePaths) == 0 {
		return "", errors.New("no files to upload")
	}

	// Generate upload token (use current timestamp)
	uploadToken := strconv.FormatInt(time.Now().UnixNano(), 10)

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add all files to form
	for _, filePath := range filePaths {
		if err := addFileToMultipart(writer, filePath); err != nil {
			return "", err
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("could not close writer: %w", err)
	}

	// Send request
	url := fmt.Sprintf("%s/users/%s/upload/%s", pp.Url, pp.userUID, uploadToken)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)
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
		return errors.New("user UID not available")
	}

	options := struct {
		Albums []string `json:"albums,omitempty"`
	}{
		Albums: albumUIDs,
	}

	return doRequestRaw(pp, "PUT", fmt.Sprintf("users/%s/upload/%s", pp.userUID, uploadToken), options)
}
