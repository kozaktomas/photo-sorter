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

// ProcessJob represents an async photo processing job
type ProcessJob struct {
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

	cancel    context.CancelFunc
	listeners []chan JobEvent
	mu        sync.RWMutex
}

// ProcessJobOptions represents options for a process job
type ProcessJobOptions struct {
	Concurrency  int  `json:"concurrency"`
	Limit        int  `json:"limit"`
	NoFaces      bool `json:"no_faces"`
	NoEmbeddings bool `json:"no_embeddings"`
}

// ProcessJobResult represents the result of a process job
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

// AddListener adds an event listener to the process job
func (j *ProcessJob) AddListener() chan JobEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan JobEvent, constants.EventChannelBuffer)
	j.listeners = append(j.listeners, ch)
	return ch
}

// RemoveListener removes an event listener from the process job
func (j *ProcessJob) RemoveListener(ch chan JobEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, listener := range j.listeners {
		if listener == ch {
			j.listeners = append(j.listeners[:i], j.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}

// SendEvent sends an event to all listeners
func (j *ProcessJob) SendEvent(event JobEvent) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	for _, listener := range j.listeners {
		select {
		case listener <- event:
		default:
			// Listener buffer full, skip
		}
	}
}

// Cancel cancels the process job
func (j *ProcessJob) Cancel() {
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Lock()
	j.Status = JobStatusCancelled
	j.mu.Unlock()
	j.SendEvent(JobEvent{Type: "cancelled", Message: "Job cancelled by user"})
}

// ProcessJobManager manages process jobs (only one at a time)
type ProcessJobManager struct {
	activeJob *ProcessJob
	mu        sync.RWMutex
}

// NewProcessJobManager creates a new process job manager
func NewProcessJobManager() *ProcessJobManager {
	return &ProcessJobManager{}
}

// GetActiveJob returns the currently active job
func (m *ProcessJobManager) GetActiveJob() *ProcessJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeJob
}

// GetJob returns a job by ID
func (m *ProcessJobManager) GetJob(id string) *ProcessJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.activeJob != nil && m.activeJob.ID == id {
		return m.activeJob
	}
	return nil
}

// SetActiveJob sets the active job
func (m *ProcessJobManager) SetActiveJob(job *ProcessJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeJob = job
}

// ClearActiveJob clears the active job
func (m *ProcessJobManager) ClearActiveJob() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeJob = nil
}

// ProcessHandler handles photo processing endpoints
type ProcessHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	jobManager     *ProcessJobManager
	facesHandler   *FacesHandler
	photosHandler  *PhotosHandler
}

// NewProcessHandler creates a new process handler
func NewProcessHandler(cfg *config.Config, sm *middleware.SessionManager, fh *FacesHandler, ph *PhotosHandler) *ProcessHandler {
	return &ProcessHandler{
		config:         cfg,
		sessionManager: sm,
		jobManager:     NewProcessJobManager(),
		facesHandler:   fh,
		photosHandler:  ph,
	}
}

// ProcessStartRequest represents a request to start processing
type ProcessStartRequest struct {
	Concurrency  int  `json:"concurrency"`
	Limit        int  `json:"limit"`
	NoFaces      bool `json:"no_faces"`
	NoEmbeddings bool `json:"no_embeddings"`
}

// Start starts a new processing job
func (h *ProcessHandler) Start(w http.ResponseWriter, r *http.Request) {
	// Validate PostgreSQL is configured
	if !database.IsInitialized() {
		respondError(w, http.StatusBadRequest, "DATABASE_URL is not configured")
		return
	}

	// Check no job already running
	if active := h.jobManager.GetActiveJob(); active != nil {
		if active.Status == JobStatusRunning || active.Status == JobStatusPending {
			respondError(w, http.StatusConflict, "a process job is already running")
			return
		}
	}

	var req ProcessStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
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

	// Create job
	jobID := uuid.New().String()
	job := &ProcessJob{
		ID:        jobID,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Options: ProcessJobOptions{
			Concurrency:  req.Concurrency,
			Limit:        req.Limit,
			NoFaces:      req.NoFaces,
			NoEmbeddings: req.NoEmbeddings,
		},
	}

	h.jobManager.SetActiveJob(job)

	// Launch processing goroutine
	go h.runProcessJob(job, session)

	respondJSON(w, http.StatusAccepted, map[string]string{
		"job_id": jobID,
		"status": string(JobStatusPending),
	})
}

// Events streams process job events via SSE
func (h *ProcessHandler) Events(w http.ResponseWriter, r *http.Request) {
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

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe to job events
	eventCh := job.AddListener()
	defer job.RemoveListener(eventCh)

	// Send initial status
	sendSSEEvent(w, flusher, "status", job)

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			sendSSEEvent(w, flusher, event.Type, event)

			// Close connection if job is done
			if job.Status == JobStatusCompleted || job.Status == JobStatusFailed || job.Status == JobStatusCancelled {
				return
			}
		}
	}
}

