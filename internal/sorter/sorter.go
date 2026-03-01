package sorter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/schollz/progressbar/v3"
)

// Sorter orchestrates photo fetching, AI analysis, and label application.
type Sorter struct {
	photoprism *photoprism.PhotoPrism
	aiProvider ai.Provider
}

// ProgressInfo contains progress information for callbacks.
type ProgressInfo struct {
	Phase    string // "analyzing", "applying", "estimating_date"
	Current  int
	Total    int
	PhotoUID string
	Message  string
}

// SortOptions configures the sort operation parameters.
type SortOptions struct {
	DryRun          bool
	Limit           int
	IndividualDates bool               // Estimate date per photo instead of album-wide
	BatchMode       bool               // Use batch API for 50% cost savings
	ForceDate       bool               // Overwrite existing dates with AI estimates
	Concurrency     int                // Number of parallel requests in standard mode
	OnProgress      func(ProgressInfo) // Optional progress callback for web UI
}

// SortResult holds the outcome of a sort operation.
type SortResult struct {
	ProcessedCount int
	SortedCount    int
	AlbumDate      string
	DateReasoning  string
	Errors         []error
	Suggestions    []ai.SortSuggestion
}

// New creates a new Sorter with the given PhotoPrism client, AI provider, and config.
func New(pp *photoprism.PhotoPrism, aiProvider ai.Provider) *Sorter {
	return &Sorter{
		photoprism: pp,
		aiProvider: aiProvider,
	}
}

// photoToMetadata converts a PhotoPrism photo to AI metadata.
// If clearDate is true, date fields are cleared so AI won't use them as reference.
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

// Sort analyzes photos in an album using AI and applies labels, descriptions, and dates.
func (s *Sorter) Sort(
	ctx context.Context, albumUID string, albumTitle string,
	albumDescription string, opts SortOptions,
) (*SortResult, error) {
	if opts.BatchMode {
		return s.sortBatch(ctx, albumUID, albumTitle, albumDescription, opts)
	}
	return s.sortImmediate(ctx, albumUID, albumTitle, albumDescription, opts)
}

// photoResult holds the result of processing a single photo.
type photoResult struct {
	index      int
	suggestion *ai.SortSuggestion
	err        error
}

// fetchLabelsAndPhotos fetches available labels and album photos for sorting.
func (s *Sorter) fetchLabelsAndPhotos(albumUID string, limit int) ([]string, []photoprism.Photo, error) {
	labels, err := s.photoprism.GetLabels(10000, 0, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch labels: %w", err)
	}
	availableLabels := make([]string, len(labels))
	for i, label := range labels {
		availableLabels[i] = label.Name
	}
	if limit == 0 {
		limit = 10000
	}
	photos, err := s.photoprism.GetAlbumPhotos(albumUID, limit, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch photos: %w", err)
	}
	return availableLabels, photos, nil
}

