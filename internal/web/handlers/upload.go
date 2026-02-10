package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// UploadHandler handles file upload endpoints
type UploadHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewUploadHandler creates a new upload handler
func NewUploadHandler(cfg *config.Config, sm *middleware.SessionManager) *UploadHandler {
	return &UploadHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// Upload handles multipart file uploads
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form with max upload size limit
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

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		respondError(w, http.StatusBadRequest, "no files provided")
		return
	}

	// Create temp directory for uploads
	tempDir, err := os.MkdirTemp("", "photo-sorter-upload-*")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create temp directory")
		return
	}
	defer os.RemoveAll(tempDir)

	// Save files to temp directory
	var filePaths []string
	for _, fileHeader := range files {
		if err := func() error {
			file, err := fileHeader.Open()
			if err != nil {
				return fmt.Errorf("failed to open file: %s", fileHeader.Filename)
			}
			defer file.Close()

			tempPath := filepath.Join(tempDir, fileHeader.Filename)
			out, err := os.Create(tempPath) //nolint:gosec // filename from multipart upload to temp dir
			if err != nil {
				return errors.New("failed to create temp file")
			}

			if _, err := io.Copy(out, file); err != nil {
				out.Close()
				return errors.New("failed to save file")
			}
			out.Close()

			filePaths = append(filePaths, tempPath)
			return nil
		}(); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Upload files to PhotoPrism
	uploadToken, err := pp.UploadFiles(filePaths)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to upload files: %v", err))
		return
	}

	// Process the upload and add to album
	if err := pp.ProcessUpload(uploadToken, []string{albumUID}); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to process upload: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"uploaded": len(filePaths),
		"album":    albumUID,
	})
}
