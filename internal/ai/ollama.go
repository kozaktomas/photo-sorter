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
	"strings"
)

const (
	defaultOllamaURL   = "http://localhost:11434"
	defaultOllamaModel = "llama3.2-vision:11b"
)

type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
	usage   Usage
}

func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}
	if model == "" {
		model = defaultOllamaModel
	}
	return &OllamaProvider{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{},
	}
}

func (p *OllamaProvider) Name() string {
	return p.model
}

func (p *OllamaProvider) SetBatchMode(enabled bool) {
	// Ollama doesn't support batch mode - no-op
}

func (p *OllamaProvider) GetUsage() *Usage {
	return &p.usage
}

func (p *OllamaProvider) ResetUsage() {
	p.usage = Usage{}
}

// ollamaRequest represents a request to the Ollama chat API
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // base64 encoded images
}

type ollamaOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
}

// ollamaResponse represents a response from the Ollama chat API
type ollamaResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done               bool `json:"done"`
	PromptEvalCount    int  `json:"prompt_eval_count"`
	EvalCount          int  `json:"eval_count"`
}

func (p *OllamaProvider) AnalyzePhoto(ctx context.Context, imageData []byte, metadata *PhotoMetadata, availableLabels []string, estimateDate bool) (*PhotoAnalysis, error) {
	const maxRetries = 5

	// Resize image to max 800px to reduce processing time
	resizedData, err := ResizeImage(imageData, 800)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	systemPrompt := buildPhotoAnalysisPrompt(availableLabels, estimateDate)
	base64Image := base64.StdEncoding.EncodeToString(resizedData)
	userMessage := buildUserMessageWithMetadata(metadata)

	// Build initial messages
	messages := []ollamaMessage{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: userMessage,
			Images:  []string{base64Image},
		},
	}

	var lastError error
	var lastResponse string

	for range maxRetries {
		resp, err := p.sendRequest(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("ollama API error: %w", err)
		}

		// Track usage (Ollama is free, but we track tokens for stats)
		p.usage.InputTokens += resp.PromptEvalCount
		p.usage.OutputTokens += resp.EvalCount

		content := resp.Message.Content
		lastResponse = content

		// Try to extract JSON from the response
		jsonContent := extractJSON(content)

		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(jsonContent), &analysis); err != nil {
			lastError = err

			// Add assistant response and error feedback for retry
			messages = append(messages,
				ollamaMessage{
					Role:    "assistant",
					Content: content,
				},
				ollamaMessage{
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

func (p *OllamaProvider) EstimateAlbumDate(ctx context.Context, albumTitle string, albumDescription string, photoDescriptions []string) (*AlbumDateEstimate, error) {
	systemPrompt := buildAlbumDatePrompt()
	userContent := buildAlbumDateContent(albumTitle, albumDescription, photoDescriptions)

	messages := []ollamaMessage{
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
		return nil, fmt.Errorf("ollama API error: %w", err)
	}

	// Track usage
	p.usage.InputTokens += resp.PromptEvalCount
	p.usage.OutputTokens += resp.EvalCount

	content := resp.Message.Content
	jsonContent := extractJSON(content)

	var estimate AlbumDateEstimate
	if err := json.Unmarshal([]byte(jsonContent), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse album date JSON: %w (response: %s)", err, content)
	}

	return &estimate, nil
}

func (p *OllamaProvider) sendRequest(ctx context.Context, messages []ollamaMessage) (*ollamaResponse, error) {
	reqBody := ollamaRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   false,
		Format:   "json",
		Options: ollamaOptions{
			NumPredict: 500,
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
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

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &ollamaResp, nil
}

// extractJSON attempts to extract JSON from a response that may contain extra text
func extractJSON(content string) string {
	// Try to find JSON object boundaries
	start := strings.Index(content, "{")
	if start == -1 {
		return content
	}

	// Find matching closing brace
	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : i+1]
			}
		}
	}

	// If no matching brace found, return from start
	return content[start:]
}

// Batch API methods - Ollama doesn't support batch operations
func (p *OllamaProvider) CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (string, error) {
	return "", errors.New("ollama does not support batch operations")
}

func (p *OllamaProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	return nil, errors.New("ollama does not support batch operations")
}

func (p *OllamaProvider) GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error) {
	return nil, errors.New("ollama does not support batch operations")
}

func (p *OllamaProvider) CancelBatch(ctx context.Context, batchID string) error {
	return errors.New("ollama does not support batch operations")
}
