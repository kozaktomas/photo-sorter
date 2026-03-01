package ai

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

//go:embed prompts/photo_analysis_with_date.txt
var photoAnalysisWithDatePrompt string

//go:embed prompts/photo_analysis.txt
var photoAnalysisPrompt string

//go:embed prompts/album_date.txt
var albumDatePrompt string

const chatModel = openai.ChatModelGPT4_1Mini

// OpenAIProvider implements Provider using the OpenAI API.
type OpenAIProvider struct {
	client       *openai.Client
	usage        Usage
	inputPrice   float64 // per 1M tokens
	outputPrice  float64 // per 1M tokens
	batchPricing RequestPricing
}

// RequestPricing holds input/output prices per 1M tokens.
type RequestPricing struct {
	Input  float64
	Output float64
}

// NewOpenAIProvider creates a new OpenAI provider with the given config.
func NewOpenAIProvider(apiKey string, singlePricing, batchPricing RequestPricing) *OpenAIProvider {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &OpenAIProvider{
		client:       &client,
		inputPrice:   singlePricing.Input,
		outputPrice:  singlePricing.Output,
		batchPricing: batchPricing,
	}
}

// SetBatchMode enables or disables batch API mode for 50% cost savings.
func (p *OpenAIProvider) SetBatchMode(enabled bool) {
	if enabled {
		p.inputPrice = p.batchPricing.Input
		p.outputPrice = p.batchPricing.Output
	}
}

// GetUsage returns the accumulated API token usage.
func (p *OpenAIProvider) GetUsage() *Usage {
	return &p.usage
}

// ResetUsage zeroes out the accumulated token usage counters.
func (p *OpenAIProvider) ResetUsage() {
	p.usage = Usage{}
}

func (p *OpenAIProvider) trackUsage(inputTokens, outputTokens int64) {
	p.usage.InputTokens += int(inputTokens)
	p.usage.OutputTokens += int(outputTokens)
	p.usage.TotalCost += float64(inputTokens) / 1_000_000 * p.inputPrice
	p.usage.TotalCost += float64(outputTokens) / 1_000_000 * p.outputPrice
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return chatModel
}

// AnalyzePhoto sends a photo to OpenAI for AI analysis and returns labels and description.
func (p *OpenAIProvider) AnalyzePhoto(
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

	messages := buildOpenAIPhotoMessages(resizedData, metadata, availableLabels, estimateDate)

	var lastError error
	var lastResponse string

	for range maxRetries {
		resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    chatModel,
			Messages: messages,
			ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
			},
			MaxTokens: openai.Int(500),
		})
		if err != nil {
			return nil, fmt.Errorf("OpenAI API error: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, errors.New("no response from OpenAI")
		}

		if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
			p.trackUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		}

		content := resp.Choices[0].Message.Content
		lastResponse = content

		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(content), &analysis); err != nil {
			lastError = err
			messages = appendOpenAIRetryMessages(messages, content, err)
			continue
		}

		return &analysis, nil
	}

	return nil, fmt.Errorf(
		"failed to parse analysis JSON after %d attempts: %w (last response: %s)",
		maxRetries, lastError, lastResponse,
	)
}

func buildOpenAIPhotoMessages(
	imageData []byte,
	metadata *PhotoMetadata,
	availableLabels []string,
	estimateDate bool,
) []openai.ChatCompletionMessageParamUnion {
	systemPrompt := buildPhotoAnalysisPrompt(availableLabels, estimateDate)
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	imageURL := "data:image/jpeg;base64," + base64Image
	userMessage := buildUserMessageWithMetadata(metadata)

	return []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String(systemPrompt),
				},
			},
		},
		{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfArrayOfContentParts: []openai.ChatCompletionContentPartUnionParam{
						openai.TextContentPart(userMessage),
						openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
							URL:    imageURL,
							Detail: "low",
						}),
					},
				},
			},
		},
	}
}

func appendOpenAIRetryMessages(
	messages []openai.ChatCompletionMessageParamUnion,
	content string,
	parseErr error,
) []openai.ChatCompletionMessageParamUnion {
	return append(messages,
		openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				Content: openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(content),
				},
			},
		},
		openai.ChatCompletionMessageParamUnion{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String(fmt.Sprintf(
						"JSON parse error: %v. Please fix the JSON and try again."+
							" Remember to escape quotes inside strings with backslash.", parseErr,
					)),
				},
			},
		},
	)
}

