package cmd

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var captureDir string

var rootCmd = &cobra.Command{
	Use:   "photo-sorter",
	Short: "A CLI tool for sorting photos in PhotoPrism using AI",
	Long: `Photo Sorter is a CLI application that connects to a PhotoPrism instance
and uses AI models (OpenAI, Anthropic, Gemini) to intelligently sort and
organize your photos based on their content.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&captureDir, "capture", "", "Directory to save API responses for testing")
}

func initConfig() {
	// .env file is optional, don't fail if not found
	_ = godotenv.Load()
}
