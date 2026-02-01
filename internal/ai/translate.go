package ai

import (
	"context"
	_ "embed"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed prompts/clip_translate.txt
var clipTranslatePrompt string

const (
	// GPT-4.1-mini pricing per 1M tokens
	translateInputPrice  = 0.40
	translateOutputPrice = 1.60
)

// TranslateResult contains the translation and usage information.
type TranslateResult struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
	Cost         float64 // USD
}

// TranslateForCLIP translates Czech text to English optimized for CLIP image search.
// On failure, returns the original text and the error.
func TranslateForCLIP(ctx context.Context, apiKey string, czechText string) (*TranslateResult, error) {
	client := openai.NewClient(option.WithAPIKey(apiKey))

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4_1Mini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(clipTranslatePrompt),
			openai.UserMessage(czechText),
		},
		MaxTokens: openai.Int(100),
	})
	if err != nil {
		return &TranslateResult{Text: czechText}, err
	}

	result := &TranslateResult{
		Text:         czechText,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}
	result.Cost = float64(result.InputTokens)*translateInputPrice/1_000_000 +
		float64(result.OutputTokens)*translateOutputPrice/1_000_000

	if len(resp.Choices) == 0 {
		return result, nil
	}

	translated := strings.TrimSpace(resp.Choices[0].Message.Content)
	if translated != "" {
		result.Text = translated
	}

	return result, nil
}
