package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/ai"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/sorter"

	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// SortHandler handles sort-related endpoints
type SortHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	jobManager     *JobManager
}

// NewSortHandler creates a new sort handler
func NewSortHandler(cfg *config.Config, sm *middleware.SessionManager, jm *JobManager) *SortHandler {
	return &SortHandler{
		config:         cfg,
		sessionManager: sm,
		jobManager:     jm,
	}
}

// StartRequest represents a sort start request
type StartRequest struct {
	AlbumUID        string `json:"album_uid"`
	DryRun          bool   `json:"dry_run"`
	Limit           int    `json:"limit"`
	IndividualDates bool   `json:"individual_dates"`
	BatchMode       bool   `json:"batch_mode"`
	Provider        string `json:"provider"`
	ForceDate       bool   `json:"force_date"`
	Concurrency     int    `json:"concurrency"`
}

// Start starts a new sort job
func (h *SortHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.AlbumUID == "" {
		respondError(w, http.StatusBadRequest, "album_uid is required")
		return
	}

	if req.Provider == "" {
		req.Provider = constants.ProviderOpenAI
	}

	if req.Concurrency <= 0 {
		req.Concurrency = constants.DefaultConcurrency
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	// Get session for background goroutine (context will be cancelled when handler returns)
	session := middleware.GetSessionFromContext(r.Context())

	// Get album info
	album, err := pp.GetAlbum(req.AlbumUID)
	if err != nil {
		respondError(w, http.StatusNotFound, "album not found")
		return
	}

	// Generate job ID
	jobID := uuid.New().String()

	// Create job
	options := SortJobOptions{
		DryRun:          req.DryRun,
		Limit:           req.Limit,
		IndividualDates: req.IndividualDates,
		BatchMode:       req.BatchMode,
		Provider:        req.Provider,
		ForceDate:       req.ForceDate,
		Concurrency:     req.Concurrency,
	}
	job := h.jobManager.CreateJob(jobID, req.AlbumUID, album.Title, options)

	// Start job in background
	go h.runSortJob(job, session)

	respondJSON(w, http.StatusAccepted, map[string]string{
		"job_id":      jobID,
		"album_uid":   req.AlbumUID,
		"album_title": album.Title,
		"status":      string(JobStatusPending),
	})
}

// Status returns the status of a sort job
func (h *SortHandler) Status(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return
	}

	job := h.jobManager.GetJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	respondJSON(w, http.StatusOK, job)
}

// Events streams job events via SSE
func (h *SortHandler) Events(w http.ResponseWriter, r *http.Request) {
	streamSSEEvents(w, r,
		func(id string) SSEJob {
			job := h.jobManager.GetJob(id)
			if job == nil {
				return nil
			}
			return job
		},
		func(job SSEJob) any {
			return job
		},
	)
}