// EstimateAlbumDate estimates the date for a set of photos based on their descriptions.
func (p *OpenAIProvider) EstimateAlbumDate(
	ctx context.Context,
	albumTitle string,
	albumDescription string,
	photoDescriptions []string,
) (*AlbumDateEstimate, error) {
	systemPrompt := buildAlbumDatePrompt()
	userContent := buildAlbumDateContent(albumTitle, albumDescription, photoDescriptions)

	resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfSystem: &openai.ChatCompletionSystemMessageParam{
					Content: openai.ChatCompletionSystemMessageParamContentUnion{
						OfString: openai.String(systemPrompt),
					},
				},
			},
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: openai.String(userContent),
					},
				},
			},
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
		MaxTokens: openai.Int(300),
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, errors.New("no response from OpenAI")
	}

	// Track usage.
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		p.trackUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}

	content := resp.Choices[0].Message.Content

	var estimate AlbumDateEstimate
	if err := json.Unmarshal([]byte(content), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse album date JSON: %w", err)
	}

	return &estimate, nil
}

func buildPhotoAnalysisPrompt(availableLabels []string, estimateDate bool) string {
	labelsJSON, _ := json.Marshal(availableLabels)

	if estimateDate {
		return fmt.Sprintf(photoAnalysisWithDatePrompt, string(labelsJSON))
	}

	return fmt.Sprintf(photoAnalysisPrompt, string(labelsJSON))
}

// formatDatePart formats the date from metadata into a human-readable string.
func formatDatePart(metadata *PhotoMetadata) string {
	if metadata.Year <= 0 {
		return ""
	}
	if metadata.Month > 0 && metadata.Day > 0 {
		return fmt.Sprintf("Metadata date: %04d-%02d-%02d", metadata.Year, metadata.Month, metadata.Day)
	}
	if metadata.Month > 0 {
		return fmt.Sprintf("Metadata date: %04d-%02d", metadata.Year, metadata.Month)
	}
	return fmt.Sprintf("Metadata year: %d", metadata.Year)
}

// formatMetadataParts builds the metadata context lines from photo metadata.
func formatMetadataParts(metadata *PhotoMetadata) []string {
	var parts []string

	if metadata.OriginalName != "" {
		parts = append(parts, "Original filename: "+metadata.OriginalName)
	} else if metadata.FileName != "" {
		parts = append(parts, "Filename: "+metadata.FileName)
	}

	if datePart := formatDatePart(metadata); datePart != "" {
		parts = append(parts, datePart)
	}

	if metadata.Lat != 0 || metadata.Lng != 0 {
		parts = append(parts, fmt.Sprintf("GPS coordinates: %.6f, %.6f", metadata.Lat, metadata.Lng))
	}

	if metadata.Country != "" && metadata.Country != "zz" {
		parts = append(parts, "Country: "+metadata.Country)
	}

	if metadata.Width > 0 && metadata.Height > 0 {
		parts = append(parts, fmt.Sprintf("Dimensions: %dx%d", metadata.Width, metadata.Height))
	}

	return parts
}

func buildUserMessageWithMetadata(metadata *PhotoMetadata) string {
	if metadata == nil {
		return "Analyze this photo."
	}

	parts := append([]string{"Analyze this photo."}, formatMetadataParts(metadata)...)
	return strings.Join(parts, "\n")
}

// batchRequest represents a single request in the batch JSONL file.
type batchRequest struct {
	CustomID string           `json:"custom_id"`
	Method   string           `json:"method"`
	URL      string           `json:"url"`
	Body     batchRequestBody `json:"body"`
}

type batchRequestBody struct {
	Model          string           `json:"model"`
	Messages       []map[string]any `json:"messages"`
	ResponseFormat map[string]any   `json:"response_format"`
	MaxTokens      int              `json:"max_tokens"`
}

