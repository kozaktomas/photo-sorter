package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultLlamaCppURL   = "http://localhost:8080"
	defaultLlamaCppModel = "llava"
)

// LlamaCppProvider implements Provider using a llama.cpp server.
type LlamaCppProvider struct {
	parsedURL *url.URL
	model     string
	client    *http.Client
	usage     Usage
}

// NewLlamaCppProvider creates a new llama.cpp provider with the given config.
func NewLlamaCppProvider(baseURL, model string) (*LlamaCppProvider, error) {
	if baseURL == "" {
		baseURL = defaultLlamaCppURL
	}
	if model == "" {
		model = defaultLlamaCppModel
	}
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid llama.cpp URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid llama.cpp URL scheme %q: must be http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("invalid llama.cpp URL: missing host")
	}
	return &LlamaCppProvider{
		parsedURL: parsed,
		model:     model,
		client:    &http.Client{},
	}, nil
}

// Name returns the provider name.
func (p *LlamaCppProvider) Name() string {
	return p.model
}

// SetBatchMode is a no-op; llama.cpp does not support batch mode.
func (p *LlamaCppProvider) SetBatchMode(enabled bool) {
	// llama.cpp doesn't support batch mode - no-op.
}

// GetUsage returns the accumulated API token usage.
func (p *LlamaCppProvider) GetUsage() *Usage {
	return &p.usage
}

// ResetUsage zeroes out the accumulated token usage counters.
func (p *LlamaCppProvider) ResetUsage() {
	p.usage = Usage{}
}

// llamaCppRequest represents a request to the llama.cpp OpenAI-compatible API.
type llamaCppRequest struct {
	Model       string            `json:"model"`
	Messages    []llamaCppMessage `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
}

type llamaCppMessage struct {
	Role    string                 `json:"role"`
	Content llamaCppMessageContent `json:"content"`
}

// llamaCppMessageContent can be a string or an array of content parts.
type llamaCppMessageContent any

type llamaCppContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *llamaCppImageURL `json:"image_url,omitempty"`
}

type llamaCppImageURL struct {
	URL string `json:"url"`
}

// llamaCppResponse represents a response from the llama.cpp OpenAI-compatible API.
type llamaCppResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// AnalyzePhoto sends a photo to llama.cpp for AI analysis and returns labels and description.
func (p *LlamaCppProvider) AnalyzePhoto(
	ctx context.Context,
	imageData []byte,
	metadata *PhotoMetadata,
	availableLabels []string,
	estimateDate bool,
) (*PhotoAnalysis, error) {
	const maxRetries = 5

	resizedData, err := ResizeImage(imageData, 800)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	messages := buildLlamaCppPhotoMessages(resizedData, metadata, availableLabels, estimateDate)

	var lastError error
	var lastResponse string

	for range maxRetries {
		resp, err := p.sendRequest(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("llama.cpp API error: %w", err)
		}

		p.usage.InputTokens += resp.Usage.PromptTokens
		p.usage.OutputTokens += resp.Usage.CompletionTokens

		if len(resp.Choices) == 0 {
			return nil, errors.New("no response from llama.cpp")
		}

		content := resp.Choices[0].Message.Content
		lastResponse = content

		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(extractJSON(content)), &analysis); err != nil {
			lastError = err
			messages = appendLlamaCppRetryMessages(messages, content, err)
			continue
		}

		return &analysis, nil
	}

	return nil, fmt.Errorf(
		"failed to parse analysis JSON after %d attempts: %w (last response: %s)",
		maxRetries, lastError, lastResponse,
	)
}

func buildLlamaCppPhotoMessages(
	imageData []byte,
	metadata *PhotoMetadata,
	availableLabels []string,
	estimateDate bool,
) []llamaCppMessage {
	systemPrompt := buildPhotoAnalysisPrompt(availableLabels, estimateDate)
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	imageURL := "data:image/jpeg;base64," + base64Image
	userMessage := buildUserMessageWithMetadata(metadata)

	return []llamaCppMessage{
		{Role: "system", Content: systemPrompt},
		{
			Role: "user",
			Content: []llamaCppContentPart{
				{Type: "text", Text: userMessage},
				{Type: "image_url", ImageURL: &llamaCppImageURL{URL: imageURL}},
			},
		},
	}
}

func appendLlamaCppRetryMessages(messages []llamaCppMessage, content string, parseErr error) []llamaCppMessage {
	return append(messages,
		llamaCppMessage{Role: "assistant", Content: content},
		llamaCppMessage{
			Role: "user",
			Content: fmt.Sprintf(
				"JSON parse error: %v. Please fix the JSON and try again."+
					" Remember to escape quotes inside strings with backslash."+
					" Output ONLY valid JSON, no other text.", parseErr,
			),
		},
	)
}

// EstimateAlbumDate estimates the date for a set of photos based on their descriptions.
func (p *LlamaCppProvider) EstimateAlbumDate(
	ctx context.Context,
	albumTitle string,
	albumDescription string,
	photoDescriptions []string,
) (*AlbumDateEstimate, error) {
	systemPrompt := buildAlbumDatePrompt()
	userContent := buildAlbumDateContent(albumTitle, albumDescription, photoDescriptions)

	messages := []llamaCppMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: userContent,
		},
	}

	resp, err := p.sendRequest(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("llama.cpp API error: %w", err)
	}

	// Track usage.
	p.usage.InputTokens += resp.Usage.PromptTokens
	p.usage.OutputTokens += resp.Usage.CompletionTokens

	if len(resp.Choices) == 0 {
		return nil, errors.New("no response from llama.cpp")
	}

	content := resp.Choices[0].Message.Content
	jsonContent := extractJSON(content)

	var estimate AlbumDateEstimate
	if err := json.Unmarshal([]byte(jsonContent), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse album date JSON: %w (response: %s)", err, content)
	}

	return &estimate, nil
}

func (p *LlamaCppProvider) sendRequest(ctx context.Context, messages []llamaCppMessage) (*llamaCppResponse, error) {
	reqBody := llamaCppRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   500,
		Temperature: 0.1,
		Stream:      false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqURL := p.parsedURL.JoinPath("/v1/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
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

	var llamaResp llamaCppResponse
	if err := json.Unmarshal(body, &llamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &llamaResp, nil
}

// CreatePhotoBatch is not supported by llama.cpp and returns an error.
func (p *LlamaCppProvider) CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (string, error) {
	return "", errors.New("llama.cpp does not support batch operations")
}

// GetBatchStatus is not supported by llama.cpp and returns an error.
func (p *LlamaCppProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	return nil, errors.New("llama.cpp does not support batch operations")
}

// GetBatchResults is not supported by llama.cpp and returns an error.
func (p *LlamaCppProvider) GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error) {
	return nil, errors.New("llama.cpp does not support batch operations")
}

// CancelBatch is not supported by llama.cpp and returns an error.
func (p *LlamaCppProvider) CancelBatch(ctx context.Context, batchID string) error {
	return errors.New("llama.cpp does not support batch operations")
}
