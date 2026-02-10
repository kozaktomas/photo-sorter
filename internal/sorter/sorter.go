package sorter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

type Sorter struct {
	photoprism *photoprism.PhotoPrism
	aiProvider ai.Provider
}

// ProgressInfo contains progress information for callbacks
type ProgressInfo struct {
	Phase           string // "analyzing", "applying", "estimating_date"
	Current         int
	Total           int
	PhotoUID        string
	Message         string
}

type SortOptions struct {
	DryRun          bool
	Limit           int
	IndividualDates bool // Estimate date per photo instead of album-wide
	BatchMode       bool // Use batch API for 50% cost savings
	ForceDate       bool // Overwrite existing dates with AI estimates
	Concurrency     int  // Number of parallel requests in standard mode
	OnProgress      func(ProgressInfo) // Optional progress callback for web UI
}

type SortResult struct {
	ProcessedCount int
	SortedCount    int
	AlbumDate      string
	DateReasoning  string
	Errors         []error
	Suggestions    []ai.SortSuggestion
}

func New(pp *photoprism.PhotoPrism, aiProvider ai.Provider) *Sorter {
	return &Sorter{
		photoprism: pp,
		aiProvider: aiProvider,
	}
}

// photoToMetadata converts a PhotoPrism photo to AI metadata
// If clearDate is true, date fields are cleared so AI won't use them as reference
func photoToMetadata(photo photoprism.Photo, clearDate bool) *ai.PhotoMetadata {
	meta := &ai.PhotoMetadata{
		OriginalName: photo.OriginalName,
		FileName:     photo.FileName,
		TakenAt:      photo.TakenAt,
		Year:         photo.Year,
		Month:        photo.Month,
		Day:          photo.Day,
		Country:      photo.Country,
		Lat:          photo.Lat,
		Lng:          photo.Lng,
		Width:        photo.Width,
		Height:       photo.Height,
	}
	if clearDate {
		meta.TakenAt = ""
		meta.Year = 0
		meta.Month = 0
		meta.Day = 0
	}
	return meta
}

func (s *Sorter) Sort(ctx context.Context, albumUID string, albumTitle string, albumDescription string, opts SortOptions) (*SortResult, error) {
	if opts.BatchMode {
		return s.sortBatch(ctx, albumUID, albumTitle, albumDescription, opts)
	}
	return s.sortImmediate(ctx, albumUID, albumTitle, albumDescription, opts)
}

// photoResult holds the result of processing a single photo
type photoResult struct {
	index      int
	suggestion *ai.SortSuggestion
	err        error
}

