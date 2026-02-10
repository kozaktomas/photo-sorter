package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/sorter"
)

var sortCmd = &cobra.Command{
	Use:   "sort [album-uid]",
	Short: "Sort photos using AI",
	Long: `Sort photos in a PhotoPrism album using AI.
The command analyzes photos and applies labels and estimated dates
based on visual content analysis.`,
	Args: cobra.ExactArgs(1),
	RunE: runSort,
}

func init() {
	rootCmd.AddCommand(sortCmd)

	sortCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	sortCmd.Flags().Int("limit", 0, "Limit number of photos to process (0 = no limit)")
	sortCmd.Flags().Bool("individual-dates", false, "Estimate date per photo instead of album-wide")
	sortCmd.Flags().Bool("batch", false, "Use batch API for 50% cost savings (slower, may take minutes)")
	sortCmd.Flags().String("provider", "openai", "AI provider to use: openai, gemini, ollama, llamacpp")
	sortCmd.Flags().Bool("force-date", false, "Overwrite existing dates with AI estimates")
	sortCmd.Flags().Int("concurrency", 5, "Number of parallel requests in standard mode")
}

// createAIProvider creates the AI provider based on the provider name and config.
func createAIProvider(providerName string, cfg *config.Config) (ai.Provider, error) {
	switch providerName {
	case "openai":
		if cfg.OpenAI.Token == "" {
			return nil, errors.New("OPENAI_TOKEN environment variable is required")
		}
		pricing := cfg.GetModelPricing("gpt-4.1-mini")
		return ai.NewOpenAIProvider(cfg.OpenAI.Token,
			ai.RequestPricing{Input: pricing.Standard.Input, Output: pricing.Standard.Output},
			ai.RequestPricing{Input: pricing.Batch.Input, Output: pricing.Batch.Output},
		), nil
	case "gemini":
		if cfg.Gemini.APIKey == "" {
			return nil, errors.New("GEMINI_API_KEY environment variable is required")
		}
		pricing := cfg.GetModelPricing("gemini-2.5-flash")
		return ai.NewGeminiProvider(context.Background(), cfg.Gemini.APIKey,
			ai.RequestPricing{Input: pricing.Standard.Input, Output: pricing.Standard.Output},
			ai.RequestPricing{Input: pricing.Batch.Input, Output: pricing.Batch.Output},
		)
	case "ollama":
		return ai.NewOllamaProvider(cfg.Ollama.URL, cfg.Ollama.Model), nil
	case "llamacpp":
		return ai.NewLlamaCppProvider(cfg.LlamaCpp.URL, cfg.LlamaCpp.Model), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: openai, gemini, ollama, llamacpp)", providerName)
	}
}

// printSortResults prints the results of a sort operation.
func printSortResults(result *sorter.SortResult, aiProvider ai.Provider, cfg *config.Config, individualDates bool) {
	fmt.Printf("\nProcessed: %d photos\n", result.ProcessedCount)
	fmt.Printf("Sorted: %d photos\n", result.SortedCount)

	usage := aiProvider.GetUsage()
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		fmt.Printf("\nAPI Usage:\n")
		fmt.Printf("  Input tokens: %d\n", usage.InputTokens)
		fmt.Printf("  Output tokens: %d\n", usage.OutputTokens)
		fmt.Printf("  Total cost: $%.4f (%.2f CZK)\n", usage.TotalCost, usage.TotalCost*21)
	}

	if result.AlbumDate != "" {
		fmt.Printf("\nEstimated album date: %s\n", result.AlbumDate)
		fmt.Printf("Reasoning: %s\n", result.DateReasoning)
	}

	if len(result.Errors) > 0 {
		fmt.Printf("\nErrors: %d\n", len(result.Errors))
		for _, err := range result.Errors {
			fmt.Printf("  - %v\n", err)
		}
	}

	if len(result.Suggestions) > 0 {
		printSortSuggestions(result.Suggestions, cfg, individualDates)
	}
}

// printSortSuggestions prints the per-photo suggestion details.
func printSortSuggestions(suggestions []ai.SortSuggestion, cfg *config.Config, individualDates bool) {
	fmt.Println("\nPhoto details:")
	for _, s := range suggestions {
		photoRef := s.PhotoUID
		if url := cfg.PhotoPrism.PhotoURL(s.PhotoUID); url != "" {
			photoRef = url
		}
		fmt.Printf("  %s:\n", photoRef)
		if len(s.Labels) > 0 {
			var labelStrs []string
			for _, l := range s.Labels {
				status := ""
				if l.Confidence < 0.8 {
					status = " (skipped)"
				}
				labelStrs = append(labelStrs, fmt.Sprintf("%s (%.0f%%)%s", l.Name, l.Confidence*100, status))
			}
			fmt.Printf("    Labels: %s\n", strings.Join(labelStrs, ", "))
		}
		if individualDates && s.EstimatedDate != "" && s.EstimatedDate != "0001-01-01" {
			fmt.Printf("    Estimated date: %s\n", s.EstimatedDate)
		}
		fmt.Printf("    Description: %s\n", s.Description)
	}
}

func runSort(cmd *cobra.Command, args []string) error {
	albumUID := args[0]
	cfg := config.Load()

	dryRun := mustGetBool(cmd, "dry-run")
	limit := mustGetInt(cmd, "limit")
	individualDates := mustGetBool(cmd, "individual-dates")
	batchMode := mustGetBool(cmd, "batch")
	providerName := mustGetString(cmd, "provider")
	forceDate := mustGetBool(cmd, "force-date")
	concurrency := mustGetInt(cmd, "concurrency")

	aiProvider, err := createAIProvider(providerName, cfg)
	if err != nil {
		return err
	}
	if batchMode {
		aiProvider.SetBatchMode(true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal...")
		cancel()
	}()

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}

	fmt.Printf("Sorting album: %s\n", album.Title)
	if album.Description != "" {
		fmt.Printf("Description: %s\n", album.Description)
	}
	fmt.Printf("Provider: %s\n", aiProvider.Name())
	if dryRun {
		fmt.Println("Mode: DRY RUN (no changes will be applied)")
	}
	if batchMode {
		fmt.Println("Mode: BATCH (50% cost savings, may take minutes)")
	}
	fmt.Println()

	s := sorter.New(pp, aiProvider)
	result, err := s.Sort(ctx, albumUID, album.Title, album.Description, sorter.SortOptions{
		DryRun:          dryRun,
		Limit:           limit,
		IndividualDates: individualDates,
		BatchMode:       batchMode,
		ForceDate:       forceDate,
		Concurrency:     concurrency,
	})
	if err != nil {
		return fmt.Errorf("sorting failed: %w", err)
	}

	printSortResults(result, aiProvider, cfg, individualDates)
	return nil
}