// Cancel cancels a process job
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

// runProcessJob executes the process job in the background
func (h *ProcessHandler) runProcessJob(job *ProcessJob, session *middleware.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	defer cancel()

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "started", Message: "Process job started"})

	// Get PostgreSQL repositories
	var embRepo database.EmbeddingReader
	var faceWriter database.FaceWriter

	if !job.Options.NoEmbeddings {
		var err error
		embRepo, err = database.GetEmbeddingReader(ctx)
		if err != nil {
			h.failJob(job, fmt.Sprintf("failed to get embedding reader: %v", err))
			return
		}
	}
	if !job.Options.NoFaces {
		var err error
		faceWriter, err = database.GetFaceWriter(ctx)
		if err != nil {
			h.failJob(job, fmt.Sprintf("failed to get face writer: %v", err))
			return
		}
	}

	// Create PhotoPrism client
	pp, err := getPhotoPrismClient(h.config, session)
	if err != nil {
		h.failJob(job, fmt.Sprintf("failed to connect to PhotoPrism: %v", err))
		return
	}

	// Create embedding clients
	embURL := h.config.Embedding.URL
	if embURL == "" {
		embURL = h.config.LlamaCpp.URL
	}
	embClient := fingerprint.NewEmbeddingClient(embURL, "clip")
	faceClient := fingerprint.NewEmbeddingClient(embURL, "faces")

	// Fetch all photos paginated (limit is applied after filtering)
	var allPhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0

	for {
		if ctx.Err() != nil {
			h.cancelJob(job)
			return
		}
		photos, err := pp.GetPhotos(pageSize, offset)
		if err != nil {
			h.failJob(job, fmt.Sprintf("failed to get photos: %v", err))
			return
		}
		if len(photos) == 0 {
			break
		}
		allPhotos = append(allPhotos, photos...)
		offset += len(photos)
	}

	job.mu.Lock()
	job.TotalPhotos = len(allPhotos)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "photos_counted", Data: map[string]int{"total": len(allPhotos)}})

	// Filter unprocessed photos
	var photosToProcess []photoprism.Photo
	for _, photo := range allPhotos {
		needsEmbed := false
		needsFaces := false

		if embRepo != nil {
			has, _ := embRepo.Has(ctx, photo.UID)
			needsEmbed = !has
		}
		if faceWriter != nil {
			processed, _ := faceWriter.IsFacesProcessed(ctx, photo.UID)
			needsFaces = !processed
		}

		if needsEmbed || needsFaces {
			photosToProcess = append(photosToProcess, photo)
		}
	}

	// Apply limit to unprocessed photos only
	if job.Options.Limit > 0 && len(photosToProcess) > job.Options.Limit {
		photosToProcess = photosToProcess[:job.Options.Limit]
	}

	skipped := len(allPhotos) - len(photosToProcess)
	job.mu.Lock()
	job.SkippedPhotos = skipped
	job.TotalPhotos = len(photosToProcess)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "filtering_done", Data: map[string]int{
		"to_process": len(photosToProcess),
		"skipped":    skipped,
	}})

	if len(photosToProcess) == 0 {
		h.completeJob(job, embRepo, faceWriter, 0, 0, 0, 0, 0)
		return
	}

	// Process photos with concurrency
	var embedSuccess, embedError, faceSuccess, faceError, totalNewFaces int64
	var processedCount int64

	sem := make(chan struct{}, job.Options.Concurrency)
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

			// Check what needs to be done
			needsEmbed := false
			needsFaces := false
			if embRepo != nil {
				has, _ := embRepo.Has(ctx, p.UID)
				needsEmbed = !has
			}
			if faceWriter != nil {
				processed, _ := faceWriter.IsFacesProcessed(ctx, p.UID)
				needsFaces = !processed
			}

			// Download original photo
			imageData, _, err := pp.GetPhotoDownload(p.UID)
			if err != nil {
				if needsEmbed {
					atomic.AddInt64(&embedError, 1)
				}
				if needsFaces {
					atomic.AddInt64(&faceError, 1)
				}
				count := atomic.AddInt64(&processedCount, 1)
				h.sendProgress(job, int(count))
				return
			}

			// Process embeddings
			if needsEmbed {
				resizedData, err := fingerprint.ResizeImage(imageData, 1920)
				if err != nil {
					atomic.AddInt64(&embedError, 1)
				} else {
					result, err := embClient.ComputeEmbeddingWithMetadata(ctx, resizedData)
					if err != nil {
						atomic.AddInt64(&embedError, 1)
					} else {
						// Save to PostgreSQL via embedding writer
						if embWriter, ok := embRepo.(interface {
							Save(ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int) error
						}); ok {
							if err := embWriter.Save(ctx, p.UID, result.Embedding, result.Model, result.Pretrained, result.Dim); err != nil {
								atomic.AddInt64(&embedError, 1)
							} else {
								atomic.AddInt64(&embedSuccess, 1)
							}
						}
					}
				}
			}

			// Process faces
			if needsFaces {
				result, err := faceClient.ComputeFaceEmbeddings(ctx, imageData)
				if err != nil {
					atomic.AddInt64(&faceError, 1)
				} else {
					faces := make([]database.StoredFace, len(result.Faces))
					for i, f := range result.Faces {
						faces[i] = database.StoredFace{
							PhotoUID:  p.UID,
							FaceIndex: f.FaceIndex,
							Embedding: f.Embedding,
							BBox:      f.BBox,
							DetScore:  f.DetScore,
							Model:     result.Model,
							Dim:       f.Dim,
						}
					}
					if err := faceWriter.SaveFaces(ctx, p.UID, faces); err != nil {
						atomic.AddInt64(&faceError, 1)
					} else {
						// Fetch and cache PhotoPrism marker data
						enrichFacesWithMarkerData(pp, faceWriter, p.UID, faces)

						faceWriter.MarkFacesProcessed(ctx, p.UID, len(faces))
						atomic.AddInt64(&faceSuccess, 1)
						atomic.AddInt64(&totalNewFaces, int64(len(faces)))
					}
				}
			}

			// Track progress
			count := atomic.AddInt64(&processedCount, 1)
			h.sendProgress(job, int(count))
		}(photo)
	}

	wg.Wait()

	if ctx.Err() != nil {
		h.cancelJob(job)
		return
	}

	h.completeJob(job, embRepo, faceWriter,
		embedSuccess, embedError, faceSuccess, faceError, totalNewFaces)
}