func newAnalysisProgressBar(total, concurrency int) *progressbar.ProgressBar {
	return progressbar.NewOptions(total,
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
}

func (s *Sorter) analyzeOnePhoto(
	ctx context.Context, idx int, p photoprism.Photo,
	availableLabels []string, opts SortOptions,
) photoResult {
	imageData, _, err := s.photoprism.GetPhotoDownload(p.UID)
	if err != nil {
		return photoResult{index: idx, err: fmt.Errorf("failed to download photo %s: %w", p.UID, err)}
	}

	metadata := photoToMetadata(p, opts.ForceDate)
	analysis, err := s.aiProvider.AnalyzePhoto(ctx, imageData, metadata, availableLabels, opts.IndividualDates)
	if err != nil {
		return photoResult{index: idx, err: fmt.Errorf("failed to analyze photo %s: %w", p.UID, err)}
	}

	return photoResult{index: idx, suggestion: &ai.SortSuggestion{
		PhotoUID:      p.UID,
		Labels:        analysis.Labels,
		Description:   analysis.Description,
		EstimatedDate: analysis.EstimatedDate,
	}}
}

// analyzePhotosParallel downloads and analyzes photos concurrently, returning ordered results.
func (s *Sorter) analyzePhotosParallel(
	ctx context.Context, photos []photoprism.Photo,
	availableLabels []string, opts SortOptions,
) []*photoResult {
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	bar := newAnalysisProgressBar(len(photos), concurrency)

	resultsChan := make(chan photoResult, len(photos))
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var processedCount int
	var progressMu sync.Mutex

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

	for i := range photos {
		wg.Add(1)
		go func(idx int, p photoprism.Photo) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if ctx.Err() != nil {
				resultsChan <- photoResult{index: idx, err: ctx.Err()}
			} else {
				resultsChan <- s.analyzeOnePhoto(ctx, idx, p, availableLabels, opts)
			}
			reportProgress(p.UID)
		}(i, photos[i])
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	results := make([]*photoResult, len(photos))
	for r := range resultsChan {
		results[r.index] = &r
	}
	fmt.Println()
	return results
}

// collectSuggestions processes ordered results into suggestions and descriptions.
func collectSuggestions(results []*photoResult, sortResult *SortResult) []string {
	var photoDescriptions []string
	for i, r := range results {
		sortResult.ProcessedCount++
		if r == nil {
			sortResult.Errors = append(sortResult.Errors, fmt.Errorf("no result for photo at index %d", i))
			continue
		}
		if r.err != nil {
			sortResult.Errors = append(sortResult.Errors, r.err)
			continue
		}
		if r.suggestion != nil {
			photoDescriptions = append(photoDescriptions, r.suggestion.Description)
			sortResult.Suggestions = append(sortResult.Suggestions, *r.suggestion)
		}
	}
	return photoDescriptions
}

// estimateAndApplyAlbumDate estimates album date and applies it to all suggestions.
func (s *Sorter) estimateAndApplyAlbumDate(
	ctx context.Context, result *SortResult,
	albumTitle, albumDescription string, photoDescriptions []string,
) {
	dateEstimate, err := s.aiProvider.EstimateAlbumDate(ctx, albumTitle, albumDescription, photoDescriptions)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to estimate album date: %w", err))
		return
	}
	result.AlbumDate = dateEstimate.EstimatedDate
	result.DateReasoning = dateEstimate.Reasoning
	for i := range result.Suggestions {
		result.Suggestions[i].EstimatedDate = dateEstimate.EstimatedDate
	}
}

// applySuggestions applies sorting suggestions to photos if not dry run.
func (s *Sorter) applySuggestions(result *SortResult, photoMap map[string]photoprism.Photo, forceDate bool) {
	for _, suggestion := range result.Suggestions {
		photo, ok := photoMap[suggestion.PhotoUID]
		if !ok {
			result.Errors = append(result.Errors, fmt.Errorf("photo not found: %s", suggestion.PhotoUID))
			continue
		}
		if err := s.applySorting(photo, suggestion, forceDate); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to apply sorting for %s: %w", photo.UID, err))
			continue
		}
		result.SortedCount++
	}
}

func (s *Sorter) sortImmediate(
	ctx context.Context, albumUID string, albumTitle string,
	albumDescription string, opts SortOptions,
) (*SortResult, error) {
	result := &SortResult{}

	availableLabels, photos, err := s.fetchLabelsAndPhotos(albumUID, opts.Limit)
	if err != nil {
		return nil, err
	}

	results := s.analyzePhotosParallel(ctx, photos, availableLabels, opts)
	photoDescriptions := collectSuggestions(results, result)

	if !opts.IndividualDates && len(photoDescriptions) > 0 {
		s.estimateAndApplyAlbumDate(ctx, result, albumTitle, albumDescription, photoDescriptions)
	}

	if !opts.DryRun {
		photoMap := make(map[string]photoprism.Photo)
		for i := range photos {
			photoMap[photos[i].UID] = photos[i]
		}
		s.applySuggestions(result, photoMap, opts.ForceDate)
	} else {
		result.SortedCount = len(result.Suggestions)
	}

	return result, nil
}