func (s *Sorter) sortImmediate(ctx context.Context, albumUID string, albumTitle string, albumDescription string, opts SortOptions) (*SortResult, error) {
	result := &SortResult{}

	// Fetch available labels from PhotoPrism
	labels, err := s.photoprism.GetLabels(10000, 0, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch labels: %w", err)
	}

	availableLabels := make([]string, len(labels))
	for i, label := range labels {
		availableLabels[i] = label.Name
	}

	// Fetch photos from album
	limit := opts.Limit
	if limit == 0 {
		limit = 10000
	}

	photos, err := s.photoprism.GetAlbumPhotos(albumUID, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch photos: %w", err)
	}

	// Set concurrency (default to 5 if not set)
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	// Analyze all photos for labels and descriptions (with concurrency)
	bar := progressbar.NewOptions(len(photos),
		progressbar.OptionSetDescription(fmt.Sprintf("Analyzing photos (%d workers)", concurrency)),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
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

	// Create channels for work distribution and results
	resultsChan := make(chan photoResult, len(photos))
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var processedCount int
	var progressMu sync.Mutex

	// Helper to report progress
	reportProgress := func(photoUID string) {
		progressMu.Lock()
		processedCount++
		current := processedCount
		progressMu.Unlock()
		bar.Add(1)
		if opts.OnProgress != nil {
			opts.OnProgress(ProgressInfo{
				Phase:    "analyzing",
				Current:  current,
				Total:    len(photos),
				PhotoUID: photoUID,
			})
		}
	}

	// Process photos concurrently
	for i := range photos {
		wg.Add(1)
		go func(idx int, p photoprism.Photo) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check if context is cancelled
			if ctx.Err() != nil {
				resultsChan <- photoResult{index: idx, err: ctx.Err()}
				reportProgress(p.UID)
				return
			}

			// Download photo
			imageData, _, err := s.photoprism.GetPhotoDownload(p.UID)
			if err != nil {
				resultsChan <- photoResult{index: idx, err: fmt.Errorf("failed to download photo %s: %w", p.UID, err)}
				reportProgress(p.UID)
				return
			}

			// Analyze photo
			metadata := photoToMetadata(p, opts.ForceDate)
			analysis, err := s.aiProvider.AnalyzePhoto(ctx, imageData, metadata, availableLabels, opts.IndividualDates)
			if err != nil {
				resultsChan <- photoResult{index: idx, err: fmt.Errorf("failed to analyze photo %s: %w", p.UID, err)}
				reportProgress(p.UID)
				return
			}

			suggestion := &ai.SortSuggestion{
				PhotoUID:      p.UID,
				Labels:        analysis.Labels,
				Description:   analysis.Description,
				EstimatedDate: analysis.EstimatedDate,
			}
			resultsChan <- photoResult{index: idx, suggestion: suggestion}
			reportProgress(p.UID)
		}(i, photos[i])
	}

	// Wait for all goroutines to complete and close results channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results maintaining order
	results := make([]*photoResult, len(photos))
	for r := range resultsChan {
		results[r.index] = &r
	}
	fmt.Println() // New line after progress bar

	// Process results in order
	var photoDescriptions []string
	for i, r := range results {
		result.ProcessedCount++
		if r == nil {
			result.Errors = append(result.Errors, fmt.Errorf("no result for photo at index %d", i))
			continue
		}
		if r.err != nil {
			result.Errors = append(result.Errors, r.err)
			continue
		}
		if r.suggestion != nil {
			photoDescriptions = append(photoDescriptions, r.suggestion.Description)
			result.Suggestions = append(result.Suggestions, *r.suggestion)
		}
	}

	// Estimate album-wide date if not using individual dates
	if !opts.IndividualDates && len(photoDescriptions) > 0 {
		dateEstimate, err := s.aiProvider.EstimateAlbumDate(ctx, albumTitle, albumDescription, photoDescriptions)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to estimate album date: %w", err))
		} else {
			result.AlbumDate = dateEstimate.EstimatedDate
			result.DateReasoning = dateEstimate.Reasoning

			// Apply the same date to all suggestions
			for i := range result.Suggestions {
				result.Suggestions[i].EstimatedDate = dateEstimate.EstimatedDate
			}
		}
	}

	// Apply changes if not dry run
	if !opts.DryRun {
		// Build a map of photo UID to photo for applying results
		photoMap := make(map[string]photoprism.Photo)
		for i := range photos {
			photoMap[photos[i].UID] = photos[i]
		}

		for _, suggestion := range result.Suggestions {
			photo, ok := photoMap[suggestion.PhotoUID]
			if !ok {
				result.Errors = append(result.Errors, fmt.Errorf("photo not found: %s", suggestion.PhotoUID))
				continue
			}
			if err := s.applySorting(photo, suggestion, opts.ForceDate); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to apply sorting for %s: %w", photo.UID, err))
				continue
			}
			result.SortedCount++
		}
	} else {
		result.SortedCount = len(result.Suggestions)
	}

	return result, nil
}

