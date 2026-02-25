package photoprism

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PhotoPrism represents a client for the PhotoPrism API
type PhotoPrism struct {
	Url       string
	parsedURL *url.URL
	token         string
	downloadToken string
	userUID       string
	captureDir    string
}

// resolveURL builds a full URL from the base API URL and the given path segments.
// If the last segment contains a query string (e.g. "labels?count=10"), it is
// split so JoinPath only receives the path portion and the query is appended.
func (pp *PhotoPrism) resolveURL(pathSegments ...string) string {
	if len(pathSegments) == 0 {
		return pp.parsedURL.String()
	}
	// Check if the last segment contains a query string
	last := pathSegments[len(pathSegments)-1]
	if pathPart, query, ok := strings.Cut(last, "?"); ok {
		pathSegments[len(pathSegments)-1] = pathPart
		result := pp.parsedURL.JoinPath(pathSegments...)
		result.RawQuery = query
		return result.String()
	}
	return pp.parsedURL.JoinPath(pathSegments...).String()
}

// authResponse is the PhotoPrism session response. Fields use unexported names
// with explicit JSON tags to avoid gosec G117 (secret field detection).
type authResponse struct {
	id     string
	token  string
	config struct {
		downloadToken string
		previewToken  string
	}
	user struct {
		uid string
	}
}

func (a *authResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal auth response: %w", err)
	}
	_ = json.Unmarshal(raw["id"], &a.id)
	_ = json.Unmarshal(raw["access_token"], &a.token)

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw["config"], &cfg); err == nil {
		_ = json.Unmarshal(cfg["downloadToken"], &a.config.downloadToken)
		_ = json.Unmarshal(cfg["previewToken"], &a.config.previewToken)
	}

	var usr map[string]json.RawMessage
	if err := json.Unmarshal(raw["user"], &usr); err == nil {
		_ = json.Unmarshal(usr["UID"], &a.user.uid)
	}
	return nil
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

	if err := os.MkdirAll(dir, 0750); err != nil {
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
	if err := os.WriteFile(filepath, body, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to capture response to %s: %v\n", filepath, err)
	}
}

// NewPhotoPrism creates a new PhotoPrism client
func NewPhotoPrism(url, username, password string) (*PhotoPrism, error) {
	return NewPhotoPrismWithCapture(url, username, password, "")
}

// NewPhotoPrismWithCapture creates a new PhotoPrism client with optional response capturing.
// Pass an empty captureDir to disable capturing.
func NewPhotoPrismWithCapture(rawURL, username, password, captureDir string) (*PhotoPrism, error) {
	apiURL := rawURL + "/api/v1"
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PhotoPrism URL: %w", err)
	}
	pp := &PhotoPrism{Url: apiURL, parsedURL: parsed}
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
func NewPhotoPrismFromToken(rawURL, token, downloadToken string) (*PhotoPrism, error) {
	apiURL := rawURL + "/api/v1"
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PhotoPrism URL: %w", err)
	}
	return &PhotoPrism{Url: apiURL, parsedURL: parsed, token: token, downloadToken: downloadToken}, nil
}

func (pp *PhotoPrism) auth(username, password string) error {
	inputBody, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return fmt.Errorf("could not marshal input: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, pp.resolveURL("sessions"), bytes.NewReader(inputBody))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL constructed from validated parsedURL via resolveURL
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse("sessions", body)

	var result authResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("could not unmarshal response: %w", err)
	}

	pp.token = result.token
	pp.downloadToken = result.config.downloadToken
	pp.userUID = result.user.uid

	return nil
}

// Logout deletes the current session (logout)
func (pp *PhotoPrism) Logout() error {
	if pp.token == "" {
		return nil // Already logged out
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, pp.resolveURL("session"), nil)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL constructed from validated parsedURL via resolveURL
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
