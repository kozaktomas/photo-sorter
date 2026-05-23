package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// UploadHandler handles file upload endpoints.
type UploadHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	jobManager     *UploadJobManager
	processHandler *ProcessHandler
}

// NewUploadHandler creates a new upload handler.
func NewUploadHandler(cfg *config.Config, sm *middleware.SessionManager, ph *ProcessHandler) *UploadHandler {
	return &UploadHandler{
		config:         cfg,
		sessionManager: sm,
		jobManager:     NewUploadJobManager(),
		processHandler: ph,
	}
}

// saveUploadedFiles saves multipart files to a temporary directory and
// returns their paths. Each file lands in a per-index subdirectory so
// two uploads with the same basename (e.g. duplicate IMG_001.jpg from
// different cameras) cannot clobber each other. The original basename
// is preserved inside that subdirectory so the downstream pipeline still
// sees the user-supplied filename for EXIF / on-disk path heuristics.
func saveUploadedFiles(files []*multipart.FileHeader, tempDir string) ([]string, error) {
	filePaths := make([]string, 0, len(files))
	for i, fileHeader := range files {
		tempPath, err := saveSingleUploadedFile(fileHeader, tempDir, i)
		if err != nil {
			return nil, err
		}
		filePaths = append(filePaths, tempPath)
	}
	return filePaths, nil
}

// saveSingleUploadedFile writes one multipart file into a per-index
// subdir under tempDir and returns the resulting path. The subdir
// scheme is what guarantees no two uploads with the same basename
// collide; the basename itself is preserved so the downstream pipeline
// still sees the user-supplied filename.
func saveSingleUploadedFile(fh *multipart.FileHeader, tempDir string, idx int) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open file: %s", fh.Filename)
	}
	defer file.Close()

	subDir := filepath.Join(tempDir, fmt.Sprintf("u%05d", idx))
	if mkErr := os.MkdirAll(subDir, 0o700); mkErr != nil {
		return "", errors.New("failed to create temp file")
	}

	safeName := filepath.Base(fh.Filename)
	if safeName == "." || safeName == "/" || safeName == "" {
		safeName = "upload"
	}
	tempPath := filepath.Join(subDir, safeName)
	//nolint:gosec // G304: filename via filepath.Base, path constrained to per-index subdir
	out, err := os.Create(tempPath)
	if err != nil {
		return "", errors.New("failed to create temp file")
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		return "", errors.New("failed to save file")
	}
	if err := out.Close(); err != nil {
		return "", errors.New("failed to save file")
	}
	return tempPath, nil
}

// Upload handles multipart file uploads.
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, constants.MaxUploadSize)
	if err := r.ParseMultipartForm(constants.MaxUploadSize); err != nil {
		respondError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	albumUID := r.FormValue("album_uid")
	if albumUID == "" {
		respondError(w, http.StatusBadRequest, "album_uid is required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	tempDir, err := os.MkdirTemp("", "photo-sorter-upload-*")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create temp directory")
		return
	}
	defer os.RemoveAll(tempDir)

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		respondError(w, http.StatusBadRequest, "no files provided")
		return
	}

	filePaths, err := saveUploadedFiles(files, tempDir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	uploadToken, err := pp.UploadFiles(filePaths)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to upload files: %v", err))
		return
	}

	if err := pp.ProcessUpload(uploadToken, []string{albumUID}); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to process upload: %v", err))
		return
	}

	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionPhotoUpload, audit.EntityAlbum, albumUID,
		map[string]any{"count": len(filePaths)},
	)
	respondJSON(w, http.StatusOK, map[string]any{
		"uploaded": len(filePaths),
		"album":    albumUID,
	})
}

// parseUploadJobOptions extracts job configuration from form fields.
func parseUploadJobOptions(r *http.Request) (*UploadJobOptions, error) {
	var albumUIDs []string
	if raw := r.FormValue("album_uids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &albumUIDs); err != nil {
			return nil, errors.New("invalid album_uids format")
		}
	}
	if len(albumUIDs) == 0 {
		return nil, errors.New("at least one album is required")
	}

	var labels []string
	if raw := r.FormValue("labels"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &labels); err != nil {
			return nil, errors.New("invalid labels format")
		}
	}

	return &UploadJobOptions{
		AlbumUIDs:     albumUIDs,
		Labels:        labels,
		BookSectionID: r.FormValue("book_section_id"),
		AutoProcess:   r.FormValue("auto_process") != "false",
		FileCount:     len(r.MultipartForm.File["files"]),
	}, nil
}

// StartJob starts a background upload job.
//
//nolint:funlen,cyclop // straight-line validation + multipart parse + job dispatch + audit.
func (h *UploadHandler) StartJob(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
	if active := h.jobManager.GetActiveJob(); active != nil {
		if active.Status == JobStatusRunning || active.Status == JobStatusPending {
			respondError(w, http.StatusConflict, "an upload job is already running")
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, constants.MaxUploadJobSize)
	if err := r.ParseMultipartForm(constants.MaxUploadJobSize); err != nil {
		respondError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		respondError(w, http.StatusBadRequest, "no files provided")
		return
	}

	opts, err := parseUploadJobOptions(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tempDir, err := os.MkdirTemp("", "photo-sorter-upload-job-*")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create temp directory")
		return
	}

	if _, err := saveUploadedFiles(files, tempDir); err != nil {
		os.RemoveAll(tempDir)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	session := middleware.GetSessionFromContext(r.Context())

	jobID := uuid.New().String()
	job := &UploadJob{
		ID:        jobID,
		Status:    JobStatusPending,
		StartedAt: time.Now(),
		Options:   *opts,
	}

	// Atomic claim of the single active-job slot. The earlier GetActiveJob
	// check is best-effort (it lets us short-circuit before parsing the
	// multipart body); StartIfIdle is what actually guarantees only one
	// upload job runs at a time — two concurrent requests can both pass the
	// early check, but only one wins StartIfIdle.
	if ok, existing := h.jobManager.StartIfIdle(job); !ok {
		os.RemoveAll(tempDir)
		respondJSON(w, http.StatusConflict, map[string]any{
			"error":  "an upload job is already running",
			"job_id": existing.ID,
			"status": existing.Status,
		})
		return
	}
	// runUploadJob's cancellation context was initialised by StartIfIdle so
	// a DELETE arriving before the goroutine is scheduled still propagates.
	go h.runUploadJob(job, session, tempDir)

	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionPhotoUpload, audit.EntityProcessJob, jobID,
		map[string]any{
			"file_count": opts.FileCount,
			"album_uids": opts.AlbumUIDs,
		},
	)
	respondJSON(w, http.StatusAccepted, map[string]string{
		"job_id": jobID,
		"status": string(JobStatusPending),
	})
}

// GetJobEvents streams upload job events via SSE.
func (h *UploadHandler) GetJobEvents(w http.ResponseWriter, r *http.Request) {
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

// CancelJob cancels an upload job.
func (h *UploadHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	if err := requireWriteRole(r); err != nil {
		respondError(w, http.StatusForbidden, "forbidden")
		return
	}
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
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionUploadJobCancel, audit.EntityProcessJob, jobID, nil,
	)
	respondJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}