func (h *ProcessHandler) sendProgress(job *ProcessJob, processed int) {
	job.mu.Lock()
	job.ProcessedPhotos = processed
	job.mu.Unlock()
	job.SendEvent(JobEvent{
		Type: "progress",
		Data: map[string]interface{}{
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

	// Refresh handlers to use updated data
	if h.facesHandler != nil {
		h.facesHandler.RefreshReader()
	}
	if h.photosHandler != nil {
		h.photosHandler.RefreshReader()
	}

	// Gather stats
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

// enrichFacesWithMarkerData fetches PhotoPrism marker/dimension data and caches it in the face records
func enrichFacesWithMarkerData(pp *photoprism.PhotoPrism, faceWriter database.FaceWriter, photoUID string, faces []database.StoredFace) {
	if len(faces) == 0 {
		return
	}

	ctx := context.Background()

	// Get photo details for dimensions
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return
	}

	fileInfo := facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return
	}

	// Update photo dimensions for all faces
	faceWriter.UpdateFacePhotoInfo(ctx, photoUID, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, fileInfo.UID)

	// Get markers from PhotoPrism
	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil || len(markers) == 0 {
		return
	}

	// Convert markers to facematch.MarkerInfo
	markerInfos := make([]facematch.MarkerInfo, 0, len(markers))
	for _, m := range markers {
		markerInfos = append(markerInfos, facematch.MarkerInfo{
			UID:     m.UID,
			Type:    m.Type,
			Name:    m.Name,
			SubjUID: m.SubjUID,
			X:       m.X,
			Y:       m.Y,
			W:       m.W,
			H:       m.H,
		})
	}

	// Match each face to a marker and update cache
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(face.BBox, markerInfos, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, constants.IoUThreshold)
		if match != nil {
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, match.MarkerUID, match.SubjectUID, match.SubjectName)
		}
	}
}

// RebuildIndexResponse represents the response from rebuilding the HNSW index
type RebuildIndexResponse struct {
	Success            bool   `json:"success"`
	FaceCount          int    `json:"face_count"`
	EmbeddingCount     int    `json:"embedding_count"`
	FaceIndexPath      string `json:"face_index_path"`
	EmbeddingIndexPath string `json:"embedding_index_path"`
	DurationMs         int64  `json:"duration_ms"`
}

// SyncCacheResponse represents the response from syncing the cache
type SyncCacheResponse struct {
	Success       bool   `json:"success"`
	PhotosScanned int    `json:"photos_scanned"`
	FacesUpdated  int    `json:"faces_updated"`
	DurationMs    int64  `json:"duration_ms"`
	Error         string `json:"error,omitempty"`
}

