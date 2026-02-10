package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/genai"
)

const geminiModel = "gemini-2.5-flash"

type GeminiProvider struct {
	client        *genai.Client
	batchPhotoIDs map[string][]string // maps batch ID to ordered photo UIDs
	usage         Usage
	inputPrice    float64 // per 1M tokens
	outputPrice   float64 // per 1M tokens
	batchPricing  RequestPricing
}

func NewGeminiProvider(ctx context.Context, apiKey string, singlePricing, batchPricing RequestPricing) (*GeminiProvider, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &GeminiProvider{
		client:        client,
		batchPhotoIDs: make(map[string][]string),
		inputPrice:    singlePricing.Input,
		outputPrice:   singlePricing.Output,
		batchPricing:  batchPricing,
	}, nil
}

func (p *GeminiProvider) SetBatchMode(enabled bool) {
	if enabled {
		p.inputPrice = p.batchPricing.Input
		p.outputPrice = p.batchPricing.Output
	}
}

func (p *GeminiProvider) GetUsage() *Usage {
	return &p.usage
}

func (p *GeminiProvider) ResetUsage() {
	p.usage = Usage{}
}

func (p *GeminiProvider) trackUsage(inputTokens, outputTokens int32) {
	p.usage.InputTokens += int(inputTokens)
	p.usage.OutputTokens += int(outputTokens)
	p.usage.TotalCost += float64(inputTokens) / 1_000_000 * p.inputPrice
	p.usage.TotalCost += float64(outputTokens) / 1_000_000 * p.outputPrice
}

func (p *GeminiProvider) Name() string {
	return geminiModel
}

func (p *GeminiProvider) AnalyzePhoto(ctx context.Context, imageData []byte, metadata *PhotoMetadata, availableLabels []string, estimateDate bool) (*PhotoAnalysis, error) {
	const maxRetries = 5

	// Resize image to max 800px to save costs
	resizedData, err := ResizeImage(imageData, 800)
	if err != nil {
		return nil, fmt.Errorf("failed to resize image: %w", err)
	}

	systemPrompt := buildPhotoAnalysisPrompt(availableLabels, estimateDate)
	userMessage := buildUserMessageWithMetadata(metadata)

	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: systemPrompt + "\n\n" + userMessage},
				{InlineData: &genai.Blob{Data: resizedData, MIMEType: "image/jpeg"}},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
	}

	var lastError error
	var lastResponse string

	for range maxRetries {
		result, err := p.client.Models.GenerateContent(ctx, geminiModel, contents, config)
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		// Track usage
		if result.UsageMetadata != nil {
			p.trackUsage(result.UsageMetadata.PromptTokenCount, result.UsageMetadata.CandidatesTokenCount)
		}

		content := result.Text()
		if content == "" {
			return nil, errors.New("no response from Gemini")
		}
		lastResponse = content

		var analysis PhotoAnalysis
		if err := json.Unmarshal([]byte(content), &analysis); err != nil {
			lastError = err

			// Add model response and error feedback to contents for retry
			contents = append(contents,
				&genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: content}},
				},
				&genai.Content{
					Role:  "user",
					Parts: []*genai.Part{{Text: fmt.Sprintf("JSON parse error: %v. Please fix the JSON and try again. Remember to escape quotes inside strings with backslash.", err)}},
				},
			)
			continue
		}

		return &analysis, nil
	}

	return nil, fmt.Errorf("failed to parse analysis JSON after %d attempts: %w (last response: %s)", maxRetries, lastError, lastResponse)
}

func (p *GeminiProvider) EstimateAlbumDate(ctx context.Context, albumTitle string, albumDescription string, photoDescriptions []string) (*AlbumDateEstimate, error) {
	systemPrompt := buildAlbumDatePrompt()
	userContent := buildAlbumDateContent(albumTitle, albumDescription, photoDescriptions)

	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: systemPrompt + "\n\n" + userContent},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
	}

	result, err := p.client.Models.GenerateContent(ctx, geminiModel, contents, config)
	if err != nil {
		return nil, fmt.Errorf("gemini API error: %w", err)
	}

	// Track usage
	if result.UsageMetadata != nil {
		p.trackUsage(result.UsageMetadata.PromptTokenCount, result.UsageMetadata.CandidatesTokenCount)
	}

	content := result.Text()
	if content == "" {
		return nil, errors.New("no response from Gemini")
	}

	var estimate AlbumDateEstimate
	if err := json.Unmarshal([]byte(content), &estimate); err != nil {
		return nil, fmt.Errorf("failed to parse album date JSON: %w (response: %s)", err, content)
	}

	return &estimate, nil
}

