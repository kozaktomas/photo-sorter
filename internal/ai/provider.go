package ai

import "context"

// PhotoMetadata contains metadata about a photo that may help with analysis.
type PhotoMetadata struct {
	OriginalName string  // Original filename when uploaded
	FileName     string  // Current filename
	TakenAt      string  // Date from EXIF or file
	Year         int     // Year from metadata
	Month        int     // Month from metadata
	Day          int     // Day from metadata
	Country      string  // Country from GPS
	Lat          float64 // GPS latitude
	Lng          float64 // GPS longitude
	Width        int     // Image width
	Height       int     // Image height
}

// Provider defines the interface for AI analysis backends.
type Provider interface {
	Name() string
	AnalyzePhoto(
		ctx context.Context,
		imageData []byte,
		metadata *PhotoMetadata,
		availableLabels []string,
		estimateDate bool,
	) (*PhotoAnalysis, error)
	EstimateAlbumDate(
		ctx context.Context,
		albumTitle string,
		albumDescription string,
		photoDescriptions []string,
	) (*AlbumDateEstimate, error)

	// Batch API methods.
	CreatePhotoBatch(ctx context.Context, requests []BatchPhotoRequest) (batchID string, err error)
	GetBatchStatus(ctx context.Context, batchID string) (*BatchStatus, error)
	GetBatchResults(ctx context.Context, batchID string) ([]BatchPhotoResult, error)
	CancelBatch(ctx context.Context, batchID string) error

	// Usage tracking.
	GetUsage() *Usage
	ResetUsage()
	SetBatchMode(enabled bool)
}

// Usage tracks token usage and calculates cost.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalCost    float64 // in USD
}

// BatchPhotoRequest represents a single photo analysis request in a batch.
type BatchPhotoRequest struct {
	PhotoUID        string
	ImageData       []byte
	Metadata        *PhotoMetadata
	AvailableLabels []string
	EstimateDate    bool
}

// BatchStatus represents the current status of a batch job.
type BatchStatus struct {
	ID             string
	Status         string // "validating", "in_progress", "completed", "failed", "expired", "cancelled"
	TotalRequests  int
	CompletedCount int
	FailedCount    int
}

// BatchPhotoResult represents the result of a single photo analysis in a batch.
type BatchPhotoResult struct {
	PhotoUID string
	Analysis *PhotoAnalysis
	Error    string
}

// PhotoAnalysis contains the AI's analysis of a photo.
type PhotoAnalysis struct {
	// Labels with confidence scores.
	Labels []LabelWithConfidence `json:"labels"`
	// Description of what's in the photo.
	Description string `json:"description"`
	// EstimatedDate in YYYY-MM-DD format (only set when individual dating is enabled).
	EstimatedDate string `json:"estimated_date,omitempty"`
}

// LabelWithConfidence represents a label with its confidence score.
type LabelWithConfidence struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"` // 0-1, only labels with >0.8 will be applied
}

// AlbumDateEstimate contains the estimated date for an album based on all photos.
type AlbumDateEstimate struct {
	// EstimatedDate in YYYY-MM-DD format.
	EstimatedDate string `json:"estimated_date"`
	// Confidence score 0-1 for the date estimate.
	Confidence float64 `json:"confidence"`
	// Reasoning for the date estimate.
	Reasoning string `json:"reasoning"`
}

// SortSuggestion holds the AI-generated labels and description for a single photo.
type SortSuggestion struct {
	PhotoUID      string
	Labels        []LabelWithConfidence
	Description   string
	EstimatedDate string
}
