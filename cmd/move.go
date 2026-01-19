package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/photoprism"
)

var moveCmd = &cobra.Command{
	Use:   "move <source-album-uid> <new-album-name>",
	Short: "Move all photos from one album to a new album",
	Long: `Move all photos from a source album to a newly created album.
After successful move, the source album will be empty.

Example:
  photo-sorter move aq8i4k2l3m9n0o1p "Vacation 2024"`,
	Args: cobra.ExactArgs(2),
	RunE: runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	sourceAlbumUID := args[0]
	newAlbumName := args[1]

	cfg := config.Load()

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Verify source album exists
	sourceAlbum, err := pp.GetAlbum(sourceAlbumUID)
	if err != nil {
		return fmt.Errorf("failed to get source album: %w", err)
	}
	fmt.Printf("Source album: %s\n", sourceAlbum.Title)

	// Get all photos from source album
	fmt.Println("Fetching photos from source album...")
	var allPhotos []photoprism.Photo
	offset := 0
	batchSize := 100

	for {
		photos, err := pp.GetAlbumPhotos(sourceAlbumUID, batchSize, offset)
		if err != nil {
			return fmt.Errorf("failed to get photos from album: %w", err)
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
		fmt.Println("No photos found in source album.")
		return nil
	}

	fmt.Printf("Found %d photo(s) to move\n", len(allPhotos))

	// Create new album
	fmt.Printf("Creating new album: %s\n", newAlbumName)
	newAlbum, err := pp.CreateAlbum(newAlbumName)
	if err != nil {
		return fmt.Errorf("failed to create new album: %w", err)
	}
	fmt.Printf("Created album: %s (UID: %s)\n", newAlbum.Title, newAlbum.UID)

	// Collect photo UIDs
	photoUIDs := make([]string, len(allPhotos))
	for i, photo := range allPhotos {
		photoUIDs[i] = photo.UID
	}

	// Add photos to new album
	fmt.Println("Adding photos to new album...")
	if err := pp.AddPhotosToAlbum(newAlbum.UID, photoUIDs); err != nil {
		return fmt.Errorf("failed to add photos to new album: %w", err)
	}

	// Remove photos from source album
	fmt.Println("Removing photos from source album...")
	if err := pp.RemovePhotosFromAlbum(sourceAlbumUID, photoUIDs); err != nil {
		return fmt.Errorf("failed to remove photos from source album: %w", err)
	}

	fmt.Printf("\nDone! Moved %d photo(s) from '%s' to '%s'\n", len(allPhotos), sourceAlbum.Title, newAlbum.Title)
	fmt.Printf("New album UID: %s\n", newAlbum.UID)

	return nil
}