// Batch API implementation for Gemini

func (p *GeminiProvider) CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (string, error) {
	if len(requests) == 0 {
		return "", errors.New("no requests provided")
	}

	// Build inlined requests and track photo UIDs in order
	var inlinedRequests []*genai.InlinedRequest
	var photoUIDs []string
	for _, req := range requests {
		// Resize image
		resizedData, err := ResizeImage(req.ImageData, 800)
		if err != nil {
			return "", fmt.Errorf("failed to resize image for %s: %w", req.PhotoUID, err)
		}

		systemPrompt := buildPhotoAnalysisPrompt(req.AvailableLabels, req.EstimateDate)
		userMessage := buildUserMessageWithMetadata(req.Metadata)

		inlinedReq := &genai.InlinedRequest{
			Contents: []*genai.Content{
				{
					Role: "user",
					Parts: []*genai.Part{
						{Text: systemPrompt + "\n\n" + userMessage},
						{InlineData: &genai.Blob{Data: resizedData, MIMEType: "image/jpeg"}},
					},
				},
			},
			Config: &genai.GenerateContentConfig{
				ResponseMIMEType: "application/json",
			},
		}
		inlinedRequests = append(inlinedRequests, inlinedReq)
		photoUIDs = append(photoUIDs, req.PhotoUID)
	}

	// Create the batch job
	batchJob, err := p.client.Batches.Create(ctx, geminiModel, &genai.BatchJobSource{
		InlinedRequests: inlinedRequests,
	}, &genai.CreateBatchJobConfig{
		DisplayName: "photo-sorter-batch",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create batch: %w", err)
	}

	// Store photo UIDs for later retrieval
	p.batchPhotoIDs[batchJob.Name] = photoUIDs

	return batchJob.Name, nil
}

func (p *GeminiProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error) {
	batch, err := p.client.Batches.Get(ctx, batchID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch status: %w", err)
	}

	status := &BatchStatus{
		ID:     batch.Name,
		Status: string(batch.State),
	}

	// Get counts from completion stats if available
	if batch.CompletionStats != nil {
		status.CompletedCount = int(batch.CompletionStats.SuccessfulCount)
		status.FailedCount = int(batch.CompletionStats.FailedCount)
		status.TotalRequests = status.CompletedCount + status.FailedCount + int(batch.CompletionStats.IncompleteCount)
	}

	return status, nil
}

func (p *GeminiProvider) GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error) {
	batch, err := p.client.Batches.Get(ctx, batchID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch: %w", err)
	}

	if batch.State != "JOB_STATE_SUCCEEDED" {
		return nil, fmt.Errorf("batch is not completed, status: %s", batch.State)
	}

	if batch.Dest == nil || len(batch.Dest.InlinedResponses) == 0 {
		return nil, errors.New("no results available")
	}

	// Get stored photo UIDs
	photoUIDs, ok := p.batchPhotoIDs[batchID]
	if !ok {
		return nil, fmt.Errorf("photo UIDs not found for batch %s", batchID)
	}

	var results []BatchPhotoResult
	for i, resp := range batch.Dest.InlinedResponses {
		photoUID := "unknown"
		if i < len(photoUIDs) {
			photoUID = photoUIDs[i]
		}
		results = append(results, parseGeminiBatchResponse(resp, photoUID))
	}

	// Clean up stored photo UIDs
	delete(p.batchPhotoIDs, batchID)

	return results, nil
}

// parseGeminiBatchResponse converts a single Gemini batch response into a BatchPhotoResult.
func parseGeminiBatchResponse(resp *genai.InlinedResponse, photoUID string) BatchPhotoResult {
	result := BatchPhotoResult{PhotoUID: photoUID}
	switch {
	case resp.Error != nil:
		result.Error = resp.Error.Message
	case resp.Response != nil:
		content := resp.Response.Text()
		if content == "" {
			result.Error = "empty response"
		} else {
			var analysis PhotoAnalysis
			if err := json.Unmarshal([]byte(content), &analysis); err != nil {
				result.Error = fmt.Sprintf("failed to parse analysis: %v", err)
			} else {
				result.Analysis = &analysis
			}
		}
	default:
		result.Error = "no response"
	}
	return result
}

func (p *GeminiProvider) CancelBatch(ctx context.Context, batchID string) error {
	err := p.client.Batches.Cancel(ctx, batchID, nil)
	if err != nil {
		return fmt.Errorf("failed to cancel batch: %w", err)
	}
	// Clean up stored photo UIDs
	delete(p.batchPhotoIDs, batchID)
	return nil
}
