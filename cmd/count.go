package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var countCmd = &cobra.Command{
	Use:   "count [album-uid]",
	Short: "Show the number of photos in an album",
	Long:  `Displays the photo count for a specific album identified by its UID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runCount,
}

func init() {
	rootCmd.AddCommand(countCmd)
}

func runCount(cmd *cobra.Command, args []string) error {
	albumUID := args[0]

	cfg := config.Load()

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}

	// Count photos by fetching them with pagination
	totalPhotos := 0
	batchSize := 1000
	offset := 0

	for {
		photos, err := pp.GetAlbumPhotos(albumUID, batchSize, offset)
		if err != nil {
			return fmt.Errorf("failed to get album photos: %w", err)
		}

		totalPhotos += len(photos)

		if len(photos) < batchSize {
			break
		}
		offset += batchSize
	}

	fmt.Printf("Album: %s\n", album.Title)
	fmt.Printf("Photos: %d\n", totalPhotos)

	return nil
}
