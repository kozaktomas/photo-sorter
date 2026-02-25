package fingerprint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

const (
	defaultEmbeddingURL   = "http://localhost:8000"
	defaultEmbeddingModel = "clip" // model name for reference only
)

// EmbeddingClient computes image embeddings using the embedding server
type EmbeddingClient struct {
	parsedURL *url.URL
	model     string
	client    *http.Client
}

// NewEmbeddingClient creates a new embedding client.
// Returns an error if baseURL is not a valid HTTP(S) URL.
func NewEmbeddingClient(baseURL, model string) (*EmbeddingClient, error) {
	if baseURL == "" {
		baseURL = defaultEmbeddingURL
	}
	if model == "" {
		model = defaultEmbeddingModel
	}
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid embedding URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid embedding URL scheme %q: must be http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("invalid embedding URL: missing host")
	}
	return &EmbeddingClient{
		parsedURL: parsed,
		model:     model,
		client:    &http.Client{},
	}, nil
}

// embeddingResponse represents the response from the embedding server
type embeddingResponse struct {
	Dim        int       `json:"dim"`
	Embedding  []float32 `json:"embedding"`
	Model      string    `json:"model"`
	Pretrained string    `json:"pretrained"`
}

// EmbeddingResult contains the embedding and its metadata
type EmbeddingResult struct {
	Embedding  []float32
	Model      string
	Pretrained string
	Dim        int
}

// postMultipartImage constructs a multipart form with the image data and posts it to the given endpoint.
// If withMIME is true, the part includes an explicit Content-Type header based on magic byte detection.
func (c *EmbeddingClient) postMultipartImage(ctx context.Context, endpoint string, imageData []byte, withMIME bool) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	var part io.Writer
	var err error
	if withMIME {
		mimeType := detectMIMEType(imageData)
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="file"; filename="image.jpg"`)
		h.Set("Content-Type", mimeType)
		part, err = writer.CreatePart(h)
	} else {
		part, err = writer.CreateFormFile("file", "image.jpg")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(imageData); err != nil {
		return nil, fmt.Errorf("failed to write image data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	reqURL := c.parsedURL.JoinPath(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req) //nolint:gosec // URL validated in NewEmbeddingClient (scheme + host check)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// ComputeEmbedding computes the embedding for an image using the embedding server
func (c *EmbeddingClient) ComputeEmbedding(ctx context.Context, imageData []byte) ([]float32, error) {
	body, err := c.postMultipartImage(ctx, "/embed/image", imageData, false)
	if err != nil {
		return nil, err
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, errors.New("empty embedding returned")
	}

	return embResp.Embedding, nil
}

// magicSignature maps a magic byte prefix (at a given offset) to a MIME type.
type magicSignature struct {
	offset   int
	magic    []byte
	mimeType string
}

// magicSignatures lists known image format magic bytes, checked in order.
var magicSignatures = []magicSignature{
	{0, []byte{0xFF, 0xD8, 0xFF}, "image/jpeg"},
	{0, []byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
	{0, []byte{0x47, 0x49, 0x46, 0x38}, "image/gif"},
	{0, []byte{0x52, 0x49, 0x46, 0x46}, "image/webp"}, // checked with extra WebP bytes below
}

// detectMIMEType detects the MIME type from image data using magic bytes.
func detectMIMEType(data []byte) string {
	for _, sig := range magicSignatures {
		end := sig.offset + len(sig.magic)
		if len(data) < end {
			continue
		}
		if !bytes.Equal(data[sig.offset:end], sig.magic) {
			continue
		}
		// WebP requires additional check at offset 8
		if sig.mimeType == "image/webp" {
			if len(data) < 12 || !bytes.Equal(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}) {
				continue
			}
		}
		return sig.mimeType
	}
	return "application/octet-stream"
}

// ComputeEmbeddingWithMetadata computes the embedding and returns full metadata
func (c *EmbeddingClient) ComputeEmbeddingWithMetadata(ctx context.Context, imageData []byte) (*EmbeddingResult, error) {
	body, err := c.postMultipartImage(ctx, "/embed/image", imageData, true)
	if err != nil {
		return nil, err
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, errors.New("empty embedding returned")
	}

	return &EmbeddingResult{
		Embedding:  embResp.Embedding,
		Model:      embResp.Model,
		Pretrained: embResp.Pretrained,
		Dim:        embResp.Dim,
	}, nil
}

// Model returns the model name being used
func (c *EmbeddingClient) Model() string {
	return c.model
}

// FaceDetection represents a single detected face
type FaceDetection struct {
	FaceIndex int       `json:"face_index"`
	Dim       int       `json:"dim"`
	Embedding []float32 `json:"embedding"`
	BBox      []float64 `json:"bbox"` // [x1, y1, x2, y2]
	DetScore  float64   `json:"det_score"`
}

// FaceResponse represents the response from the face embedding endpoint
type FaceResponse struct {
	FacesCount int             `json:"faces_count"`
	Faces      []FaceDetection `json:"faces"`
	Model      string          `json:"model"`
}

// textEmbeddingRequest represents the request body for text embedding
type textEmbeddingRequest struct {
	Text string `json:"text"`
}

// ComputeTextEmbedding computes the CLIP embedding for a text query
func (c *EmbeddingClient) ComputeTextEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody, err := json.Marshal(textEmbeddingRequest{Text: text})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqURL := c.parsedURL.JoinPath("/embed/text")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req) //nolint:gosec // URL validated in NewEmbeddingClient (scheme + host check)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, errors.New("empty embedding returned")
	}

	return embResp.Embedding, nil
}

// ComputeTextEmbeddingWithMetadata computes the CLIP embedding for a text query and returns full metadata
func (c *EmbeddingClient) ComputeTextEmbeddingWithMetadata(ctx context.Context, text string) (*EmbeddingResult, error) {
	reqBody, err := json.Marshal(textEmbeddingRequest{Text: text})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqURL := c.parsedURL.JoinPath("/embed/text")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req) //nolint:gosec // URL validated in NewEmbeddingClient (scheme + host check)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, errors.New("empty embedding returned")
	}

	return &EmbeddingResult{
		Embedding:  embResp.Embedding,
		Model:      embResp.Model,
		Pretrained: embResp.Pretrained,
		Dim:        embResp.Dim,
	}, nil
}

// ComputeFaceEmbeddings detects faces and computes their embeddings
func (c *EmbeddingClient) ComputeFaceEmbeddings(ctx context.Context, imageData []byte) (*FaceResponse, error) {
	body, err := c.postMultipartImage(ctx, "/embed/face", imageData, true)
	if err != nil {
		return nil, err
	}

	var faceResp FaceResponse
	if err := json.Unmarshal(body, &faceResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &faceResp, nil
}