// downloadBatchPhotos downloads photos and prepares batch requests.
func (s *Sorter) downloadBatchPhotos(
	photos []photoprism.Photo, availableLabels []string,
	opts SortOptions, result *SortResult,
) ([]ai.BatchPhotoRequest, map[string]photoprism.Photo) {
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
	return batchRequests, photoMap
}

// cancelBatch attempts to cancel a batch job and returns the context error.
func (s *Sorter) cancelBatch(batchID string) {
	fmt.Println("\n\nCancelling batch job...")
	if err := s.aiProvider.CancelBatch(context.Background(), batchID); err != nil {
		fmt.Printf("Warning: failed to cancel batch: %v\n", err)
	} else {
		fmt.Println("Batch job cancelled successfully.")
	}
}

// pollBatchCompletion polls a batch job until completion or cancellation.
func (s *Sorter) pollBatchCompletion(ctx context.Context, batchID string) error {
	pollBar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription("Processing"),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
	)

	for {
		select {
		case <-ctx.Done():
			s.cancelBatch(batchID)
			return fmt.Errorf("sort cancelled: %w", ctx.Err())
		default:
		}

		status, err := s.aiProvider.GetBatchStatus(ctx, batchID)
		if err != nil {
			if ctx.Err() != nil {
				s.cancelBatch(batchID)
				return fmt.Errorf("sort cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("failed to get batch status: %w", err)
		}

		pollBar.Describe(fmt.Sprintf(
			"Status: %s (%d/%d completed)",
			status.Status, status.CompletedCount, status.TotalRequests,
		))
		pollBar.Add(1)

		switch status.Status {
		case "completed", "JOB_STATE_SUCCEEDED":
			fmt.Println("\nBatch completed!")
			return nil
		case "failed", "expired", "cancelled", "JOB_STATE_FAILED", "JOB_STATE_CANCELLED":
			return fmt.Errorf("batch failed with status: %s", status.Status)
		}

		time.Sleep(5 * time.Second)
	}
}

// collectBatchResults converts batch API results into suggestions and descriptions.
func collectBatchResults(batchResults []ai.BatchPhotoResult, result *SortResult) []string {
	var photoDescriptions []string
	for _, batchResult := range batchResults {
		result.ProcessedCount++
		if batchResult.Error != "" {
			result.Errors = append(result.Errors, fmt.Errorf(
				"analysis failed for %s: %s", batchResult.PhotoUID, batchResult.Error,
			))
			continue
		}
		if batchResult.Analysis == nil {
			result.Errors = append(result.Errors, fmt.Errorf("no analysis for %s", batchResult.PhotoUID))
			continue
		}
		photoDescriptions = append(photoDescriptions, batchResult.Analysis.Description)
		result.Suggestions = append(result.Suggestions, ai.SortSuggestion{
			PhotoUID:      batchResult.PhotoUID,
			Labels:        batchResult.Analysis.Labels,
			Description:   batchResult.Analysis.Description,
			EstimatedDate: batchResult.Analysis.EstimatedDate,
		})
	}
	return photoDescriptions
}

// applySuggestionsWithProgress applies suggestions with a progress bar.
func (s *Sorter) applySuggestionsWithProgress(
	result *SortResult, photoMap map[string]photoprism.Photo, forceDate bool,
) {
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
		if err := s.applySorting(photo, suggestion, forceDate); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to apply sorting for %s: %w", photo.UID, err))
			applyBar.Add(1)
			continue
		}
		result.SortedCount++
		applyBar.Add(1)
	}
	fmt.Println()
}

