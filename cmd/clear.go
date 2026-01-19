package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/photoprism"
)

var clearCmd = &cobra.Command{
	Use:   "clear <album-uid>",
	Short: "Remove all photos from an album",
	Long: `Remove all photos from an album while keeping them in the library.

This command removes the association between photos and the album,
but does not delete the photos from PhotoPrism.

Example:
  photo-sorter clear aq8i4k2l3m9n0o1p`,
	Args: cobra.ExactArgs(1),
	RunE: runClear,
}

func init() {
	rootCmd.AddCommand(clearCmd)

	clearCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runClear(cmd *cobra.Command, args []string) error {
	albumUID := args[0]
	skipConfirm, _ := cmd.Flags().GetBool("yes")

	cfg := config.Load()

	// Connect to PhotoPrism
	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Get album info
	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}

	fmt.Printf("Album: %s\n", album.Title)

	// Get all photos from album
	fmt.Println("Fetching photos...")
	var allPhotos []photoprism.Photo
	offset := 0
	batchSize := 100

	for {
		photos, err := pp.GetAlbumPhotos(albumUID, batchSize, offset)
		if err != nil {
			return fmt.Errorf("failed to get album photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		allPhotos = append(allPhotos, photos...)
		offset += len(photos)
		if len(photos) < batchSize {
			break
		}
	}

	if len(allPhotos) == 0 {
		fmt.Println("Album is already empty.")
		return nil
	}

	fmt.Printf("Photos: %d\n", len(allPhotos))

	// Confirm
	if !skipConfirm {
		fmt.Printf("\nRemove all %d photo(s) from this album? [y/N]: ", len(allPhotos))
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Extract photo UIDs
	photoUIDs := make([]string, len(allPhotos))
	for i, photo := range allPhotos {
		photoUIDs[i] = photo.UID
	}

	// Remove photos from album
	fmt.Printf("Removing %d photo(s) from album...\n", len(photoUIDs))
	if err := pp.RemovePhotosFromAlbum(albumUID, photoUIDs); err != nil {
		return fmt.Errorf("failed to remove photos: %w", err)
	}

	fmt.Printf("Done! Removed %d photo(s) from album '%s'\n", len(photoUIDs), album.Title)
	return nil
}
