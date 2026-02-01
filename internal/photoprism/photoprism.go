package photoprism

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PhotoPrism represents a client for the PhotoPrism API
type PhotoPrism struct {
	Url           string
	token         string
	downloadToken string
	userUID       string
	captureDir    string
}

// readErrorBody reads the response body for error messages.
// Returns empty string if reading fails (we're already in an error path).
func readErrorBody(r io.Reader) string {
	body, err := io.ReadAll(r)
	if err != nil {
		return "(could not read error body)"
	}
	return string(body)
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

	// WriteFile error is non-critical for capturing - log and continue
	if err := os.WriteFile(filepath, body, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to capture response to %s: %v\n", filepath, err)
	}
}

// NewPhotoPrism creates a new PhotoPrism client
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

// NewPhotoPrismFromToken creates a new PhotoPrism client from existing tokens
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
		return fmt.Errorf("logout failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	pp.token = ""
	pp.downloadToken = ""

	return nil
}
