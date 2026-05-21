// Package handlers provides HTTP handlers for the web API.
// This file contains the FacesHandler struct and constructor.
// Handler methods are organized in separate files:.
//   - subjects.go: Subject CRUD operations (ListSubjects, GetSubject, UpdateSubject)
//   - face_match.go: Face matching and similarity search (Match)
//   - face_apply.go: Applying face matches (Apply, ComputeFaces)
//   - face_outliers.go: Outlier detection (FindOutliers)
//   - face_photos.go: Photo face retrieval and suggestions (GetPhotoFaces)
//   - face_helpers.go: Shared helper functions
package handlers

import (
	"context"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// FacesHandler handles face-related endpoints. The marker / subject / photo
// repositories back the native data path (replacing the previous PhotoPrism
// marker / subject calls). They may be nil in tests that exercise paths
// which do not touch markers or subjects.
type FacesHandler struct {
	config         *config.Config
	sessionManager *middleware.SessionManager
	faceReader     database.FaceReader
	faceWriter     database.FaceWriter // For cache sync on Apply
	markerRepo     database.MarkerWriter
	subjectRepo    database.SubjectWriter
	photoReader    database.PhotoReader
	storage        *storage.Storage
	writerMu       sync.Mutex // Protects faceWriter
}

// NewFacesHandler creates a new faces handler. markerRepo, subjectRepo,
// photoReader and store back the native marker/subject/photo path; any of
// them may be nil in transitional environments — affected endpoints will
// then surface a 503 rather than blocking server startup.
func NewFacesHandler(
	cfg *config.Config, sm *middleware.SessionManager,
	markerRepo database.MarkerWriter,
	subjectRepo database.SubjectWriter,
	photoReader database.PhotoReader,
	store *storage.Storage,
) *FacesHandler {
	h := &FacesHandler{
		config:         cfg,
		sessionManager: sm,
		markerRepo:     markerRepo,
		subjectRepo:    subjectRepo,
		photoReader:    photoReader,
		storage:        store,
	}
	// Try to get a face reader from PostgreSQL.
	if reader, err := database.GetFaceReader(context.Background()); err == nil {
		h.faceReader = reader
	}
	// Try to get a face writer for cache sync.
	if writer, err := database.GetFaceWriter(context.Background()); err == nil {
		h.faceWriter = writer
	}
	return h
}

// RefreshReader reloads the face reader from the database.
// Called after processing completes to pick up new face data.
func (h *FacesHandler) RefreshReader() {
	if reader, err := database.GetFaceReader(context.Background()); err == nil {
		h.faceReader = reader
	}
	// Also refresh the writer.
	h.writerMu.Lock()
	defer h.writerMu.Unlock()
	if writer, err := database.GetFaceWriter(context.Background()); err == nil {
		h.faceWriter = writer
	}
}