// Cancel cancels a sort job
func (h *SortHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	if jobID == "" {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return
	}

	job := h.jobManager.GetJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	job.Cancel()
	respondJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

// runSortJob runs the sort job in the background
func (h *SortHandler) runSortJob(job *SortJob, session *middleware.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	defer cancel()

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "started", Message: "Sort job started"})

	// Create PhotoPrism client
	pp, err := getPhotoPrismClient(h.config, session)
	if err != nil {
		h.failJob(job, fmt.Sprintf("failed to connect to PhotoPrism: %v", err))
		return
	}

	// Create AI provider
	aiProvider, err := h.createAIProvider(job.Options.Provider)
	if err != nil {
		h.failJob(job, err.Error())
		return
	}

	if job.Options.BatchMode {
		aiProvider.SetBatchMode(true)
	}

	// Get photos to count
	limit := job.Options.Limit
	if limit == 0 {
		limit = constants.MaxPhotosPerFetch
	}
	photos, err := pp.GetAlbumPhotos(job.AlbumUID, limit, 0)
	if err != nil {
		h.failJob(job, fmt.Sprintf("failed to get photos: %v", err))
		return
	}

	job.mu.Lock()
	job.TotalPhotos = len(photos)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "photos_counted", Data: map[string]int{"total": len(photos)}})

	// Create sorter and run with progress callback
	s := sorter.New(pp, aiProvider)

	// Run the sort with progress callback
	result, err := s.Sort(ctx, job.AlbumUID, job.AlbumTitle, "", sorter.SortOptions{
		DryRun:          job.Options.DryRun,
		Limit:           job.Options.Limit,
		IndividualDates: job.Options.IndividualDates,
		BatchMode:       job.Options.BatchMode,
		ForceDate:       job.Options.ForceDate,
		Concurrency:     job.Options.Concurrency,
		OnProgress: func(info sorter.ProgressInfo) {
			job.mu.Lock()
			job.ProcessedPhotos = info.Current
			job.Progress = int(float64(info.Current) / float64(info.Total) * 100)
			job.mu.Unlock()
			job.SendEvent(JobEvent{
				Type: "progress",
				Data: map[string]any{
					"phase":            info.Phase,
					"current":          info.Current,
					"total":            info.Total,
					"photo_uid":        info.PhotoUID,
					"processed_photos": info.Current,
					"total_photos":     info.Total,
				},
			})
		},
	})

	if err != nil {
		if ctx.Err() != nil {
			job.mu.Lock()
			job.Status = JobStatusCancelled
			job.mu.Unlock()
			job.SendEvent(JobEvent{Type: "cancelled", Message: "Job was cancelled"})
			return
		}
		h.failJob(job, fmt.Sprintf("sorting failed: %v", err))
		return
	}

	// Get usage info
	usage := aiProvider.GetUsage()

	// Build result
	errors := make([]string, len(result.Errors))
	for i, e := range result.Errors {
		errors[i] = e.Error()
	}

	jobResult := &SortJobResult{
		ProcessedCount: result.ProcessedCount,
		SortedCount:    result.SortedCount,
		AlbumDate:      result.AlbumDate,
		DateReasoning:  result.DateReasoning,
		Errors:         errors,
		Suggestions:    result.Suggestions,
		Usage: &UsageInfo{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			TotalCost:    usage.TotalCost,
		},
	}

	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.ProcessedPhotos = result.ProcessedCount
	job.Progress = 100
	job.Result = jobResult
	job.mu.Unlock()

	job.SendEvent(JobEvent{Type: "completed", Data: jobResult})
}

func (h *SortHandler) failJob(job *SortJob, message string) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusFailed
	job.Error = message
	job.CompletedAt = &now
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "job_error", Message: message})
}

func (h *SortHandler) createAIProvider(providerName string) (ai.Provider, error) {
	switch providerName {
	case constants.ProviderOpenAI:
		return h.createOpenAIProvider()
	case constants.ProviderGemini:
		return h.createGeminiProvider()
	case constants.ProviderOllama:
		p, err := ai.NewOllamaProvider(h.config.Ollama.URL, h.config.Ollama.Model)
		if err != nil {
			return nil, fmt.Errorf("creating Ollama provider: %w", err)
		}
		return p, nil
	case constants.ProviderLlamaCpp:
		p, err := ai.NewLlamaCppProvider(h.config.LlamaCpp.URL, h.config.LlamaCpp.Model)
		if err != nil {
			return nil, fmt.Errorf("creating llama.cpp provider: %w", err)
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
}

func (h *SortHandler) createOpenAIProvider() (ai.Provider, error) {
	if h.config.OpenAI.Token == "" {
		return nil, errors.New("OPENAI_TOKEN environment variable is required")
	}
	pricing := h.config.GetModelPricing("gpt-4.1-mini")
	return ai.NewOpenAIProvider(h.config.OpenAI.Token,
		ai.RequestPricing{Input: pricing.Standard.Input, Output: pricing.Standard.Output},
		ai.RequestPricing{Input: pricing.Batch.Input, Output: pricing.Batch.Output},
	), nil
}

func (h *SortHandler) createGeminiProvider() (ai.Provider, error) {
	if h.config.Gemini.GetAPIKey() == "" {
		return nil, errors.New("GEMINI_API_KEY environment variable is required")
	}
	pricing := h.config.GetModelPricing("gemini-2.5-flash")
	provider, err := ai.NewGeminiProvider(context.Background(), h.config.Gemini.GetAPIKey(),
		ai.RequestPricing{Input: pricing.Standard.Input, Output: pricing.Standard.Output},
		ai.RequestPricing{Input: pricing.Batch.Input, Output: pricing.Batch.Output},
	)
	if err != nil {
		return nil, fmt.Errorf("creating Gemini provider: %w", err)
	}
	return provider, nil
}

func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	jsonData, _ := json.Marshal(data)
	_, _ = io.WriteString(w, "event: "+eventType+"\n")
	_, _ = io.WriteString(w, "data: ")
	_, _ = io.Copy(w, bytes.NewReader(jsonData))
	_, _ = io.WriteString(w, "\n\n")
	flusher.Flush()
}
