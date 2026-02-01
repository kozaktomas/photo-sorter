package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

var uploadCmd = &cobra.Command{
	Use:   "upload <album-uid> <folder-path> [folder-path...]",
	Short: "Upload photos to an album",
	Long: `Upload photos from one or more folders to a PhotoPrism album.

By default, only files in the specified folders are uploaded (non-recursive).
Use -r to search recursively in subdirectories.
Supported formats: jpg, jpeg, png, gif, heic, heif, webp, tiff, bmp, raw, cr2, nef, arw, dng

Example:
  photo-sorter upload aq8i4k2l3m9n0o1p /path/to/photos
  photo-sorter upload aq8i4k2l3m9n0o1p /path/to/folder1 /path/to/folder2 /path/to/folder3
  photo-sorter upload -r aq8i4k2l3m9n0o1p /path/to/photos  # recursive search`,
	Args: cobra.MinimumNArgs(2),
	RunE: runUpload,
}

func init() {
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().BoolP("recursive", "r", false, "Search for photos recursively in subdirectories")
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

func runUpload(cmd *cobra.Command, args []string) error {
	albumUID := args[0]
	folderPaths := args[1:]
	recursive := mustGetBool(cmd, "recursive")

	cfg := config.Load()

	// Collect files from all folders
	var filePaths []string
	for _, folderPath := range folderPaths {
		// Check if folder exists
		info, err := os.Stat(folderPath)
		if err != nil {
			return fmt.Errorf("cannot access folder %s: %w", folderPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", folderPath)
		}

		if recursive {
			// Walk directory recursively
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
				return fmt.Errorf("cannot walk folder %s: %w", folderPath, err)
			}
		} else {
			// List files in folder (non-recursive)
			entries, err := os.ReadDir(folderPath)
			if err != nil {
				return fmt.Errorf("cannot read folder %s: %w", folderPath, err)
			}

			// Filter image files
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if isImageFile(entry.Name()) {
					filePaths = append(filePaths, filepath.Join(folderPath, entry.Name()))
				}
			}
		}
	}

	if len(filePaths) == 0 {
		fmt.Println("No image files found in the specified folders.")
		return nil
	}

	fmt.Printf("Found %d image(s) to upload from %d folder(s)\n", len(filePaths), len(folderPaths))

	// Connect to PhotoPrism
	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Verify album exists
	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}
	fmt.Printf("Uploading to album: %s\n\n", album.Title)

	// Upload files one by one with progress bar
	uploadBar := progressbar.NewOptions(len(filePaths),
		progressbar.OptionSetDescription("Uploading"),
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

	var uploadTokens []string
	var uploadErrors []string
	for _, filePath := range filePaths {
		fileName := filepath.Base(filePath)

		token, err := pp.UploadFile(filePath)
		if err != nil {
			uploadErrors = append(uploadErrors, fmt.Sprintf("%s: %v", fileName, err))
			uploadBar.Add(1)
			continue
		}
		uploadTokens = append(uploadTokens, token)
		uploadBar.Add(1)
	}
	fmt.Println()

	// Print any upload errors
	for _, errMsg := range uploadErrors {
		fmt.Printf("Failed: %s\n", errMsg)
	}

	if len(uploadTokens) == 0 {
		return fmt.Errorf("no files were uploaded successfully")
	}

	// Process all uploads and add to album
	fmt.Printf("\nProcessing %d upload(s)...\n", len(uploadTokens))
	processBar := progressbar.NewOptions(len(uploadTokens),
		progressbar.OptionSetDescription("Processing"),
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

	var (
		processErrors []string
		errorsMu      sync.Mutex
		wg            sync.WaitGroup
		sem           = make(chan struct{}, 8) // 8 concurrent workers
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
			processBar.Add(1)
		}(token)
	}
	wg.Wait()
	fmt.Println()

	// Print any processing errors
	for _, errMsg := range processErrors {
		fmt.Printf("Warning: failed to process %s\n", errMsg)
	}

	fmt.Printf("\nDone! Uploaded %d file(s) to album '%s'\n", len(uploadTokens), album.Title)
	return nil
}
