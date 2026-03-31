package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// UploadJob represents an async upload job.
type UploadJob struct {
	EventBroadcaster

	ID          string           `json:"id"`
	Status      JobStatus        `json:"status"`
	Error       string           `json:"error,omitempty"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Options     UploadJobOptions `json:"options"`
	Result      *UploadJobResult `json:"result,omitempty"`
}

// UploadJobOptions describes upload job configuration.
type UploadJobOptions struct {
	AlbumUIDs     []string `json:"album_uids"`
	Labels        []string `json:"labels"`
	BookSectionID string   `json:"book_section_id,omitempty"`
	AutoProcess   bool     `json:"auto_process"`
	FileCount     int      `json:"file_count"`
}

// UploadJobResult describes the outcome of an upload job.
type UploadJobResult struct {
	Uploaded      int      `json:"uploaded"`
	ExistingCount int      `json:"existing_count"`
	LabelsApplied int      `json:"labels_applied"`
	AlbumsApplied int      `json:"albums_applied"`
	BookAdded     int      `json:"book_added"`
	NewPhotoUIDs  []string `json:"new_photo_uids"`
}

// GetStatus returns the current job status (implements SSEJob).
func (j *UploadJob) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// Cancel cancels the upload job.
func (j *UploadJob) Cancel() {
	j.EventBroadcaster.Cancel()
	j.mu.Lock()
	j.Status = JobStatusCancelled
	j.mu.Unlock()
}

// UploadJobManager manages upload jobs (one at a time).
type UploadJobManager struct {
	activeJob *UploadJob
	mu        sync.RWMutex
}

// NewUploadJobManager creates a new upload job manager.
func NewUploadJobManager() *UploadJobManager {
	return &UploadJobManager{}
}

// GetActiveJob returns the currently active job.
func (m *UploadJobManager) GetActiveJob() *UploadJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeJob
}

// GetJob returns a job by ID.
func (m *UploadJobManager) GetJob(id string) *UploadJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.activeJob != nil && m.activeJob.ID == id {
		return m.activeJob
	}
	return nil
}

// SetActiveJob sets the active job.
func (m *UploadJobManager) SetActiveJob(job *UploadJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeJob = job
}

// needsBeforeSnapshot returns true if we need to snapshot album UIDs before upload.
func needsBeforeSnapshot(opts UploadJobOptions) bool {
	return len(opts.Labels) > 0 ||
		len(opts.AlbumUIDs) > 1 ||
		opts.BookSectionID != "" ||
		opts.AutoProcess
}

// runUploadJob executes the upload job in the background.
func (h *UploadHandler) runUploadJob(
	job *UploadJob, session *middleware.Session, tempDir string,
) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	defer cancel()
	defer os.RemoveAll(tempDir)

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "started", Message: "Upload job started"})

	pp, err := getPhotoPrismClient(h.config, session)
	if err != nil {
		h.failUploadJob(job, "failed to connect to PhotoPrism: "+err.Error())
		return
	}

	primaryAlbumUID := job.Options.AlbumUIDs[0]
	beforeUIDs := h.snapshotBefore(ctx, job, pp, primaryAlbumUID)

	uploaded := h.uploadFiles(ctx, job, pp, tempDir)
	if uploaded == nil {
		return // job already failed or cancelled
	}

	job.SendEvent(JobEvent{
		Type: "processing_upload", Message: "Processing uploads",
		Data: map[string]any{"current": 0, "total": len(uploaded.tokens)},
	})
	processed, timedOut := processUploadTokens(ctx, pp, uploaded.tokens, job.Options.AlbumUIDs, job)
	if timedOut > 0 {
		log.Printf("Upload processing: %d/%d succeeded, %d timed out", processed, len(uploaded.tokens), timedOut)
	}

	if ctx.Err() != nil {
		h.cancelUploadJob(job)
		return
	}

	newUIDs := h.detectNewPhotos(ctx, job, pp, primaryAlbumUID, beforeUIDs)
	jobResult := &UploadJobResult{
		Uploaded:     len(uploaded.tokens),
		NewPhotoUIDs: newUIDs,
	}
	if beforeUIDs != nil {
		jobResult.ExistingCount = max(len(uploaded.tokens)-len(newUIDs), 0)
	}

	// For book section: find all uploaded photos by filename (new + duplicates).
	var bookUIDs []string
	if job.Options.BookSectionID != "" {
		bookUIDs = h.findUploadedPhotoUIDs(ctx, pp, uploaded.fileNames)
	}
	h.applyPostUploadActions(ctx, job, pp, session, newUIDs, bookUIDs, jobResult)

	if ctx.Err() != nil {
		h.cancelUploadJob(job)
		return
	}

	h.completeUploadJob(job, jobResult)
}

// snapshotBefore captures album photo UIDs before upload if needed.
func (h *UploadHandler) snapshotBefore(
	ctx context.Context, job *UploadJob,
	pp *photoprism.PhotoPrism, albumUID string,
) map[string]struct{} {
	if !needsBeforeSnapshot(job.Options) {
		return nil
	}
	job.SendEvent(JobEvent{
		Type: "detecting_photos", Message: "Snapshotting album",
	})
	uids, err := getAlbumPhotoUIDSet(ctx, pp, albumUID)
	if err != nil {
		log.Printf("Warning: failed to snapshot album: %v", err)
		return make(map[string]struct{})
	}
	return uids
}

// uploadResult holds the tokens and original filenames from an upload.
type uploadResult struct {
	tokens    []string
	fileNames []string // base filenames (e.g. "photo.jpg")
}

// uploadFiles uploads files one-by-one, returning tokens and filenames or nil on failure.
func (h *UploadHandler) uploadFiles(
	ctx context.Context, job *UploadJob,
	pp *photoprism.PhotoPrism, tempDir string,
) *uploadResult {
	filePaths, err := listFilesInDir(tempDir)
	if err != nil {
		h.failUploadJob(job, "failed to list upload files: "+err.Error())
		return nil
	}

	result := &uploadResult{}
	for i, filePath := range filePaths {
		if ctx.Err() != nil {
			h.cancelUploadJob(job)
			return nil
		}
		fileName := filepath.Base(filePath)
		job.SendEvent(JobEvent{Type: "upload_progress", Data: map[string]any{
			"current": i + 1, "total": len(filePaths), "filename": fileName,
		}})
		token, uploadErr := pp.UploadFile(filePath)
		if uploadErr != nil {
			log.Printf("Upload error for %s: %v", fileName, uploadErr)
			continue
		}
		result.tokens = append(result.tokens, token)
		result.fileNames = append(result.fileNames, fileName)
	}

	if len(result.tokens) == 0 {
		h.failUploadJob(job, "no files were uploaded successfully")
		return nil
	}
	return result
}

// detectNewPhotos diffs album UIDs before/after to find new photos.
// Returns UIDs that appeared in the album after upload (includes duplicates
// that PhotoPrism recognized and added to the album).
func (h *UploadHandler) detectNewPhotos(
	ctx context.Context, job *UploadJob,
	pp *photoprism.PhotoPrism, albumUID string,
	beforeUIDs map[string]struct{},
) []string {
	if beforeUIDs == nil {
		return nil
	}
	job.SendEvent(JobEvent{
		Type: "detecting_photos", Message: "Detecting new photos",
	})
	afterUIDs, err := getAlbumPhotoUIDSet(ctx, pp, albumUID)
	if err != nil {
		log.Printf("Warning: failed to snapshot album after upload: %v", err)
		return nil
	}
	var newUIDs []string
	for uid := range afterUIDs {
		if _, existed := beforeUIDs[uid]; !existed {
			newUIDs = append(newUIDs, uid)
		}
	}
	return newUIDs
}

// findUploadedPhotoUIDs searches PhotoPrism globally for photos matching the
// uploaded filenames. This finds both new photos and duplicates that PhotoPrism
// recognized, even if the duplicate was not added to the target album.
func (h *UploadHandler) findUploadedPhotoUIDs(
	ctx context.Context,
	pp *photoprism.PhotoPrism,
	fileNames []string,
) []string {
	if len(fileNames) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	var matched []string
	for _, name := range fileNames {
		if ctx.Err() != nil {
			break
		}
		// Strip duplicate suffix like "(0)", "(1)" that PhotoPrism adds,
		// and search by the base original filename.
		baseName := stripDupSuffix(strings.TrimSuffix(name, filepath.Ext(name)))
		photos, err := pp.GetPhotosWithQuery(10, 0, "original:"+baseName, 0)
		if err != nil {
			log.Printf("Warning: search for uploaded file %s: %v", name, err)
			continue
		}
		for _, p := range photos {
			if _, ok := seen[p.UID]; ok {
				continue
			}
			origName := filepath.Base(p.OriginalName)
			origBase := stripDupSuffix(strings.TrimSuffix(origName, filepath.Ext(origName)))
			if strings.EqualFold(origBase, baseName) {
				matched = append(matched, p.UID)
				seen[p.UID] = struct{}{}
			}
		}
	}
	return matched
}

// applyPostUploadActions applies labels, albums, book section, and auto-process.
func (h *UploadHandler) applyPostUploadActions(
	ctx context.Context, job *UploadJob,
	pp *photoprism.PhotoPrism, session *middleware.Session,
	newUIDs []string, bookUIDs []string, result *UploadJobResult,
) {
	// Add uploaded photos to book section. bookUIDs are matched by filename
	// so they include both new and duplicate uploads.
	if job.Options.BookSectionID != "" && len(bookUIDs) > 0 {
		addToBookSection(ctx, job, bookUIDs, result)
	}

	if len(newUIDs) == 0 {
		return
	}

	if len(job.Options.Labels) > 0 {
		result.LabelsApplied = applyLabels(
			ctx, pp, newUIDs, job.Options.Labels, job,
		)
	}

	if len(job.Options.AlbumUIDs) > 1 {
		for _, albumUID := range job.Options.AlbumUIDs[1:] {
			job.SendEvent(JobEvent{
				Type:    "applying_albums",
				Message: "Adding to album " + albumUID,
			})
			if err := pp.AddPhotosToAlbum(albumUID, newUIDs); err != nil {
				log.Printf("Warning: add to album %s: %v", albumUID, err)
			} else {
				result.AlbumsApplied++
			}
		}
	}

	if job.Options.AutoProcess && database.IsInitialized() {
		h.autoProcessPhotos(ctx, job, session, newUIDs)
	}
}

// addToBookSection adds photos to a book section.
func addToBookSection(
	ctx context.Context, job *UploadJob,
	newUIDs []string, result *UploadJobResult,
) {
	job.SendEvent(JobEvent{
		Type: "adding_to_book", Message: "Adding to book section",
	})
	bookWriter, err := database.GetBookWriter(ctx)
	if err != nil {
		return
	}
	if err := bookWriter.AddSectionPhotos(
		ctx, job.Options.BookSectionID, newUIDs,
	); err != nil {
		log.Printf("Warning: add to book section: %v", err)
	} else {
		result.BookAdded = len(newUIDs)
	}
}

// getAlbumPhotoUIDSet fetches all photo UIDs in an album as a set.
func getAlbumPhotoUIDSet(
	ctx context.Context,
	pp *photoprism.PhotoPrism, albumUID string,
) (map[string]struct{}, error) {
	var allPhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("cancelled: %w", ctx.Err())
		}
		photos, err := pp.GetAlbumPhotos(albumUID, pageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("fetching album photos: %w", err)
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
	uids := make(map[string]struct{}, len(allPhotos))
	for _, p := range allPhotos {
		uids[p.UID] = struct{}{}
	}
	return uids, nil
}

// stripDupSuffix removes PhotoPrism's duplicate suffix like "(0)", "(1)" from
// a filename base (without extension). E.g. "photo(0)" → "photo".
func stripDupSuffix(base string) string {
	if !strings.HasSuffix(base, ")") {
		return base
	}
	idx := strings.LastIndex(base, "(")
	if idx < 1 {
		return base
	}
	// Check that content between parens is all digits.
	inner := base[idx+1 : len(base)-1]
	for _, c := range inner {
		if c < '0' || c > '9' {
			return base
		}
	}
	return base[:idx]
}

// listFilesInDir returns all file paths in a directory.
func listFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

// processUploadTokens processes upload tokens with concurrency and progress events.
// Returns the number of successfully processed and timed-out tokens.
func processUploadTokens(
	ctx context.Context, pp *photoprism.PhotoPrism,
	tokens []string, albumUIDs []string, job *UploadJob,
) (processed, timedOut int) {
	sem := make(chan struct{}, constants.UploadProcessConcurrency)
	var wg sync.WaitGroup
	var processedCount atomic.Int64
	var timedOutCount atomic.Int64
	total := len(tokens)

	for _, token := range tokens {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := pp.ProcessUpload(t, albumUIDs[:1]); err != nil {
				if ctx.Err() == nil {
					// Timeout or other error — log warning but continue.
					log.Printf("Warning: process upload %s: %v", t, err)
					timedOutCount.Add(1)
				}
			} else {
				processedCount.Add(1)
			}
			current := processedCount.Load() + timedOutCount.Load()
			job.SendEvent(JobEvent{
				Type: "processing_upload",
				Data: map[string]any{
					"current": current,
					"total":   total,
				},
			})
		}(token)
	}
	wg.Wait()
	return int(processedCount.Load()), int(timedOutCount.Load())
}

// applyLabels applies labels to photos with progress events.
func applyLabels(
	ctx context.Context, pp *photoprism.PhotoPrism,
	photoUIDs, labels []string, job *UploadJob,
) int {
	var applied int64
	total := len(photoUIDs) * len(labels)
	sem := make(chan struct{}, constants.UploadProcessConcurrency)
	var wg sync.WaitGroup

	for _, uid := range photoUIDs {
		for _, label := range labels {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(photoUID, labelName string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				_, err := pp.AddPhotoLabel(photoUID, photoprism.PhotoLabel{
					Name: labelName, LabelSrc: "manual", Uncertainty: 0,
				})
				if err != nil {
					log.Printf("Warning: label %s on %s: %v",
						labelName, photoUID, err)
				} else {
					count := atomic.AddInt64(&applied, 1)
					job.SendEvent(JobEvent{
						Type: "applying_labels",
						Data: map[string]any{
							"current": count, "total": total,
						},
					})
				}
			}(uid, label)
		}
	}
	wg.Wait()
	return int(applied)
}

// autoProcessPhotos runs embeddings/face detection for new photos.
func (h *UploadHandler) autoProcessPhotos(
	ctx context.Context, job *UploadJob,
	session *middleware.Session, photoUIDs []string,
) {
	opts := ProcessJobOptions{Concurrency: constants.DefaultConcurrency}
	repos, err := initProcessJobRepos(ctx, opts)
	if err != nil {
		log.Printf("Warning: auto-process init repos failed: %v", err)
		return
	}

	clients, err := h.processHandler.initProcessJobClients(session)
	if err != nil {
		log.Printf("Warning: auto-process init clients failed: %v", err)
		return
	}

	var processed atomic.Int64
	total := len(photoUIDs)
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup

	for _, uid := range photoUIDs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(photoUID string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			counters := &processJobCounters{}
			processOnePhoto(
				ctx, clients.pp,
				clients.embClient, clients.faceClient,
				repos, photoUID, counters,
			)
			count := processed.Add(1)
			job.SendEvent(JobEvent{
				Type: "process_progress",
				Data: map[string]any{
					"processed": count, "total": total,
				},
			})
		}(uid)
	}
	wg.Wait()
}

func (h *UploadHandler) failUploadJob(job *UploadJob, message string) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusFailed
	job.Error = message
	job.CompletedAt = &now
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "job_error", Message: message})
}

func (h *UploadHandler) cancelUploadJob(job *UploadJob) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCancelled
	job.CompletedAt = &now
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "cancelled", Message: "Job cancelled"})
}

func (h *UploadHandler) completeUploadJob(
	job *UploadJob, result *UploadJobResult,
) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.Result = result
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "completed", Data: result})
}
