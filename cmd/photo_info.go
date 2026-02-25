package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var photoInfoCmd = &cobra.Command{
	Use:   "info [photo-uid]",
	Short: "Display photo information including perceptual hashes",
	Long: `Display detailed information about a photo including metadata and
perceptual hashes (pHash and dHash) for similarity matching.

Examples:
  # Get info for a single photo
  photo-sorter photo info pq8abc123def

  # Get info for all photos in an album
  photo-sorter photo info --album aq8xyz789ghi

  # Output as JSON
  photo-sorter photo info --json pq8abc123def

  # Process album with limited concurrency
  photo-sorter photo info --album aq8xyz789ghi --concurrency 3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPhotoInfo,
}

func init() {
	photoCmd.AddCommand(photoInfoCmd)

	photoInfoCmd.Flags().String("album", "", "Process all photos in an album")
	photoInfoCmd.Flags().Bool("json", false, "Output as JSON")
	photoInfoCmd.Flags().Int("limit", 0, "Limit number of photos (0 = no limit)")
	photoInfoCmd.Flags().Int("concurrency", 5, "Number of parallel workers")
}

func runPhotoInfo(cmd *cobra.Command, args []string) error {
	albumUID := mustGetString(cmd, "album")
	jsonOutput := mustGetBool(cmd, "json")
	limit := mustGetInt(cmd, "limit")
	concurrency := mustGetInt(cmd, "concurrency")

	// Validate args
	if albumUID == "" && len(args) == 0 {
		return errors.New("either provide a photo UID or use --album flag")
	}
	if albumUID != "" && len(args) > 0 {
		return errors.New("cannot specify both photo UID and --album flag")
	}

	cfg := config.Load()

	// Connect to PhotoPrism
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL,
		cfg.PhotoPrism.Username,
		cfg.PhotoPrism.GetPassword(),
		captureDir,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	if albumUID != "" {
		return runPhotoInfoAlbum(pp, albumUID, limit, concurrency, jsonOutput, &cfg.PhotoPrism)
	}
	return runPhotoInfoSingle(pp, args[0], jsonOutput, &cfg.PhotoPrism)
}

func runPhotoInfoSingle(pp *photoprism.PhotoPrism, photoUID string, jsonOutput bool, ppCfg *config.PhotoPrismConfig) error {
	// Get photo metadata
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return fmt.Errorf("failed to get photo details: %w", err)
	}

	// Download photo for hash computation
	imageData, _, err := pp.GetPhotoDownload(photoUID)
	if err != nil {
		return fmt.Errorf("failed to download photo: %w", err)
	}

	// Compute hashes
	hashes, err := fingerprint.ComputeHashes(imageData)
	if err != nil {
		return fmt.Errorf("failed to compute hashes: %w", err)
	}

	// Build PhotoInfo from details map
	info := buildPhotoInfo(details, hashes)

	if jsonOutput {
		return outputJSON(info)
	}

	return outputHumanReadableSingle(info, ppCfg)
}

// processPhotoHash downloads a photo and computes its hashes, returning the result.
func processPhotoHash(pp *photoprism.PhotoPrism, p photoprism.Photo) (fingerprint.PhotoInfo, error) {
	imageData, _, err := pp.GetPhotoDownload(p.UID)
	if err != nil {
		return fingerprint.PhotoInfo{}, fmt.Errorf("photo %s: %w", p.UID, err)
	}

	hashes, err := fingerprint.ComputeHashes(imageData)
	if err != nil {
		return fingerprint.PhotoInfo{}, fmt.Errorf("photo %s: %w", p.UID, err)
	}

	return buildPhotoInfoFromPhoto(p, hashes), nil
}

// processPhotosConcurrently processes photos with workers and returns results and errors.
func processPhotosConcurrently(pp *photoprism.PhotoPrism, photos []photoprism.Photo, concurrency int, bar *progressbar.ProgressBar) ([]fingerprint.PhotoInfo, []error) {
	results := make([]fingerprint.PhotoInfo, len(photos))
	var errs []error
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range photos {
		wg.Add(1)
		go func(idx int, p photoprism.Photo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := processPhotoHash(pp, p)
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			} else {
				mu.Lock()
				results[idx] = info
				mu.Unlock()
			}

			if bar != nil {
				bar.Add(1)
			}
		}(i, photos[i])
	}
	wg.Wait()

	// Filter out empty results (from errors)
	validResults := make([]fingerprint.PhotoInfo, 0, len(results))
	for i := range results {
		if results[i].UID != "" {
			validResults = append(validResults, results[i])
		}
	}
	return validResults, errs
}

// newHashProgressBar creates a progress bar for hash computation, or nil if JSON output.
func newHashProgressBar(count int, jsonOutput bool) *progressbar.ProgressBar {
	if jsonOutput {
		return nil
	}
	return progressbar.NewOptions(count,
		progressbar.OptionSetDescription("Computing hashes"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)
}

// outputInfoAlbumResults outputs the batch info results as JSON or human-readable with errors.
func outputInfoAlbumResults(validResults []fingerprint.PhotoInfo, errs []error, jsonOutput bool, ppCfg *config.PhotoPrismConfig) error {
	if jsonOutput {
		return outputJSON(fingerprint.PhotoInfoBatch{Photos: validResults, Count: len(validResults)})
	}

	outputHumanReadableBatch(validResults, ppCfg)
	if len(errs) > 0 {
		fmt.Printf("\nErrors: %d\n", len(errs))
		for _, e := range errs {
			fmt.Printf("  - %v\n", e)
		}
	}
	return nil
}

func runPhotoInfoAlbum(pp *photoprism.PhotoPrism, albumUID string, limit, concurrency int, jsonOutput bool, ppCfg *config.PhotoPrismConfig) error {
	album, err := pp.GetAlbum(albumUID)
	if err != nil {
		return fmt.Errorf("failed to get album: %w", err)
	}

	if limit == 0 {
		limit = 10000
	}
	photos, err := pp.GetAlbumPhotos(albumUID, limit, 0)
	if err != nil {
		return fmt.Errorf("failed to get album photos: %w", err)
	}

	if len(photos) == 0 {
		if jsonOutput {
			return outputJSON(fingerprint.PhotoInfoBatch{Photos: []fingerprint.PhotoInfo{}, Count: 0})
		}
		fmt.Println("No photos found in album.")
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Album: %s\n", album.Title)
		fmt.Printf("Processing %d photos...\n\n", len(photos))
	}

	bar := newHashProgressBar(len(photos), jsonOutput)
	validResults, errs := processPhotosConcurrently(pp, photos, concurrency, bar)
	if bar != nil {
		fmt.Println()
	}

	return outputInfoAlbumResults(validResults, errs, jsonOutput, ppCfg)
}

// detailsString extracts a string field from a details map.
func detailsString(details map[string]any, key string) string {
	if v, ok := details[key].(string); ok {
		return v
	}
	return ""
}

// detailsInt extracts a float64 field from a details map and returns it as int.
func detailsInt(details map[string]any, key string) int {
	if v, ok := details[key].(float64); ok {
		return int(v)
	}
	return 0
}

// detailsFloat64 extracts a float64 field from a details map.
func detailsFloat64(details map[string]any, key string) float64 {
	if v, ok := details[key].(float64); ok {
		return v
	}
	return 0
}

func buildPhotoInfo(details map[string]any, hashes *fingerprint.HashResult) fingerprint.PhotoInfo {
	return fingerprint.PhotoInfo{
		UID:          detailsString(details, "UID"),
		OriginalName: detailsString(details, "OriginalName"),
		FileName:     detailsString(details, "FileName"),
		Width:        detailsInt(details, "Width"),
		Height:       detailsInt(details, "Height"),
		TakenAt:      detailsString(details, "TakenAt"),
		Year:         detailsInt(details, "Year"),
		Month:        detailsInt(details, "Month"),
		Day:          detailsInt(details, "Day"),
		Lat:          detailsFloat64(details, "Lat"),
		Lng:          detailsFloat64(details, "Lng"),
		Country:      detailsString(details, "Country"),
		CameraModel:  detailsString(details, "CameraModel"),
		Hash:         detailsString(details, "Hash"),
		Title:        detailsString(details, "Title"),
		Description:  detailsString(details, "Description"),
		PHash:        hashes.PHash,
		DHash:        hashes.DHash,
		PHashBits:    hashes.PHashBits,
		DHashBits:    hashes.DHashBits,
		ComputedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

func buildPhotoInfoFromPhoto(p photoprism.Photo, hashes *fingerprint.HashResult) fingerprint.PhotoInfo {
	return fingerprint.PhotoInfo{
		UID:          p.UID,
		OriginalName: p.OriginalName,
		FileName:     p.FileName,
		Width:        p.Width,
		Height:       p.Height,
		TakenAt:      p.TakenAt,
		Year:         p.Year,
		Month:        p.Month,
		Day:          p.Day,
		Lat:          p.Lat,
		Lng:          p.Lng,
		Country:      p.Country,
		CameraModel:  p.CameraModel,
		Hash:         p.Hash,
		Title:        p.Title,
		Description:  p.Description,
		PHash:        hashes.PHash,
		DHash:        hashes.DHash,
		PHashBits:    hashes.PHashBits,
		DHashBits:    hashes.DHashBits,
		ComputedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

func outputJSON(data any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encoding JSON output: %w", err)
	}
	return nil
}

// printPhotoMetadata prints the metadata section of photo info.
func printPhotoMetadata(info fingerprint.PhotoInfo) {
	fmt.Println("\nMetadata:")
	if info.OriginalName != "" {
		fmt.Printf("  Original Name:  %s\n", info.OriginalName)
	}
	if info.FileName != "" {
		fmt.Printf("  File Name:      %s\n", info.FileName)
	}
	if info.Width > 0 && info.Height > 0 {
		fmt.Printf("  Dimensions:     %d x %d\n", info.Width, info.Height)
	}
	if info.CameraModel != "" {
		fmt.Printf("  Camera:         %s\n", info.CameraModel)
	}
}

// printPhotoDates prints the dates section of photo info.
func printPhotoDates(info fingerprint.PhotoInfo) {
	if info.TakenAt == "" && info.Year == 0 {
		return
	}
	fmt.Println("\nDates:")
	if info.TakenAt != "" {
		fmt.Printf("  Taken At:       %s\n", info.TakenAt)
	}
	if info.Year > 0 {
		fmt.Printf("  Year/Month/Day: %d / %02d / %02d\n", info.Year, info.Month, info.Day)
	}
}

// printPhotoLocation prints the location section of photo info.
func printPhotoLocation(info fingerprint.PhotoInfo) {
	if info.Lat == 0 && info.Lng == 0 && info.Country == "" {
		return
	}
	fmt.Println("\nLocation:")
	if info.Lat != 0 || info.Lng != 0 {
		fmt.Printf("  GPS:            %.6f, %.6f\n", info.Lat, info.Lng)
	}
	if info.Country != "" && info.Country != "zz" {
		fmt.Printf("  Country:        %s\n", info.Country)
	}
}

func outputHumanReadableSingle(info fingerprint.PhotoInfo, ppCfg *config.PhotoPrismConfig) error {
	if url := ppCfg.PhotoURL(info.UID); url != "" {
		fmt.Printf("Photo: %s\n", url)
	} else {
		fmt.Printf("Photo: %s\n", info.UID)
	}
	fmt.Println("────────────────────────────────────────")

	printPhotoMetadata(info)
	printPhotoDates(info)
	printPhotoLocation(info)

	fmt.Println("\nHashes:")
	fmt.Printf("  pHash:          %s\n", info.PHash)
	fmt.Printf("  dHash:          %s\n", info.DHash)
	if info.Hash != "" {
		fmt.Printf("  PhotoPrism:     %s\n", info.Hash)
	}

	return nil
}

func outputHumanReadableBatch(results []fingerprint.PhotoInfo, ppCfg *config.PhotoPrismConfig) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PHOTO\tDIMENSIONS\tTAKEN\tPHASH\tDHASH")
	fmt.Fprintln(w, "-----\t----------\t-----\t-----\t-----")

	for i := range results {
		info := &results[i]
		taken := ""
		if info.Year > 0 {
			taken = fmt.Sprintf("%d-%02d-%02d", info.Year, info.Month, info.Day)
		}
		dims := ""
		if info.Width > 0 && info.Height > 0 {
			dims = fmt.Sprintf("%dx%d", info.Width, info.Height)
		}
		photoRef := info.UID
		if url := ppCfg.PhotoURL(info.UID); url != "" {
			photoRef = url
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			photoRef, dims, taken, info.PHash, info.DHash)
	}

	w.Flush()
	fmt.Printf("\nTotal: %d photos\n", len(results))
}
