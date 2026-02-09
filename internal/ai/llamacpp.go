package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	defaultLlamaCppURL   = "http://localhost:8080"
	defaultLlamaCppModel = "llava"
)

type LlamaCppProvider struct {
	baseURL string
	model   string
	client  *http.Client
	usage   Usage
}

func NewLlamaCppProvider(baseURL, model string) *LlamaCppProvider {
	if baseURL == "" {
		baseURL = defaultLlamaCppURL
	}
	if model == "" {
		model = defaultLlamaCppModel
	}
	return &LlamaCppProvider{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (p *LlamaCppProvider) Name() string {
	return p.model
}

func (p *LlamaCppProvider) SetBatchMode(enabled bool) {
	// llama.cpp doesn't support batch mode - no-op
}

func (p *LlamaCppProvider) GetUsage() *Usage {
	return &p.usage
}

func (p *LlamaCppProvider) ResetUsage() {
	p.usage = Usage{}
}

// llamaCppRequest represents a request to the llama.cpp OpenAI-compatible API
type llamaCppRequest struct {
	Model       string             `json:"model"`
	Messages    []llamaCppMessage  `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream"`
}

type llamaCppMessage struct {
	Role    string                 `json:"role"`
	Content llamaCppMessageContent `json:"content"`
}

// llamaCppMessageContent can be a string or an array of content parts
type llamaCppMessageContent interface{}

type llamaCppContentPart struct {
	Type     string               `json:"type"`
	Text     string               `json:"text,omitempty"`
	ImageURL *llamaCppImageURL    `json:"image_url,omitempty"`
}

type llamaCppImageURL struct {
	URL string `json:"url"`
}

// llamaCppResponse represents a response from the llama.cpp OpenAI-compatible API
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

func (p *LlamaCppProvider) AnalyzePhoto(ctx context.Context, imageData []byte, metadata *PhotoMetadata, availableLabels []string, estimateDate bool) (*PhotoAnalysis, error) {
	const maxRetries = 5

	// Resize image to max 800px to reduce processing time
	resizedData, err := ResizeImage(imageData, 800)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	systemPrompt := buildPhotoAnalysisPrompt(availableLabels, estimateDate)
	base64Image := base64.StdEncoding.EncodeToString(resizedData)
	imageURL := fmt.Sprintf("data:image/jpeg;base64,%s", base64Image)
	userMessage := buildUserMessageWithMetadata(metadata)

	// Build initial messages
	messages := []llamaCppMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role: "user",
			Content: []llamaCppContentPart{
				{Type: "text", Text: userMessage},
				{Type: "image_url", ImageURL: &llamaCppImageURL{URL: imageURL}},
			},
		},
	}

	var lastError error
	var lastResponse string

	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := p.sendRequest(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("llama.cpp API error: %w", err)
		}

		// Track usage (llama.cpp is local/free, but we track tokens for stats)
		p.usage.InputTokens += resp.Usage.PromptTokens
		p.usage.OutputTokens += resp.Usage.CompletionTokens

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no response from llama.cpp")
		}

		content := resp.Choices[0].Message.Content
		lastResponse = content

		// Try to extract JSON from the response
		jsonContent := extractJSON(content)

		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(jsonContent), &analysis); err != nil {
			lastError = err

			// Add assistant response and error feedback for retry
			messages = append(messages,
				llamaCppMessage{
					Role:    "assistant",
					Content: content,
				},
				llamaCppMessage{
					Role:    "user",
					Content: fmt.Sprintf("JSON parse error: %v. Please fix the JSON and try again. Remember to escape quotes inside strings with backslash. Output ONLY valid JSON, no other text.", err),
				},
			)
			continue
		}

		return &analysis, nil
	}

	return nil, fmt.Errorf("failed to parse analysis JSON after %d attempts: %w (last response: %s)", maxRetries, lastError, lastResponse)
}

func (p *LlamaCppProvider) EstimateAlbumDate(ctx context.Context, albumTitle string, albumDescription string, photoDescriptions []string) (*AlbumDateEstimate, error) {
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

	// Track usage
	p.usage.InputTokens += resp.Usage.PromptTokens
	p.usage.OutputTokens += resp.Usage.CompletionTokens

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from llama.cpp")
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(jsonBody))
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

// Batch API methods - llama.cpp doesn't support batch operations
func (p *LlamaCppProvider) CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (string, error) {
	return "", fmt.Errorf("llama.cpp does not support batch operations")
}

func (p *LlamaCppProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	return nil, fmt.Errorf("llama.cpp does not support batch operations")
}

func (p *LlamaCppProvider) GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error) {
	return nil, fmt.Errorf("llama.cpp does not support batch operations")
}

func (p *LlamaCppProvider) CancelBatch(ctx context.Context, batchID string) error {
	return fmt.Errorf("llama.cpp does not support batch operations")
}