// batchResponse represents a single response in the batch output JSONL file.
type batchResponse struct {
	ID       string `json:"id"`
	CustomID string `json:"custom_id"`
	Response struct {
		StatusCode int `json:"status_code"`
		Body       struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// CreatePhotoBatch submits a batch of photos for asynchronous processing via the OpenAI batch API.
func (p *OpenAIProvider) CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (string, error) {
	if len(requests) == 0 {
		return "", errors.New("no requests provided")
	}

	var jsonlBuffer bytes.Buffer
	for _, req := range requests {
		if err := writeBatchRequestLine(&jsonlBuffer, req); err != nil {
			return "", err
		}
	}

	uploadResp, err := p.client.Files.New(ctx, openai.FileNewParams{
		File:    strings.NewReader(jsonlBuffer.String()),
		Purpose: openai.FilePurposeBatch,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload batch file: %w", err)
	}

	batchResp, err := p.client.Batches.New(ctx, openai.BatchNewParams{
		InputFileID:      uploadResp.ID,
		Endpoint:         "/v1/chat/completions",
		CompletionWindow: "24h",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create batch: %w", err)
	}

	return batchResp.ID, nil
}

func writeBatchRequestLine(buf *bytes.Buffer, req BatchPhotoRequest) error {
	resizedData, err := ResizeImage(req.ImageData, 800)
	if err != nil {
		return fmt.Errorf("failed to resize image for %s: %w", req.PhotoUID, err)
	}

	systemPrompt := buildPhotoAnalysisPrompt(req.AvailableLabels, req.EstimateDate)
	base64Image := base64.StdEncoding.EncodeToString(resizedData)
	imageURL := "data:image/jpeg;base64," + base64Image
	userMessage := buildUserMessageWithMetadata(req.Metadata)

	batchReq := batchRequest{
		CustomID: req.PhotoUID,
		Method:   "POST",
		URL:      "/v1/chat/completions",
		Body: batchRequestBody{
			Model: chatModel,
			Messages: []map[string]any{
				{"role": "system", "content": systemPrompt},
				{
					"role": "user",
					"content": []map[string]any{
						{"type": "text", "text": userMessage},
						{"type": "image_url", "image_url": map[string]any{"url": imageURL, "detail": "low"}},
					},
				},
			},
			ResponseFormat: map[string]any{"type": "json_object"},
			MaxTokens:      500,
		},
	}

	reqJSON, err := json.Marshal(batchReq)
	if err != nil {
		return fmt.Errorf("failed to marshal batch request: %w", err)
	}
	buf.Write(reqJSON)
	buf.WriteByte('\n')
	return nil
}

// GetBatchStatus checks the status of an async batch job.
func (p *OpenAIProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	batch, err := p.client.Batches.Get(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch status: %w", err)
	}

	return &BatchStatus{
		ID:             batch.ID,
		Status:         string(batch.Status),
		TotalRequests:  int(batch.RequestCounts.Total),
		CompletedCount: int(batch.RequestCounts.Completed),
		FailedCount:    int(batch.RequestCounts.Failed),
	}, nil
}

// GetBatchResults downloads and parses the results of a completed batch job.
func (p *OpenAIProvider) GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error) {
	// Get batch to find output file ID.
	batch, err := p.client.Batches.Get(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch: %w", err)
	}

	if batch.Status != openai.BatchStatusCompleted {
		return nil, fmt.Errorf("batch is not completed, status: %s", batch.Status)
	}

	if batch.OutputFileID == "" {
		return nil, errors.New("no output file available")
	}

	// Download output file content.
	fileContent, err := p.client.Files.Content(ctx, batch.OutputFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to download batch results: %w", err)
	}
	defer fileContent.Body.Close()

	// Read and parse JSONL results.
	content, err := io.ReadAll(fileContent.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read batch results: %w", err)
	}

	var results []BatchPhotoResult
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		results = append(results, parseBatchResultLine(line))
	}

	return results, nil
}

// parseBatchResultLine parses a single JSONL line from a batch output file.
func parseBatchResultLine(line []byte) BatchPhotoResult {
	var resp batchResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return BatchPhotoResult{
			PhotoUID: "unknown",
			Error:    fmt.Sprintf("failed to parse response: %v", err),
		}
	}

	result := BatchPhotoResult{PhotoUID: resp.CustomID}
	switch {
	case resp.Error != nil:
		result.Error = resp.Error.Message
	case resp.Response.Body.Error != nil:
		result.Error = resp.Response.Body.Error.Message
	case len(resp.Response.Body.Choices) > 0:
		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(resp.Response.Body.Choices[0].Message.Content), &analysis); err != nil {
			result.Error = fmt.Sprintf("failed to parse analysis: %v", err)
		} else {
			result.Analysis = &analysis
		}
	default:
		result.Error = "no choices in response"
	}
	return result
}

// CancelBatch cancels a running batch job.
func (p *OpenAIProvider) CancelBatch(ctx context.Context, batchID string) error {
	_, err := p.client.Batches.Cancel(ctx, batchID)
	if err != nil {
		return fmt.Errorf("failed to cancel batch: %w", err)
	}
	return nil
}