// RebuildIndex rebuilds the HNSW indexes and reloads them in memory
func (h *ProcessHandler) RebuildIndex(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	startTime := time.Now()

	// Rebuild face HNSW index
	faceRebuilder := database.GetFaceHNSWRebuilder()
	if faceRebuilder == nil {
		respondError(w, http.StatusInternalServerError, "Face HNSW rebuilder not registered")
		return
	}

	// Rebuild in-memory face HNSW index from PostgreSQL
	if err := faceRebuilder.RebuildHNSW(ctx); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to rebuild face HNSW index: %v", err))
		return
	}

	// Save face index to disk if path is configured
	if err := faceRebuilder.SaveHNSWIndex(); err != nil {
		// Log warning but don't fail - index is usable in memory
		fmt.Printf("Warning: failed to save face HNSW index to disk: %v\n", err)
	}

	faceCount := faceRebuilder.HNSWCount()

	// Rebuild embedding HNSW index
	embCount := 0
	embRebuilder := database.GetEmbeddingHNSWRebuilder()
	if embRebuilder != nil {
		// Rebuild in-memory embedding HNSW index from PostgreSQL
		if err := embRebuilder.RebuildHNSW(ctx); err != nil {
			fmt.Printf("Warning: failed to rebuild embedding HNSW index: %v\n", err)
		} else {
			// Save embedding index to disk if path is configured
			if err := embRebuilder.SaveHNSWIndex(); err != nil {
				fmt.Printf("Warning: failed to save embedding HNSW index to disk: %v\n", err)
			}
			embCount = embRebuilder.HNSWCount()
		}
	}

	// Fall back to count from reader if HNSW not enabled
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

// SyncCache syncs face marker data from PhotoPrism to the local cache
// without recomputing embeddings. This is useful when faces are assigned/unassigned
// directly in PhotoPrism's native UI.
func (h *ProcessHandler) SyncCache(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Get PhotoPrism client
	pp := middleware.MustGetPhotoPrism(ctx, w)
	if pp == nil {
		return
	}

	// Get face writer
	faceWriter, err := database.GetFaceWriter(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get face writer: %v", err))
		return
	}

	// Get all unique photo UIDs with faces
	photoUIDs, err := faceWriter.GetUniquePhotoUIDs(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get photo UIDs: %v", err))
		return
	}

	// Process each photo
	var facesUpdated int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.WorkerPoolSize)

	for _, photoUID := range photoUIDs {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			updated := h.syncPhotoCache(ctx, pp, faceWriter, uid)
			if updated > 0 {
				atomic.AddInt64(&facesUpdated, int64(updated))
			}
		}(photoUID)
	}

	wg.Wait()

	durationMs := time.Since(startTime).Milliseconds()

	respondJSON(w, http.StatusOK, SyncCacheResponse{
		Success:       true,
		PhotosScanned: len(photoUIDs),
		FacesUpdated:  int(facesUpdated),
		DurationMs:    durationMs,
	})
}

// syncPhotoCache syncs the cache for a single photo and returns the number of faces updated
func (h *ProcessHandler) syncPhotoCache(ctx context.Context, pp *photoprism.PhotoPrism, faceWriter database.FaceWriter, photoUID string) int {
	// Get photo details for dimensions
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return 0
	}

	fileInfo := facematch.ExtractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return 0
	}

	// Update photo dimensions for all faces
	faceWriter.UpdateFacePhotoInfo(ctx, photoUID, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, fileInfo.UID)

	// Get markers from PhotoPrism
	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil || len(markers) == 0 {
		return 0
	}

	// Get faces for this photo from database
	faces, err := faceWriter.GetFaces(ctx, photoUID)
	if err != nil || len(faces) == 0 {
		return 0
	}

	// Convert markers to facematch.MarkerInfo
	markerInfos := make([]facematch.MarkerInfo, 0, len(markers))
	for _, m := range markers {
		markerInfos = append(markerInfos, facematch.MarkerInfo{
			UID:     m.UID,
			Type:    m.Type,
			Name:    m.Name,
			SubjUID: m.SubjUID,
			X:       m.X,
			Y:       m.Y,
			W:       m.W,
			H:       m.H,
		})
	}

	// Match each face to a marker and update cache
	updated := 0
	for _, face := range faces {
		if len(face.BBox) != 4 {
			continue
		}
		match := facematch.MatchFaceToMarkers(face.BBox, markerInfos, fileInfo.Width, fileInfo.Height, fileInfo.Orientation, constants.IoUThreshold)
		if match != nil {
			// Check if update is needed (to count actual changes)
			if face.MarkerUID != match.MarkerUID || face.SubjectUID != match.SubjectUID || face.SubjectName != match.SubjectName {
				faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, match.MarkerUID, match.SubjectUID, match.SubjectName)
				updated++
			}
		} else if face.MarkerUID != "" {
			// No match found but face had a marker - clear it
			faceWriter.UpdateFaceMarker(ctx, photoUID, face.FaceIndex, "", "", "")
			updated++
		}
	}

	return updated
}
