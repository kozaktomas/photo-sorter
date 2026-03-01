package handlers

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// UploadHandler handles file upload endpoints.
type UploadHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
}

// NewUploadHandler creates a new upload handler.
func NewUploadHandler(cfg *config.Config, sm *middleware.SessionManager) *UploadHandler {
	return &UploadHandler{
		config:         cfg,
		sessionManager: sm,
	}
}

// saveUploadedFiles saves multipart files to a temporary directory and returns their paths.
func saveUploadedFiles(files []*multipart.FileHeader, tempDir string) ([]string, error) {
	var filePaths []string
	for _, fileHeader := range files {
		if err := func() error {
			file, err := fileHeader.Open()
			if err != nil {
				return fmt.Errorf("failed to open file: %s", fileHeader.Filename)
			}
			defer file.Close()

			safeName := filepath.Base(fileHeader.Filename)
			tempPath := filepath.Join(tempDir, safeName)
			out, err := os.Create(tempPath) //nolint:gosec // filename sanitized via filepath.Base
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
			return nil, err
		}
	}
	return filePaths, nil
}

// Upload handles multipart file uploads.
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
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

	respondJSON(w, http.StatusOK, map[string]any{
		"uploaded": len(filePaths),
		"album":    albumUID,
	})
}
