package cmd

import (
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create <album-name>",
	Short: "Create a new album",
	Long: `Create a new album in PhotoPrism.

Example:
  photo-sorter create "Summer Vacation 2024"`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func init() {
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	albumName := args[0]

	cfg := config.Load()

	// Connect to PhotoPrism.
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.GetPassword(), captureDir,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Create album.
	album, err := pp.CreateAlbum(albumName)
	if err != nil {
		return fmt.Errorf("failed to create album: %w", err)
	}

	fmt.Printf("Created album: %s\n", album.Title)
	fmt.Printf("UID: %s\n", album.UID)

	return nil
}
