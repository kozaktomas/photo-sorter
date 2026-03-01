package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// ProcessJob represents an async photo processing job.
type ProcessJob struct {
	EventBroadcaster

	ID              string            `json:"id"`
	Status          JobStatus         `json:"status"`
	TotalPhotos     int               `json:"total_photos"`
	ProcessedPhotos int               `json:"processed_photos"`
	SkippedPhotos   int               `json:"skipped_photos"`
	Error           string            `json:"error,omitempty"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	Options         ProcessJobOptions `json:"options"`
	Result          *ProcessJobResult `json:"result,omitempty"`
}

// ProcessJobOptions represents options for a process job.
type ProcessJobOptions struct {
	Concurrency  int  `json:"concurrency"`
	Limit        int  `json:"limit"`
	NoFaces      bool `json:"no_faces"`
	NoEmbeddings bool `json:"no_embeddings"`
}

// ProcessJobResult represents the result of a process job.
type ProcessJobResult struct {
	EmbedSuccess    int64 `json:"embed_success"`
	EmbedError      int64 `json:"embed_error"`
	FaceSuccess     int64 `json:"face_success"`
	FaceError       int64 `json:"face_error"`
	TotalNewFaces   int64 `json:"total_new_faces"`
	TotalEmbeddings int   `json:"total_embeddings"`
	TotalFaces      int   `json:"total_faces"`
	TotalFacePhotos int   `json:"total_face_photos"`
}

// GetStatus returns the current job status (implements SSEJob).
func (j *ProcessJob) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// Cancel cancels the process job.
func (j *ProcessJob) Cancel() {
	j.EventBroadcaster.Cancel()
	j.mu.Lock()
	j.Status = JobStatusCancelled
	j.mu.Unlock()
}

// ProcessJobManager manages process jobs (only one at a time).
type ProcessJobManager struct {
	activeJob *ProcessJob
	mu        sync.RWMutex
}

// NewProcessJobManager creates a new process job manager.
func NewProcessJobManager() *ProcessJobManager {
	return &ProcessJobManager{}
}

// GetActiveJob returns the currently active job.
func (m *ProcessJobManager) GetActiveJob() *ProcessJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeJob
}

// GetJob returns a job by ID.
func (m *ProcessJobManager) GetJob(id string) *ProcessJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.activeJob != nil && m.activeJob.ID == id {
		return m.activeJob
	}
	return nil
}

// SetActiveJob sets the active job.
func (m *ProcessJobManager) SetActiveJob(job *ProcessJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeJob = job
}

// ClearActiveJob clears the active job.
func (m *ProcessJobManager) ClearActiveJob() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeJob = nil
}

// ProcessHandler handles photo processing endpoints.
type ProcessHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	jobManager     *ProcessJobManager
	facesHandler   *FacesHandler
	photosHandler  *PhotosHandler
	statsHandler   *StatsHandler
}

// NewProcessHandler creates a new process handler.
func NewProcessHandler(
	cfg *config.Config, sm *middleware.SessionManager,
	fh *FacesHandler, ph *PhotosHandler, sh *StatsHandler,
) *ProcessHandler {
	return &ProcessHandler{
		config:         cfg,
		sessionManager: sm,
		jobManager:     NewProcessJobManager(),
		facesHandler:   fh,
		photosHandler:  ph,
		statsHandler:   sh,
	}
}

// ProcessStartRequest represents a request to start processing.
type ProcessStartRequest struct {
	Concurrency  int  `json:"concurrency"`
	Limit        int  `json:"limit"`
	NoFaces      bool `json:"no_faces"`
	NoEmbeddings bool `json:"no_embeddings"`
}

// Start starts a new processing job.
func (h *ProcessHandler) Start(w http.ResponseWriter, r *http.Request) {
	// Validate PostgreSQL is configured.
	if !database.IsInitialized() {
		respondError(w, http.StatusBadRequest, "DATABASE_URL is not configured")
		return
	}

	// Check no job already running.
	if active := h.jobManager.GetActiveJob(); active != nil {
		if active.Status == JobStatusRunning || active.Status == JobStatusPending {
			respondError(w, http.StatusConflict, "a process job is already running")
			return
		}
	}

	var req ProcessStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.NoFaces && req.NoEmbeddings {
		respondError(w, http.StatusBadRequest, "cannot skip both faces and embeddings")
		return
	}

	if req.Concurrency <= 0 {
		req.Concurrency = constants.DefaultConcurrency
	}

	session := middleware.GetSessionFromContext(r.Context())

	// Create job.
	jobID := uuid.New().String()
	job := &ProcessJob{
		ID:        jobID,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Options:   ProcessJobOptions(req),
	}

	h.jobManager.SetActiveJob(job)

	// Launch processing goroutine.
	go h.runProcessJob(job, session)

	respondJSON(w, http.StatusAccepted, map[string]string{
		"job_id": jobID,
		"status": string(JobStatusPending),
	})
}

// Events streams process job events via SSE.
func (h *ProcessHandler) Events(w http.ResponseWriter, r *http.Request) {
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

// Cancel cancels a process job.
func (h *ProcessHandler) Cancel(w http.ResponseWriter, r *http.Request) {
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

// processJobRepos holds the repositories needed for a process job.
type processJobRepos struct {
	embRepo    database.EmbeddingReader
	faceWriter database.FaceWriter
}

// processJobCounters tracks atomic counters for the process job.
type processJobCounters struct {
	embedSuccess   int64
	embedError     int64
	faceSuccess    int64
	faceError      int64
	totalNewFaces  int64
	processedCount int64
}

// initProcessJobRepos initializes the repositories needed for the process job.
func initProcessJobRepos(ctx context.Context, options ProcessJobOptions) (*processJobRepos, error) {
	repos := &processJobRepos{}
	if !options.NoEmbeddings {
		var err error
		repos.embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get embedding reader: %w", err)
		}
	}
	if !options.NoFaces {
		var err error
		repos.faceWriter, err = database.GetFaceWriter(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get face writer: %w", err)
		}
	}
	return repos, nil
}

// fetchAllPhotos fetches all photos from PhotoPrism with pagination, respecting context cancellation.
func fetchAllPhotos(ctx context.Context, pp *photoprism.PhotoPrism) ([]photoprism.Photo, error) {
	var allPhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0

	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("fetching photos cancelled: %w", ctx.Err())
		}
		photos, err := pp.GetPhotos(pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("fetching photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		allPhotos = append(allPhotos, photos...)
		offset += len(photos)
	}
	return allPhotos, nil
}

// filterUnprocessedPhotos filters photos that need embedding or face processing.
func filterUnprocessedPhotos(
	ctx context.Context, allPhotos []photoprism.Photo,
	repos *processJobRepos, limit int,
) ([]photoprism.Photo, int) {
	var photosToProcess []photoprism.Photo
	for _, photo := range allPhotos {
		needsEmbed := false
		needsFaces := false
		if repos.embRepo != nil {
			has, _ := repos.embRepo.Has(ctx, photo.UID)
			needsEmbed = !has
		}
		if repos.faceWriter != nil {
			processed, _ := repos.faceWriter.IsFacesProcessed(ctx, photo.UID)
			needsFaces = !processed
		}
		if needsEmbed || needsFaces {
			photosToProcess = append(photosToProcess, photo)
		}
	}

	if limit > 0 && len(photosToProcess) > limit {
		photosToProcess = photosToProcess[:limit]
	}

	skipped := len(allPhotos) - len(photosToProcess)
	return photosToProcess, skipped
}

// processOnePhoto processes a single photo for embeddings and faces.
func processOnePhoto(ctx context.Context, pp *photoprism.PhotoPrism, embClient, faceClient *fingerprint.EmbeddingClient,
	repos *processJobRepos, photoUID string, counters *processJobCounters) {
	needsEmbed := false
	needsFaces := false
	if repos.embRepo != nil {
		has, _ := repos.embRepo.Has(ctx, photoUID)
		needsEmbed = !has
	}
	if repos.faceWriter != nil {
		processed, _ := repos.faceWriter.IsFacesProcessed(ctx, photoUID)
		needsFaces = !processed
	}

	imageData, _, err := pp.GetPhotoDownload(photoUID)
	if err != nil {
		if needsEmbed {
			atomic.AddInt64(&counters.embedError, 1)
		}
		if needsFaces {
			atomic.AddInt64(&counters.faceError, 1)
		}
		return
	}

	if needsEmbed {
		processPhotoEmbedding(ctx, embClient, repos.embRepo, photoUID, imageData, counters)
	}
	if needsFaces {
		processPhotoFaces(ctx, pp, faceClient, repos.faceWriter, photoUID, imageData, counters)
	}
}

// processPhotoEmbedding computes and saves a CLIP embedding for a single photo.
func processPhotoEmbedding(
	ctx context.Context, embClient *fingerprint.EmbeddingClient,
	embRepo database.EmbeddingReader, photoUID string,
	imageData []byte, counters *processJobCounters,
) {
	resizedData, err := fingerprint.ResizeImage(imageData, 1920)
	if err != nil {
		atomic.AddInt64(&counters.embedError, 1)
		return
	}
	result, err := embClient.ComputeEmbeddingWithMetadata(ctx, resizedData)
	if err != nil {
		atomic.AddInt64(&counters.embedError, 1)
		return
	}
	if embWriter, ok := embRepo.(interface {
		Save(ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int) error
	}); ok {
		if err := embWriter.Save(ctx, photoUID, result.Embedding, result.Model, result.Pretrained, result.Dim); err != nil {
			atomic.AddInt64(&counters.embedError, 1)
		} else {
			atomic.AddInt64(&counters.embedSuccess, 1)
		}
	}
}

// processPhotoFaces computes face embeddings and enriches with marker data.
func processPhotoFaces(
	ctx context.Context, pp *photoprism.PhotoPrism,
	faceClient *fingerprint.EmbeddingClient, faceWriter database.FaceWriter,
	photoUID string, imageData []byte, counters *processJobCounters,
) {
	result, err := faceClient.ComputeFaceEmbeddings(ctx, imageData)
	if err != nil {
		atomic.AddInt64(&counters.faceError, 1)
		return
	}
	faces := make([]database.StoredFace, len(result.Faces))
	for i, f := range result.Faces {
		faces[i] = database.StoredFace{
			PhotoUID: photoUID, FaceIndex: f.FaceIndex, Embedding: f.Embedding,
			BBox: f.BBox, DetScore: f.DetScore, Model: result.Model, Dim: f.Dim,
		}
	}
	if err := faceWriter.SaveFaces(ctx, photoUID, faces); err != nil {
		atomic.AddInt64(&counters.faceError, 1)
		return
	}
	enrichFacesWithMarkerData(pp, faceWriter, photoUID, faces)
	faceWriter.MarkFacesProcessed(ctx, photoUID, len(faces))
	atomic.AddInt64(&counters.faceSuccess, 1)
	atomic.AddInt64(&counters.totalNewFaces, int64(len(faces)))
}

// getEmbeddingURL returns the embedding service URL from config.
func getEmbeddingURL(cfg *config.Config) string {
	if cfg.Embedding.URL != "" {
		return cfg.Embedding.URL
	}
	return cfg.LlamaCpp.URL
}

// processJobClients holds the initialized clients for a process job.
type processJobClients struct {
	pp         *photoprism.PhotoPrism
	embClient  *fingerprint.EmbeddingClient
	faceClient *fingerprint.EmbeddingClient
}

func (h *ProcessHandler) initProcessJobClients(session *middleware.Session) (*processJobClients, error) {
	pp, err := getPhotoPrismClient(h.config, session)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	embURL := getEmbeddingURL(h.config)
	embClient, err := fingerprint.NewEmbeddingClient(embURL, "clip")
	if err != nil {
		return nil, fmt.Errorf("invalid embedding config: %w", err)
	}
	faceClient, err := fingerprint.NewEmbeddingClient(embURL, "faces")
	if err != nil {
		return nil, fmt.Errorf("invalid embedding config: %w", err)
	}

	return &processJobClients{pp: pp, embClient: embClient, faceClient: faceClient}, nil
}

// processPhotosWorkerPool runs the concurrent photo processing worker pool.
func (h *ProcessHandler) processPhotosWorkerPool(
	ctx context.Context, pp *photoprism.PhotoPrism,
	embClient, faceClient *fingerprint.EmbeddingClient,
	repos *processJobRepos, photosToProcess []photoprism.Photo,
	concurrency int, job *ProcessJob,
) *processJobCounters {
	counters := &processJobCounters{}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, photo := range photosToProcess {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(p photoprism.Photo) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			processOnePhoto(ctx, pp, embClient, faceClient, repos, p.UID, counters)
			count := atomic.AddInt64(&counters.processedCount, 1)
			h.sendProgress(job, int(count))
		}(photo)
	}
	wg.Wait()
	return counters
}

func (h *ProcessHandler) fetchAndFilterPhotos(
	ctx context.Context, clients *processJobClients, repos *processJobRepos,
	job *ProcessJob,
) ([]photoprism.Photo, error) {
	allPhotos, err := fetchAllPhotos(ctx, clients.pp)
	if err != nil {
		return nil, err
	}

	job.mu.Lock()
	job.TotalPhotos = len(allPhotos)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "photos_counted", Data: map[string]int{"total": len(allPhotos)}})

	photosToProcess, skipped := filterUnprocessedPhotos(ctx, allPhotos, repos, job.Options.Limit)

	job.mu.Lock()
	job.SkippedPhotos = skipped
	job.TotalPhotos = len(photosToProcess)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "filtering_done", Data: map[string]int{
		"to_process": len(photosToProcess), "skipped": skipped,
	}})

	return photosToProcess, nil
}

// runProcessJob executes the process job in the background.
func (h *ProcessHandler) runProcessJob(job *ProcessJob, session *middleware.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	defer cancel()

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "started", Message: "Process job started"})

	repos, err := initProcessJobRepos(ctx, job.Options)
	if err != nil {
		h.failJob(job, err.Error())
		return
	}

	clients, err := h.initProcessJobClients(session)
	if err != nil {
		h.failJob(job, err.Error())
		return
	}

	photosToProcess, err := h.fetchAndFilterPhotos(ctx, clients, repos, job)
	if err != nil {
		if ctx.Err() != nil {
			h.cancelJob(job)
		} else {
			h.failJob(job, fmt.Sprintf("failed to get photos: %v", err))
		}
		return
	}

	if len(photosToProcess) == 0 {
		h.completeJob(job, repos.embRepo, repos.faceWriter, 0, 0, 0, 0, 0)
		return
	}

	counters := h.processPhotosWorkerPool(
		ctx, clients.pp, clients.embClient, clients.faceClient, repos,
		photosToProcess, job.Options.Concurrency, job,
	)

	if ctx.Err() != nil {
		h.cancelJob(job)
		return
	}

	h.completeJob(job, repos.embRepo, repos.faceWriter,
		counters.embedSuccess, counters.embedError,
		counters.faceSuccess, counters.faceError, counters.totalNewFaces)
}

func (h *ProcessHandler) sendProgress(job *ProcessJob, processed int) {
	job.mu.Lock()
	job.ProcessedPhotos = processed
	job.mu.Unlock()
	job.SendEvent(JobEvent{
		Type: "progress",
		Data: map[string]any{
			"processed": processed,
			"total":     job.TotalPhotos,
		},
	})
}

func (h *ProcessHandler) failJob(job *ProcessJob, message string) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusFailed
	job.Error = message
	job.CompletedAt = &now
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "job_error", Message: message})
}

func (h *ProcessHandler) cancelJob(job *ProcessJob) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCancelled
	job.CompletedAt = &now
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "cancelled", Message: "Job cancelled"})
}

func (h *ProcessHandler) completeJob(job *ProcessJob, embRepo database.EmbeddingReader, faceWriter database.FaceWriter,
	embedSuccess, embedError, faceSuccess, faceError, totalNewFaces int64) {
	// Refresh handlers to use updated data.
	if h.facesHandler != nil {
		h.facesHandler.RefreshReader()
	}
	if h.photosHandler != nil {
		h.photosHandler.RefreshReader()
	}
	if h.statsHandler != nil {
		h.statsHandler.InvalidateCache()
	}

	// Gather stats.
	ctx := context.Background()
	result := &ProcessJobResult{
		EmbedSuccess:  embedSuccess,
		EmbedError:    embedError,
		FaceSuccess:   faceSuccess,
		FaceError:     faceError,
		TotalNewFaces: totalNewFaces,
	}
	if embRepo != nil {
		count, _ := embRepo.Count(ctx)
		result.TotalEmbeddings = count
	}
	if faceWriter != nil {
		faceCount, _ := faceWriter.Count(ctx)
		photoCount, _ := faceWriter.CountPhotos(ctx)
		result.TotalFaces = faceCount
		result.TotalFacePhotos = photoCount
	}

	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.Result = result
	job.mu.Unlock()

	job.SendEvent(JobEvent{Type: "completed", Data: result})
}

// convertMarkersToInfos converts PhotoPrism markers to facematch.MarkerInfo slice.
func convertMarkersToInfos(markers []photoprism.Marker) []facematch.MarkerInfo {
	markerInfos := make([]facematch.MarkerInfo, 0, len(markers))
	for i := range markers {
		m := &markers[i]
		markerInfos = append(markerInfos, facematch.MarkerInfo{
			UID: m.UID, Type: m.Type, Name: m.Name, SubjUID: m.SubjUID,
			X: m.X, Y: m.Y, W: m.W, H: m.H,
		})
	}
	return markerInfos
}

// matchFacesToMarkers matches each face to its best marker and updates the face cache.
func matchFacesToMarkers(
	ctx context.Context, faceWriter database.FaceWriter,
	photoUID string, faces []database.StoredFace,
	markerInfos []facematch.MarkerInfo, fileInfo *facematch.PrimaryFileInfo,
) {
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(
			face.BBox, markerInfos,
			fileInfo.Width, fileInfo.Height,
			fileInfo.Orientation, constants.IoUThreshold,
		)
		if match != nil {
			faceWriter.UpdateFaceMarker(
				ctx, photoUID, face.FaceIndex,
				match.MarkerUID, match.SubjectUID, match.SubjectName,
			)
		}
	}
}

// enrichFacesWithMarkerData fetches PhotoPrism marker/dimension data and caches it in the face records.
func enrichFacesWithMarkerData(
	pp *photoprism.PhotoPrism, faceWriter database.FaceWriter,
	photoUID string, faces []database.StoredFace,
) {
	if len(faces) == 0 {
		return
	}

	ctx := context.Background()
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return
	}

	fileInfo := facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return
	}

	faceWriter.UpdateFacePhotoInfo(
		ctx, photoUID,
		fileInfo.Width, fileInfo.Height,
		fileInfo.Orientation, fileInfo.UID,
	)

	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil || len(markers) == 0 {
		return
	}

	markerInfos := convertMarkersToInfos(markers)
	matchFacesToMarkers(
		ctx, faceWriter, photoUID,
		faces, markerInfos, fileInfo,
	)
}

// RebuildIndexResponse represents the response from rebuilding the HNSW index.
type RebuildIndexResponse struct {
	Success            bool   `json:"success"`
	FaceCount          int    `json:"face_count"`
	EmbeddingCount     int    `json:"embedding_count"`
	FaceIndexPath      string `json:"face_index_path"`
	EmbeddingIndexPath string `json:"embedding_index_path"`
	DurationMs         int64  `json:"duration_ms"`
}

// SyncCacheResponse represents the response from syncing the cache.
type SyncCacheResponse struct {
	Success       bool   `json:"success"`
	PhotosScanned int    `json:"photos_scanned"`
	FacesUpdated  int    `json:"faces_updated"`
	PhotosDeleted int    `json:"photos_deleted"`
	DurationMs    int64  `json:"duration_ms"`
	Error         string `json:"error,omitempty"`
}

// RebuildIndex rebuilds the HNSW indexes and reloads them in memory.
func (h *ProcessHandler) RebuildIndex(w http.ResponseWriter, _ *http.Request) {
	ctx := context.Background()
	startTime := time.Now()

	// Rebuild face HNSW index.
	faceRebuilder := database.GetFaceHNSWRebuilder()
	if faceRebuilder == nil {
		respondError(w, http.StatusInternalServerError, "Face HNSW rebuilder not registered")
		return
	}

	// Rebuild in-memory face HNSW index from PostgreSQL.
	if err := faceRebuilder.RebuildHNSW(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to rebuild face HNSW index: %v", err))
		return
	}

	// Save face index to disk if path is configured.
	if err := faceRebuilder.SaveHNSWIndex(); err != nil {
		// Log warning but don't fail - index is usable in memory.
		fmt.Printf("Warning: failed to save face HNSW index to disk: %v\n", err)
	}

	faceCount := faceRebuilder.HNSWCount()

	// Rebuild embedding HNSW index.
	embCount := 0
	embRebuilder := database.GetEmbeddingHNSWRebuilder()
	if embRebuilder != nil {
		// Rebuild in-memory embedding HNSW index from PostgreSQL.
		if err := embRebuilder.RebuildHNSW(ctx); err != nil {
			fmt.Printf("Warning: failed to rebuild embedding HNSW index: %v\n", err)
		} else {
			// Save embedding index to disk if path is configured.
			if err := embRebuilder.SaveHNSWIndex(); err != nil {
				fmt.Printf("Warning: failed to save embedding HNSW index to disk: %v\n", err)
			}
			embCount = embRebuilder.HNSWCount()
		}
	}

	// Fall back to count from reader if HNSW not enabled.
	if embCount == 0 {
		embReader, err := database.GetEmbeddingReader(ctx)
		if err == nil {
			embCount, _ = embReader.Count(ctx)
		}
	}

	durationMs := time.Since(startTime).Milliseconds()

	respondJSON(w, http.StatusOK, RebuildIndexResponse{
		Success:            true,
		FaceCount:          faceCount,
		EmbeddingCount:     embCount,
		FaceIndexPath:      "(persisted)",
		EmbeddingIndexPath: "(persisted)",
		DurationMs:         durationMs,
	})
}

// collectSyncPhotoUIDs collects all unique photo UIDs from faces and embeddings tables.
func collectSyncPhotoUIDs(
	ctx context.Context, faceWriter database.FaceWriter,
	embWriter database.EmbeddingWriter,
) ([]string, error) {
	faceUIDs, err := faceWriter.GetUniquePhotoUIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting unique photo UIDs: %w", err)
	}

	uidSet := make(map[string]struct{}, len(faceUIDs))
	for _, uid := range faceUIDs {
		uidSet[uid] = struct{}{}
	}

	if embWriter != nil {
		embUIDs, err := embWriter.GetUniquePhotoUIDs(ctx)
		if err == nil {
			for _, uid := range embUIDs {
				uidSet[uid] = struct{}{}
			}
		}
	}

	photoUIDs := make([]string, 0, len(uidSet))
	for uid := range uidSet {
		photoUIDs = append(photoUIDs, uid)
	}
	return photoUIDs, nil
}

// SyncCache syncs face marker data from PhotoPrism to the local cache.
// without recomputing embeddings. This is useful when faces are assigned/unassigned.
// directly in PhotoPrism's native UI. Also cleans up data for photos that have.
// been deleted or archived in PhotoPrism.
func (h *ProcessHandler) SyncCache(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	pp := middleware.MustGetPhotoPrism(ctx, w)
	if pp == nil {
		return
	}

	faceWriter, err := database.GetFaceWriter(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get face writer: %v", err))
		return
	}

	embWriter, _ := database.GetEmbeddingWriter(ctx)

	photoUIDs, err := collectSyncPhotoUIDs(ctx, faceWriter, embWriter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get face photo UIDs: %v", err))
		return
	}

	var facesUpdated int64
	var photosDeleted int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.WorkerPoolSize)

	for _, photoUID := range photoUIDs {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			updated, deleted := h.syncPhotoCache(ctx, pp, faceWriter, embWriter, uid)
			if updated > 0 {
				atomic.AddInt64(&facesUpdated, int64(updated))
			}
			if deleted {
				atomic.AddInt64(&photosDeleted, 1)
			}
		}(photoUID)
	}

	wg.Wait()

	if h.statsHandler != nil {
		h.statsHandler.InvalidateCache()
	}

	respondJSON(w, http.StatusOK, SyncCacheResponse{
		Success: true, PhotosScanned: len(photoUIDs),
		FacesUpdated: int(facesUpdated), PhotosDeleted: int(photosDeleted),
		DurationMs: time.Since(startTime).Milliseconds(),
	})
}

// cleanupDeletedPhoto removes all cached data for a deleted/archived photo.
func cleanupDeletedPhoto(
	ctx context.Context, faceWriter database.FaceWriter,
	embWriter database.EmbeddingWriter, photoUID string,
) {
	faceWriter.DeleteFacesByPhoto(ctx, photoUID)
	if embWriter != nil {
		embWriter.DeleteEmbedding(ctx, photoUID)
	}
}

// syncFaceMarkers matches faces to markers and updates changed entries, returning the count of updates.
func syncFaceMarkers(
	ctx context.Context, faceWriter database.FaceWriter,
	photoUID string, faces []database.StoredFace,
	markerInfos []facematch.MarkerInfo, fileInfo *facematch.PrimaryFileInfo,
) int {
	updated := 0
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(
			face.BBox, markerInfos,
			fileInfo.Width, fileInfo.Height,
			fileInfo.Orientation, constants.IoUThreshold,
		)
		if match != nil {
			if face.MarkerUID != match.MarkerUID ||
				face.SubjectUID != match.SubjectUID ||
				face.SubjectName != match.SubjectName {
				faceWriter.UpdateFaceMarker(
					ctx, photoUID, face.FaceIndex,
					match.MarkerUID, match.SubjectUID,
					match.SubjectName,
				)
				updated++
			}
		} else if face.MarkerUID != "" {
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
			updated++
		}
	}
	return updated
}

// fetchPhotoDetailsForSync fetches photo details and handles deleted/archived photos.
// Returns details and fileInfo on success, or (nil, nil, true) if the photo.
// was deleted, or (nil, nil, false) on other errors.
func fetchPhotoDetailsForSync(
	ctx context.Context, pp *photoprism.PhotoPrism,
	faceWriter database.FaceWriter, embWriter database.EmbeddingWriter,
	photoUID string,
) (details map[string]any, fileInfo *facematch.PrimaryFileInfo, deleted bool) {
	var err error
	details, err = pp.GetPhotoDetails(photoUID)
	if err != nil {
		if photoprism.IsNotFoundError(err) {
			cleanupDeletedPhoto(ctx, faceWriter, embWriter, photoUID)
			return nil, nil, true
		}
		return nil, nil, false
	}

	if photoprism.IsPhotoDeleted(details) {
		cleanupDeletedPhoto(ctx, faceWriter, embWriter, photoUID)
		return nil, nil, true
	}

	fileInfo = facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return nil, nil, false
	}
	return details, fileInfo, false
}

// syncPhotoCache syncs the cache for a single photo and returns the number of faces updated.
// and whether the photo was deleted/archived in PhotoPrism (404 or DeletedAt set).
func (h *ProcessHandler) syncPhotoCache(
	ctx context.Context, pp *photoprism.PhotoPrism,
	faceWriter database.FaceWriter, embWriter database.EmbeddingWriter,
	photoUID string,
) (int, bool) {
	_, fileInfo, deleted := fetchPhotoDetailsForSync(
		ctx, pp, faceWriter, embWriter, photoUID,
	)
	if fileInfo == nil {
		return 0, deleted
	}

	faceWriter.UpdateFacePhotoInfo(
		ctx, photoUID,
		fileInfo.Width, fileInfo.Height,
		fileInfo.Orientation, fileInfo.UID,
	)

	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil || len(markers) == 0 {
		return 0, false
	}

	faces, err := faceWriter.GetFaces(ctx, photoUID)
	if err != nil || len(faces) == 0 {
		return 0, false
	}

	markerInfos := convertMarkersToInfos(markers)
	return syncFaceMarkers(ctx, faceWriter, photoUID, faces, markerInfos, fileInfo), false
}
