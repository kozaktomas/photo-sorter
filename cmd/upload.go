package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var uploadCmd = &cobra.Command{
	Use:   "upload <album-uid> <folder-path> [folder-path...]",
	Short: "Upload photos to an album",
	Long: `Upload photos from one or more folders to a PhotoPrism album.

By default, only files in the specified folders are uploaded (non-recursive).
Use -r to search recursively in subdirectories.
Use -l to apply labels to uploaded photos (can be repeated for multiple labels).
Supported formats: jpg, jpeg, png, gif, heic, heif, webp, tiff, bmp, raw, cr2, nef, arw, dng

Example:
  photo-sorter upload aq8i4k2l3m9n0o1p /path/to/photos
  photo-sorter upload aq8i4k2l3m9n0o1p /path/to/folder1 /path/to/folder2 /path/to/folder3
  photo-sorter upload -r aq8i4k2l3m9n0o1p /path/to/photos  # recursive search
  photo-sorter upload -l "Vacation" -l "Summer" aq8i4k2l3m9n0o1p /path/to/photos`,
	Args: cobra.MinimumNArgs(2),
	RunE: runUpload,
}

func init() {
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().BoolP("recursive", "r", false, "Search for photos recursively in subdirectories")
	uploadCmd.Flags().StringSliceP("label", "l", nil, "Labels to apply to uploaded photos (can be specified multiple times)")
}

// isImageFile checks if a file has a supported image extension
func isImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	supported := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".heic": true,
		".heif": true,
		".webp": true,
		".tiff": true,
		".tif":  true,
		".bmp":  true,
		".raw":  true,
		".cr2":  true,
		".nef":  true,
		".arw":  true,
		".dng":  true,
	}
	return supported[ext]
}

// collectImageFiles collects image file paths from the given folders.
// collectImagesFromFolder collects image file paths from a single folder.
func collectImagesFromFolder(folderPath string, recursive bool) ([]string, error) {
	info, err := os.Stat(folderPath)
	if err != nil {
		return nil, fmt.Errorf("cannot access folder %s: %w", folderPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", folderPath)
	}

	if recursive {
		return collectImagesRecursive(folderPath)
	}
	return collectImagesFlat(folderPath)
}

// collectImagesRecursive walks a directory recursively for image files.
func collectImagesRecursive(folderPath string) ([]string, error) {
	var filePaths []string
	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isImageFile(d.Name()) {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cannot walk folder %s: %w", folderPath, err)
	}
	return filePaths, nil
}

// collectImagesFlat lists image files in a single directory (non-recursive).
func collectImagesFlat(folderPath string) ([]string, error) {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read folder %s: %w", folderPath, err)
	}
	var filePaths []string
	for _, entry := range entries {
		if !entry.IsDir() && isImageFile(entry.Name()) {
			filePaths = append(filePaths, filepath.Join(folderPath, entry.Name()))
		}
	}
	return filePaths, nil
}

func collectImageFiles(folderPaths []string, recursive bool) ([]string, error) {
	var filePaths []string
	for _, folderPath := range folderPaths {
		paths, err := collectImagesFromFolder(folderPath, recursive)
		if err != nil {
			return nil, err
		}
		filePaths = append(filePaths, paths...)
	}
	return filePaths, nil
}

