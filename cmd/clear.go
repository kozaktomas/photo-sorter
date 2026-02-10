package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
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

// fetchAllAlbumPhotos retrieves all photos from an album using pagination.
func fetchAllAlbumPhotos(pp *photoprism.PhotoPrism, albumUID string, pageSize int) ([]photoprism.Photo, error) {
	var allPhotos []photoprism.Photo
	offset := 0
	for {
		photos, err := pp.GetAlbumPhotos(albumUID, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to get photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		allPhotos = append(allPhotos, photos...)
		if len(photos) < pageSize {
			break
		}
		offset += len(photos)
	}
	return allPhotos, nil
}

func confirmAction(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

func runClear(cmd *cobra.Command, args []string) error {
	albumUID := args[0]
	skipConfirm := mustGetBool(cmd, "yes")

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

	fmt.Printf("Album: %s\n", album.Title)

	fmt.Println("Fetching photos...")
	allPhotos, err := fetchAllAlbumPhotos(pp, albumUID, 100)
	if err != nil {
		return err
	}

	if len(allPhotos) == 0 {
		fmt.Println("Album is already empty.")
		return nil
	}

	fmt.Printf("Photos: %d\n", len(allPhotos))

	if !skipConfirm && !confirmAction(fmt.Sprintf("\nRemove all %d photo(s) from this album? [y/N]: ", len(allPhotos))) {
		fmt.Println("Cancelled.")
		return nil
	}

	photoUIDs := make([]string, len(allPhotos))
	for i := range allPhotos {
		photoUIDs[i] = allPhotos[i].UID
	}

	fmt.Printf("Removing %d photo(s) from album...\n", len(photoUIDs))
	if err := pp.RemovePhotosFromAlbum(albumUID, photoUIDs); err != nil {
		return fmt.Errorf("failed to remove photos: %w", err)
	}

	fmt.Printf("Done! Removed %d photo(s) from album '%s'\n", len(photoUIDs), album.Title)
	return nil
}