func (s *Sorter) sortBatch(
	ctx context.Context, albumUID string, albumTitle string,
	albumDescription string, opts SortOptions,
) (*SortResult, error) {
	result := &SortResult{}

	availableLabels, photos, err := s.fetchLabelsAndPhotos(albumUID, opts.Limit)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Downloading %d photos for batch processing...\n", len(photos))
	batchRequests, photoMap := s.downloadBatchPhotos(photos, availableLabels, opts, result)
	if len(batchRequests) == 0 {
		return nil, errors.New("no photos to process")
	}

	fmt.Println("Creating batch job...")
	batchID, err := s.aiProvider.CreatePhotoBatch(ctx, batchRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to create batch: %w", err)
	}
	fmt.Printf("Batch created: %s\n", batchID)

	fmt.Println("Waiting for batch to complete (this may take a few minutes)...")
	fmt.Println("Press Ctrl+C to cancel the batch job...")
	if err := s.pollBatchCompletion(ctx, batchID); err != nil {
		return nil, err
	}

	fmt.Println("Downloading results...")
	batchResults, err := s.aiProvider.GetBatchResults(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get batch results: %w", err)
	}

	photoDescriptions := collectBatchResults(batchResults, result)

	if !opts.IndividualDates && len(photoDescriptions) > 0 {
		fmt.Println("Estimating album date...")
		s.estimateAndApplyAlbumDate(ctx, result, albumTitle, albumDescription, photoDescriptions)
	}

	if !opts.DryRun {
		s.applySuggestionsWithProgress(result, photoMap, opts.ForceDate)
	} else {
		result.SortedCount = len(result.Suggestions)
	}

	return result, nil
}

// applyLabels replaces photo labels with AI-suggested ones (confidence > 80%).
func (s *Sorter) applyLabels(photoUID string, labels []ai.LabelWithConfidence) error {
	if err := s.photoprism.RemoveAllPhotoLabels(photoUID); err != nil {
		return fmt.Errorf("failed to remove existing labels: %w", err)
	}
	for _, label := range labels {
		if label.Confidence < 0.8 {
			continue
		}
		uncertainty := int((1 - label.Confidence) * 100)
		_, err := s.photoprism.AddPhotoLabel(photoUID, photoprism.PhotoLabel{
			Name:        label.Name,
			LabelSrc:    "manual",
			Uncertainty: uncertainty,
		})
		if err != nil {
			return fmt.Errorf("failed to add label %s: %w", label.Name, err)
		}
	}
	return nil
}

// buildDateUpdate adds date fields to the photo update if appropriate.
func buildDateUpdate(
	update *photoprism.PhotoUpdate, photo photoprism.Photo,
	estimatedDate string, forceDate bool,
) error {
	photoHasDate := photo.Year > 0 && photo.Year != 1
	cameraLower := strings.ToLower(photo.CameraModel)
	isScannedPhoto := photo.Scan || strings.Contains(cameraLower, "scanjet") || strings.Contains(cameraLower, "scanner")
	shouldUpdateDate := forceDate || !photoHasDate || isScannedPhoto
	if !shouldUpdateDate || estimatedDate == "" || estimatedDate == "0001-01-01" {
		return nil
	}

	prague, _ := time.LoadLocation("Europe/Prague")
	localTime, err := time.ParseInLocation("2006-01-02", estimatedDate, prague)
	if err != nil {
		return fmt.Errorf("failed to parse date %s: %w", estimatedDate, err)
	}
	localTime = localTime.Add(12 * time.Hour)

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
	return nil
}

func (s *Sorter) applySorting(photo photoprism.Photo, suggestion ai.SortSuggestion, forceDate bool) error {
	if err := s.applyLabels(photo.UID, suggestion.Labels); err != nil {
		return err
	}

	update := photoprism.PhotoUpdate{}

	desc := suggestion.Description
	if desc != "" {
		desc = fmt.Sprintf("%s\n\nAI_MODEL: %s", desc, s.aiProvider.Name())
	} else {
		desc = "AI_MODEL: " + s.aiProvider.Name()
	}
	update.Description = &desc
	descSrc := "manual"
	update.DescriptionSrc = &descSrc

	notes := "Analyzed by: " + s.aiProvider.Name()
	update.Details = &photoprism.PhotoDetails{
		Notes: &notes,
	}

	if err := buildDateUpdate(&update, photo, suggestion.EstimatedDate, forceDate); err != nil {
		return err
	}

	_, err := s.photoprism.EditPhoto(photo.UID, update)
	if err != nil {
		return fmt.Errorf("failed to update photo: %w", err)
	}

	return nil
}
