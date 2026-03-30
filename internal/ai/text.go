package ai

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

//go:embed prompts/text_check.txt
var textCheckPrompt string

//go:embed prompts/text_rewrite.txt
var textRewritePrompt string

// TokenUsage holds token counts from an API call.
type TokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// TextCheckResult contains the result of a text check operation.
type TextCheckResult struct {
	CorrectedText    string     `json:"corrected_text"`
	ReadabilityScore int        `json:"readability_score"`
	Changes          []string   `json:"changes"`
	Usage            TokenUsage `json:"usage"`
}

// TextRewriteResult contains the result of a text rewrite operation.
type TextRewriteResult struct {
	RewrittenText string     `json:"rewritten_text"`
	Usage         TokenUsage `json:"usage"`
}

// CheckText sends text to GPT-4.1-mini for spelling, diacritics, and grammar checking.
func CheckText(ctx context.Context, apiKey string, text string) (*TextCheckResult, error) {
	client := openai.NewClient(option.WithAPIKey(apiKey))

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4_1Mini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(textCheckPrompt),
			openai.UserMessage(text),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
		MaxTokens: openai.Int(2000),
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, errors.New("no response from OpenAI")
	}

	var result TextCheckResult
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse text check response: %w", err)
	}

	result.Usage = TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}

	return &result, nil
}

// RewriteText sends text to GPT-4.1-mini for length adjustment.
func RewriteText(ctx context.Context, apiKey string, text string, targetLength string) (*TextRewriteResult, error) {
	client := openai.NewClient(option.WithAPIKey(apiKey))

	userMessage := fmt.Sprintf("Target length: %s\n\nText:\n%s", targetLength, text)

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4_1Mini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(textRewritePrompt),
			openai.UserMessage(userMessage),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		},
		MaxTokens: openai.Int(2000),
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, errors.New("no response from OpenAI")
	}

	var result TextRewriteResult
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse text rewrite response: %w", err)
	}

	result.Usage = TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}

	return &result, nil
}