// newUploadProgressBar creates a progress bar for upload or processing.
func newUploadProgressBar(total int, description string) *progressbar.ProgressBar {
	return progressbar.NewOptions(total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("files"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

// uploadFiles uploads files one by one and returns tokens and errors.
func uploadFiles(pp *photoprism.PhotoPrism, filePaths []string) ([]string, []string) {
	bar := newUploadProgressBar(len(filePaths), "Uploading")

	var uploadTokens []string
	var uploadErrors []string
	for _, filePath := range filePaths {
		fileName := filepath.Base(filePath)
		token, err := pp.UploadFile(filePath)
		if err != nil {
			uploadErrors = append(uploadErrors, fmt.Sprintf("%s: %v", fileName, err))
			bar.Add(1)
			continue
		}
		uploadTokens = append(uploadTokens, token)
		bar.Add(1)
	}
	fmt.Println()
	return uploadTokens, uploadErrors
}

// processUploads processes upload tokens in parallel and adds to album.
func processUploads(pp *photoprism.PhotoPrism, uploadTokens []string, albumUID string) []string {
	bar := newUploadProgressBar(len(uploadTokens), "Processing")

	var (
		processErrors []string
		errorsMu      sync.Mutex
		wg            sync.WaitGroup
		sem           = make(chan struct{}, 8)
	)

	for _, token := range uploadTokens {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := pp.ProcessUpload(t, []string{albumUID}); err != nil {
				errorsMu.Lock()
				processErrors = append(processErrors, fmt.Sprintf("upload %s: %v", t, err))
				errorsMu.Unlock()
			}
			bar.Add(1)
		}(token)
	}
	wg.Wait()
	fmt.Println()
	return processErrors
}

// getAlbumPhotoUIDs fetches all photo UIDs in an album as a set.
func getAlbumPhotoUIDs(pp *photoprism.PhotoPrism, albumUID string) (map[string]struct{}, error) {
	photos, err := fetchAllAlbumPhotos(pp, albumUID, constants.DefaultPageSize)
	if err != nil {
		return nil, err
	}
	uids := make(map[string]struct{}, len(photos))
	for _, p := range photos {
		uids[p.UID] = struct{}{}
	}
	return uids, nil
}

// applyLabelsToPhotos applies the given labels to all specified photos with parallel concurrency.
func applyLabelsToPhotos(pp *photoprism.PhotoPrism, photoUIDs []string, labels []string) {
	totalOps := len(photoUIDs) * len(labels)
	bar := newUploadProgressBar(totalOps, "Applying labels")

	var (
		labelErrors []string
		errorsMu    sync.Mutex
		wg          sync.WaitGroup
		sem         = make(chan struct{}, 8)
	)

	for _, uid := range photoUIDs {
		for _, label := range labels {
			wg.Add(1)
			go func(photoUID, labelName string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				_, err := pp.AddPhotoLabel(photoUID, photoprism.PhotoLabel{
					Name:        labelName,
					LabelSrc:    "manual",
					Uncertainty: 0,
				})
				if err != nil {
					errorsMu.Lock()
					labelErrors = append(labelErrors, fmt.Sprintf("%s/%s: %v", photoUID, labelName, err))
					errorsMu.Unlock()
				}
				bar.Add(1)
			}(uid, label)
		}
	}
	wg.Wait()
	fmt.Println()

	for _, errMsg := range labelErrors {
		fmt.Printf("Warning: failed to apply label %s\n", errMsg)
	}
}

func runUpload(cmd *cobra.Command, args []string) error {
	albumUID := args[0]
	folderPaths := args[1:]
	recursive := mustGetBool(cmd, "recursive")
	labels := mustGetStringSlice(cmd, "label")

	cfg := config.Load()

	filePaths, err := collectImageFiles(folderPaths, recursive)
	if err != nil {
		return err
	}

	if len(filePaths) == 0 {
		fmt.Println("No image files found in the specified folders.")
		return nil
	}

	fmt.Printf("Found %d image(s) to upload from %d folder(s)\n", len(filePaths), len(folderPaths))

	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}
	fmt.Printf("Uploading to album: %s\n\n", album.Title)

	// Snapshot album photo UIDs before upload (for label diffing)
	var beforeUIDs map[string]struct{}
	if len(labels) > 0 {
		beforeUIDs, err = getAlbumPhotoUIDs(pp, albumUID)
		if err != nil {
			return fmt.Errorf("failed to snapshot album photos: %w", err)
		}
	}

	uploadTokens, uploadErrors := uploadFiles(pp, filePaths)
	for _, errMsg := range uploadErrors {
		fmt.Printf("Failed: %s\n", errMsg)
	}

	if len(uploadTokens) == 0 {
		return errors.New("no files were uploaded successfully")
	}

	fmt.Printf("\nProcessing %d upload(s)...\n", len(uploadTokens))
	processErrors := processUploads(pp, uploadTokens, albumUID)
	for _, errMsg := range processErrors {
		fmt.Printf("Warning: failed to process %s\n", errMsg)
	}

	// Apply labels to newly uploaded photos
	if len(labels) > 0 {
		afterUIDs, err := getAlbumPhotoUIDs(pp, albumUID)
		if err != nil {
			return fmt.Errorf("failed to snapshot album photos after upload: %w", err)
		}

		var newUIDs []string
		for uid := range afterUIDs {
			if _, existed := beforeUIDs[uid]; !existed {
				newUIDs = append(newUIDs, uid)
			}
		}

		if len(newUIDs) > 0 {
			fmt.Printf("\nApplying %d label(s) to %d new photo(s)...\n", len(labels), len(newUIDs))
			applyLabelsToPhotos(pp, newUIDs, labels)
		} else {
			fmt.Println("\nNo new photos detected in album; skipping label application.")
		}
	}

	fmt.Printf("\nDone! Uploaded %d file(s) to album '%s'\n", len(uploadTokens), album.Title)
	return nil
}
