package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/latex"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// TTLs for the book export job lifecycle. Completed jobs linger a short
// while so a mid-download network blip can retry without re-running the
// whole ~4-minute export.
const (
	bookExportUnconsumedTTL = time.Hour
	bookExportConsumedTTL   = 10 * time.Minute
	bookExportTerminalTTL   = 5 * time.Minute
	bookExportSweepInterval = time.Minute
)

// BookExportJob represents an async PDF export job for a photo book.
// Progress is reported via SSE events; the finished PDF is served from a
// temp file (see pdfPath) to avoid keeping 700 MB in memory.
type BookExportJob struct {
	EventBroadcaster

	ID          string     `json:"id"`
	BookID      string     `json:"book_id"`
	BookTitle   string     `json:"book_title"`
	Status      JobStatus  `json:"status"`
	Phase       string     `json:"phase"`
	Current     int        `json:"current"`
	Total       int        `json:"total"`
	FileSize    int64      `json:"file_size,omitempty"`
	Filename    string     `json:"filename,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Consumed    bool       `json:"consumed"`
	Debug       bool       `json:"debug,omitempty"`

	pdfPath   string
	expiresAt time.Time
}

// GetStatus returns the current job status (implements SSEJob).
func (j *BookExportJob) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// Cancel cancels the export job. Cancels the underlying context (which
// SIGKILLs any running lualatex process) and sends a cancelled event.
func (j *BookExportJob) Cancel() {
	j.EventBroadcaster.Cancel()
	j.mu.Lock()
	if j.Status != JobStatusCompleted && j.Status != JobStatusFailed {
		j.Status = JobStatusCancelled
	}
	j.expiresAt = time.Now().Add(bookExportTerminalTTL)
	j.mu.Unlock()
}

// isExpired reports whether the job is past its TTL window.
func (j *BookExportJob) isExpired(now time.Time) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return !j.expiresAt.IsZero() && now.After(j.expiresAt)
}

// removeTempFile deletes the backing PDF file if present. Idempotent.
func (j *BookExportJob) removeTempFile() {
	j.mu.Lock()
	path := j.pdfPath
	j.pdfPath = ""
	j.mu.Unlock()
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("BookExportJob %s: failed to remove %s: %v", j.ID, path, err)
	}
}

// BookExportJobManager tracks active book export jobs and periodically
// sweeps expired ones.
type BookExportJobManager struct {
	jobs map[string]*BookExportJob
	mu   sync.RWMutex
	stop chan struct{}
	wg   sync.WaitGroup
}

// NewBookExportJobManager creates a manager and starts the TTL sweeper.
func NewBookExportJobManager() *BookExportJobManager {
	m := &BookExportJobManager{
		jobs: make(map[string]*BookExportJob),
		stop: make(chan struct{}),
	}
	m.wg.Add(1)
	go m.sweepLoop()
	return m
}

// CreateJob atomically creates a new job for a book. Returns the new job on
// success, or the existing active job (with err != nil) if one is already
// running for the same book.
func (m *BookExportJobManager) CreateJob(
	id, bookID, bookTitle string, debug bool,
) (*BookExportJob, *BookExportJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.jobs {
		if existing.BookID != bookID {
			continue
		}
		existing.mu.RLock()
		active := existing.Status == JobStatusPending || existing.Status == JobStatusRunning
		existing.mu.RUnlock()
		if active {
			return nil, existing, fmt.Errorf("export already in progress for book %s", bookID)
		}
	}

	job := &BookExportJob{
		ID:        id,
		BookID:    bookID,
		BookTitle: bookTitle,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Debug:     debug,
	}
	m.jobs[id] = job
	return job, nil, nil
}

// GetJob returns a job by ID or nil.
func (m *BookExportJobManager) GetJob(id string) *BookExportJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

// DeleteJob removes a job and cleans up its temp file.
func (m *BookExportJobManager) DeleteJob(id string) {
	m.mu.Lock()
	job := m.jobs[id]
	delete(m.jobs, id)
	m.mu.Unlock()
	if job != nil {
		job.removeTempFile()
	}
}

// Shutdown stops the sweeper and removes every backing temp file. Safe to
// call multiple times.
func (m *BookExportJobManager) Shutdown() {
	select {
	case <-m.stop:
		// already closed
	default:
		close(m.stop)
	}
	m.wg.Wait()
	m.mu.Lock()
	toRemove := make([]*BookExportJob, 0, len(m.jobs))
	for id, job := range m.jobs {
		toRemove = append(toRemove, job)
		delete(m.jobs, id)
	}
	m.mu.Unlock()
	for _, job := range toRemove {
		job.removeTempFile()
	}
}

func (m *BookExportJobManager) sweepLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(bookExportSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

// sweep removes jobs past their TTL and deletes their temp files.
func (m *BookExportJobManager) sweep() {
	now := time.Now()
	m.mu.Lock()
	var toRemove []*BookExportJob
	for id, job := range m.jobs {
		if job.isExpired(now) {
			toRemove = append(toRemove, job)
			delete(m.jobs, id)
		}
	}
	m.mu.Unlock()
	for _, job := range toRemove {
		log.Printf("BookExportJob %s: expired, cleaning up", job.ID)
		job.removeTempFile()
	}
}

// ============================================================================
// HTTP handlers (methods on BooksHandler; the struct is defined in books.go).
// ============================================================================

// StartExportJob handles POST /api/v1/books/{id}/export-pdf/job.
// It validates the request, creates a background job, and returns 202 with
// the job ID. The job runs in a goroutine and reports progress via SSE.
func (h *BooksHandler) StartExportJob(w http.ResponseWriter, r *http.Request) {
	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}
	bw := getBookWriter(r, w)
	if bw == nil {
		return
	}

	bookID := chi.URLParam(r, "id")
	book, err := bw.GetBook(r.Context(), bookID)
	if err != nil || book == nil {
		respondError(w, http.StatusNotFound, "book not found")
		return
	}

	if _, err := exec.LookPath("lualatex"); err != nil {
		respondError(w, http.StatusServiceUnavailable, "lualatex is not installed on the server")
		return
	}

	debug := r.URL.Query().Get("format") == exportFormatDebug
	session := middleware.GetSessionFromContext(r.Context())
	jobID := uuid.New().String()

	job, existing, err := h.exportJobs.CreateJob(jobID, bookID, book.Title, debug)
	if err != nil {
		respondJSON(w, http.StatusConflict, map[string]any{
			"error":  "export already in progress for this book",
			"job_id": existing.ID,
			"status": existing.Status,
		})
		return
	}

	go h.runBookExportJob(job, session) //nolint:gosec // G118 - background job outlives HTTP request

	respondJSON(w, http.StatusAccepted, map[string]any{
		"job_id":     jobID,
		"book_id":    bookID,
		"book_title": book.Title,
		"status":     string(JobStatusPending),
	})
}

// GetExportJob handles GET /api/v1/book-export/{jobId}.
func (h *BooksHandler) GetExportJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := h.exportJobs.GetJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}
	respondJSON(w, http.StatusOK, job)
}

// StreamExportJobEvents handles GET /api/v1/book-export/{jobId}/events.
func (h *BooksHandler) StreamExportJobEvents(w http.ResponseWriter, r *http.Request) {
	streamSSEEvents(w, r,
		func(id string) SSEJob {
			job := h.exportJobs.GetJob(id)
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

// DownloadExport handles GET /api/v1/book-export/{jobId}/download. Streams
// the compiled PDF temp file via http.ServeContent (which handles
// Content-Length and range requests automatically). On successful serve,
// the job's TTL is shortened to the "consumed" window.
func (h *BooksHandler) DownloadExport(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := h.exportJobs.GetJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	job.mu.RLock()
	status := job.Status
	path := job.pdfPath
	filename := job.Filename
	completedAt := job.CompletedAt
	expired := !job.expiresAt.IsZero() && time.Now().After(job.expiresAt)
	job.mu.RUnlock()

	if status != JobStatusCompleted {
		respondError(w, http.StatusConflict, "job is not completed: "+string(status))
		return
	}
	if expired || path == "" {
		respondError(w, http.StatusGone, "export file is no longer available")
		return
	}

	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		respondError(w, http.StatusGone, "export file is no longer available")
		return
	}
	defer file.Close()

	if filename == "" {
		filename = "book.pdf"
	}
	modTime := time.Now()
	if completedAt != nil {
		modTime = *completedAt
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// Disable nginx/proxy buffering so the browser sees chunks as they
	// arrive over the wire (required for download progress to be meaningful).
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeContent(w, r, filename, modTime, file)

	job.mu.Lock()
	job.Consumed = true
	job.expiresAt = time.Now().Add(bookExportConsumedTTL)
	job.mu.Unlock()
}

// CancelExportJob handles DELETE /api/v1/book-export/{jobId}.
func (h *BooksHandler) CancelExportJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := h.exportJobs.GetJob(jobID)
	if job == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}
	job.Cancel()
	job.removeTempFile()
	respondJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

// ============================================================================
// Background runner.
// ============================================================================

// runBookExportJob is the background goroutine that actually runs the
// export. It instantiates its own PhotoPrism client from the session (the
// request context is gone by the time this runs), calls latex with a
// progress translator, writes the PDF to a temp file, and emits the
// terminal SSE event.
func (h *BooksHandler) runBookExportJob(job *BookExportJob, session *middleware.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	defer cancel()

	job.mu.Lock()
	job.Status = JobStatusRunning
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "started", Message: "Book export started"})

	pdfData, ok := h.generateBookPDF(ctx, job, session)
	if !ok {
		return
	}

	tmpPath, ok := h.materializeExportFile(job, pdfData)
	if !ok {
		return
	}

	h.finalizeBookExport(job, tmpPath, pdfData)
}

// generateBookPDF runs the latex pipeline with progress translation. Returns
// the PDF bytes on success, or (nil, false) after emitting the appropriate
// terminal event (fail/cancel) on failure.
func (h *BooksHandler) generateBookPDF(
	ctx context.Context, job *BookExportJob, session *middleware.Session,
) ([]byte, bool) {
	pp, err := getPhotoPrismClient(h.config, session)
	if err != nil {
		h.failBookExportJob(job, "failed to connect to PhotoPrism: "+err.Error())
		return nil, false
	}

	bw, err := database.GetBookWriter(ctx)
	if err != nil {
		h.failBookExportJob(job, "book storage not available: "+err.Error())
		return nil, false
	}

	job.mu.Lock()
	job.Phase = "fetching_metadata"
	job.mu.Unlock()
	job.SendEvent(JobEvent{
		Type: "progress",
		Data: map[string]any{"phase": "fetching_metadata"},
	})

	opts := latex.ExportOptions{
		Debug:      job.Debug,
		OnProgress: h.exportProgressTranslator(job),
	}
	pdfData, _, err := latex.GeneratePDFWithCallbacks(ctx, pp, bw, job.BookID, opts)
	if err != nil {
		if ctx.Err() != nil {
			h.cancelBookExportJob(job)
			return nil, false
		}
		h.failBookExportJob(job, fmt.Sprintf("PDF generation failed: %v", err))
		return nil, false
	}

	// Race: cancel may have fired between latex finishing and here.
	if ctx.Err() != nil {
		h.cancelBookExportJob(job)
		return nil, false
	}
	return pdfData, true
}

// materializeExportFile writes the PDF bytes to a temp file on disk. Returns
// the path on success, or ("", false) after emitting a failed event.
func (h *BooksHandler) materializeExportFile(job *BookExportJob, pdfData []byte) (string, bool) {
	tmpFile, err := os.CreateTemp("", "book-export-*.pdf")
	if err != nil {
		h.failBookExportJob(job, "failed to create temp file: "+err.Error())
		return "", false
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(pdfData); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		h.failBookExportJob(job, "failed to write temp file: "+err.Error())
		return "", false
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		h.failBookExportJob(job, "failed to close temp file: "+err.Error())
		return "", false
	}
	return tmpPath, true
}

// finalizeBookExport atomically transitions the job to completed and emits
// the "completed" SSE event. If the job has been cancelled while we were
// writing, the temp file is discarded instead of overwriting the status.
func (h *BooksHandler) finalizeBookExport(job *BookExportJob, tmpPath string, pdfData []byte) {
	filename := sanitizePDFFilename(job.BookTitle)
	now := time.Now()

	job.mu.Lock()
	if job.Status != JobStatusRunning {
		job.mu.Unlock()
		_ = os.Remove(tmpPath)
		return
	}
	job.Status = JobStatusCompleted
	job.CompletedAt = &now
	job.Filename = filename
	job.FileSize = int64(len(pdfData))
	job.pdfPath = tmpPath
	job.Phase = "done"
	job.expiresAt = now.Add(bookExportUnconsumedTTL)
	job.mu.Unlock()

	job.SendEvent(JobEvent{
		Type: "completed",
		Data: map[string]any{
			"job_id":       job.ID,
			"filename":     filename,
			"file_size":    len(pdfData),
			"download_url": "/api/v1/book-export/" + job.ID + "/download",
		},
	})
}

// exportProgressTranslator returns a callback that converts latex
// ProgressInfo into a JobEvent and updates the job's phase/counters.
func (h *BooksHandler) exportProgressTranslator(job *BookExportJob) func(latex.ProgressInfo) {
	return func(info latex.ProgressInfo) {
		job.mu.Lock()
		job.Phase = info.Phase
		job.Current = info.Current
		job.Total = info.Total
		job.mu.Unlock()

		data := map[string]any{
			"phase":   info.Phase,
			"current": info.Current,
			"total":   info.Total,
		}
		if info.PhotoUID != "" {
			data["photo_uid"] = info.PhotoUID
		}
		job.SendEvent(JobEvent{Type: "progress", Data: data})
	}
}

func (h *BooksHandler) failBookExportJob(job *BookExportJob, message string) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusFailed
	job.Error = message
	job.CompletedAt = &now
	job.expiresAt = now.Add(bookExportTerminalTTL)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "job_error", Message: message})
}

func (h *BooksHandler) cancelBookExportJob(job *BookExportJob) {
	now := time.Now()
	job.mu.Lock()
	job.Status = JobStatusCancelled
	job.CompletedAt = &now
	job.expiresAt = now.Add(bookExportTerminalTTL)
	job.mu.Unlock()
	job.SendEvent(JobEvent{Type: "cancelled", Message: "Job was cancelled"})
}

// ============================================================================
// Helpers.
// ============================================================================

// sanitizePDFFilename produces a safe Content-Disposition filename from a
// book title. Replaces characters that confuse HTTP parsers (quotes,
// newlines, backslashes) with underscores and appends ".pdf".
func sanitizePDFFilename(title string) string {
	if title == "" {
		title = "book"
	}
	b := make([]byte, 0, len(title)+4)
	for i := range len(title) {
		c := title[i]
		switch c {
		case '"', '\\', '\n', '\r', '\t':
			b = append(b, '_')
		default:
			b = append(b, c)
		}
	}
	return string(b) + ".pdf"
}