func (s *Sorter) sortBatch(ctx context.Context, albumUID string, albumTitle string, albumDescription string, opts SortOptions) (*SortResult, error) {
	result := &SortResult{}

	// Fetch available labels from PhotoPrism
	labels, err := s.photoprism.GetLabels(10000, 0, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch labels: %w", err)
	}

	availableLabels := make([]string, len(labels))
	for i, label := range labels {
		availableLabels[i] = label.Name
	}

	// Fetch photos from album
	limit := opts.Limit
	if limit == 0 {
		limit = 10000
	}

	photos, err := s.photoprism.GetAlbumPhotos(albumUID, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch photos: %w", err)
	}

	fmt.Printf("Downloading %d photos for batch processing...\n", len(photos))

	// Download all photos and prepare batch requests
	bar := progressbar.NewOptions(len(photos),
		progressbar.OptionSetDescription("Downloading photos"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("photos"),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	var batchRequests []ai.BatchPhotoRequest
	photoMap := make(map[string]photoprism.Photo)

	for i := range photos {
		imageData, _, err := s.photoprism.GetPhotoDownload(photos[i].UID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to download photo %s: %w", photos[i].UID, err))
			bar.Add(1)
			continue
		}

		batchRequests = append(batchRequests, ai.BatchPhotoRequest{
			PhotoUID:        photos[i].UID,
			ImageData:       imageData,
			Metadata:        photoToMetadata(photos[i], opts.ForceDate),
			AvailableLabels: availableLabels,
			EstimateDate:    opts.IndividualDates,
		})
		photoMap[photos[i].UID] = photos[i]
		bar.Add(1)
	}
	fmt.Println()

	if len(batchRequests) == 0 {
		return nil, errors.New("no photos to process")
	}

	// Create batch
	fmt.Println("Creating batch job...")
	batchID, err := s.aiProvider.CreatePhotoBatch(ctx, batchRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to create batch: %w", err)
	}
	fmt.Printf("Batch created: %s\n", batchID)

	// Poll for completion
	fmt.Println("Waiting for batch to complete (this may take a few minutes)...")
	fmt.Println("Press Ctrl+C to cancel the batch job...")
	pollBar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription("Processing"),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
	)

	for {
		// Check if context was cancelled (Ctrl+C)
		select {
		case <-ctx.Done():
			fmt.Println("\n\nCancelling batch job...")
			if err := s.aiProvider.CancelBatch(context.Background(), batchID); err != nil {
				fmt.Printf("Warning: failed to cancel batch: %v\n", err)
			} else {
				fmt.Println("Batch job cancelled successfully.")
			}
			return nil, ctx.Err()
		default:
		}

		status, err := s.aiProvider.GetBatchStatus(ctx, batchID)
		if err != nil {
			// Check if it was a cancellation
			if ctx.Err() != nil {
				fmt.Println("\n\nCancelling batch job...")
				if cancelErr := s.aiProvider.CancelBatch(context.Background(), batchID); cancelErr != nil {
					fmt.Printf("Warning: failed to cancel batch: %v\n", cancelErr)
				} else {
					fmt.Println("Batch job cancelled successfully.")
				}
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("failed to get batch status: %w", err)
		}

		pollBar.Describe(fmt.Sprintf("Status: %s (%d/%d completed)", status.Status, status.CompletedCount, status.TotalRequests))
		pollBar.Add(1)

		if status.Status == "completed" || status.Status == "JOB_STATE_SUCCEEDED" {
			fmt.Println("\nBatch completed!")
			break
		} else if status.Status == "failed" || status.Status == "expired" || status.Status == "cancelled" ||
			status.Status == "JOB_STATE_FAILED" || status.Status == "JOB_STATE_CANCELLED" {
			return nil, fmt.Errorf("batch failed with status: %s", status.Status)
		}

		time.Sleep(5 * time.Second)
	}

	// Get results
	fmt.Println("Downloading results...")
	batchResults, err := s.aiProvider.GetBatchResults(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch results: %w", err)
	}

	// Process results
	var photoDescriptions []string
	for _, batchResult := range batchResults {
		result.ProcessedCount++

		if batchResult.Error != "" {
			result.Errors = append(result.Errors, fmt.Errorf("analysis failed for %s: %s", batchResult.PhotoUID, batchResult.Error))
			continue
		}

		if batchResult.Analysis == nil {
			result.Errors = append(result.Errors, fmt.Errorf("no analysis for %s", batchResult.PhotoUID))
			continue
		}

		photoDescriptions = append(photoDescriptions, batchResult.Analysis.Description)

		suggestion := ai.SortSuggestion{
			PhotoUID:      batchResult.PhotoUID,
			Labels:        batchResult.Analysis.Labels,
			Description:   batchResult.Analysis.Description,
			EstimatedDate: batchResult.Analysis.EstimatedDate,
		}
		result.Suggestions = append(result.Suggestions, suggestion)
	}

	// Estimate album-wide date if not using individual dates
	if !opts.IndividualDates && len(photoDescriptions) > 0 {
		fmt.Println("Estimating album date...")
		dateEstimate, err := s.aiProvider.EstimateAlbumDate(ctx, albumTitle, albumDescription, photoDescriptions)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to estimate album date: %w", err))
		} else {
			result.AlbumDate = dateEstimate.EstimatedDate
			result.DateReasoning = dateEstimate.Reasoning

			// Apply the same date to all suggestions
			for i := range result.Suggestions {
				result.Suggestions[i].EstimatedDate = dateEstimate.EstimatedDate
			}
		}
	}

	// Apply changes if not dry run
	if !opts.DryRun {
		fmt.Println("Applying changes to PhotoPrism...")
		applyBar := progressbar.NewOptions(len(result.Suggestions),
			progressbar.OptionSetDescription("Applying"),
			progressbar.OptionShowCount(),
			progressbar.OptionFullWidth(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
		)

		for _, suggestion := range result.Suggestions {
			photo, ok := photoMap[suggestion.PhotoUID]
			if !ok {
				result.Errors = append(result.Errors, fmt.Errorf("photo not found: %s", suggestion.PhotoUID))
				applyBar.Add(1)
				continue
			}
			if err := s.applySorting(photo, suggestion, opts.ForceDate); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to apply sorting for %s: %w", photo.UID, err))
				applyBar.Add(1)
				continue
			}
			result.SortedCount++
			applyBar.Add(1)
		}
		fmt.Println()
	} else {
		result.SortedCount = len(result.Suggestions)
	}

	return result, nil
}

func (s *Sorter) applySorting(photo photoprism.Photo, suggestion ai.SortSuggestion, forceDate bool) error {
	// Remove all existing labels first
	if err := s.photoprism.RemoveAllPhotoLabels(photo.UID); err != nil {
		return fmt.Errorf("failed to remove existing labels: %w", err)
	}

	// Add labels to photo (only if confidence > 80%)
	for _, label := range suggestion.Labels {
		if label.Confidence < 0.8 {
			continue // Skip low confidence labels
		}
		// Convert confidence to uncertainty (0-100 scale, where 0 = certain, 100 = uncertain)
		uncertainty := int((1 - label.Confidence) * 100)
		_, err := s.photoprism.AddPhotoLabel(photo.UID, photoprism.PhotoLabel{
			Name:        label.Name,
			LabelSrc:    "manual",
			Uncertainty: uncertainty,
		})
		if err != nil {
			return fmt.Errorf("failed to add label %s: %w", label.Name, err)
		}
	}

	// Build update with description, notes, and optional date
	update := photoprism.PhotoUpdate{}

	// Set description with model info
	desc := suggestion.Description
	if desc != "" {
		desc = fmt.Sprintf("%s\n\nAI_MODEL: %s", desc, s.aiProvider.Name())
	} else {
		desc = "AI_MODEL: " + s.aiProvider.Name()
	}
	update.Description = &desc
	descSrc := "manual"
	update.DescriptionSrc = &descSrc

	// Set notes with model info
	notes := "Analyzed by: " + s.aiProvider.Name()
	update.Details = &photoprism.PhotoDetails{
		Notes: &notes,
	}

	// Update taken date only if photo doesn't already have a valid date
	// (preserve existing metadata, only fill in missing dates)
	// Unless forceDate is true - then always overwrite
	// Also force date update for scanned photos (scanner dates are unreliable)
	photoHasDate := photo.Year > 0 && photo.Year != 1
	cameraLower := strings.ToLower(photo.CameraModel)
	isScannedPhoto := photo.Scan || strings.Contains(cameraLower, "scanjet") || strings.Contains(cameraLower, "scanner")
	shouldUpdateDate := forceDate || !photoHasDate || isScannedPhoto
	if shouldUpdateDate && suggestion.EstimatedDate != "" && suggestion.EstimatedDate != "0001-01-01" {
		// Parse the date and set it as local time in Europe/Prague
		prague, _ := time.LoadLocation("Europe/Prague")
		localTime, err := time.ParseInLocation("2006-01-02", suggestion.EstimatedDate, prague)
		if err != nil {
			return fmt.Errorf("failed to parse date %s: %w", suggestion.EstimatedDate, err)
		}
		// Set to noon local time
		localTime = localTime.Add(12 * time.Hour)

		// Format for PhotoPrism
		takenAtLocal := localTime.Format("2006-01-02T15:04:05Z")
		takenAt := localTime.UTC().Format("2006-01-02T15:04:05Z")
		timeZone := "Europe/Prague"
		year := localTime.Year()
		month := int(localTime.Month())
		day := localTime.Day()

		update.TakenAt = &takenAt
		update.TakenAtLocal = &takenAtLocal
		update.TimeZone = &timeZone
		update.Year = &year
		update.Month = &month
		update.Day = &day
	}

	// Apply updates
	_, err := s.photoprism.EditPhoto(photo.UID, update)
	if err != nil {
		return fmt.Errorf("failed to update photo: %w", err)
	}

	return nil
}
