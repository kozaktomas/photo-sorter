package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// ApplyRequest represents a request to apply a face match (create marker or assign person)
type ApplyRequest struct {
	PhotoUID   string      `json:"photo_uid"`
	PersonName string      `json:"person_name"`
	Action     MatchAction `json:"action"`
	MarkerUID  string      `json:"marker_uid,omitempty"`
	FileUID    string      `json:"file_uid,omitempty"`
	BBoxRel    []float64   `json:"bbox_rel,omitempty"`
	FaceIndex  int         `json:"face_index,omitempty"` // For cache sync
}

// ApplyResponse represents the response after applying a face match
type ApplyResponse struct {
	Success   bool   `json:"success"`
	MarkerUID string `json:"marker_uid,omitempty"`
	Error     string `json:"error,omitempty"`
}

// applyCreateMarker handles the create_marker action
func (h *FacesHandler) applyCreateMarker(w http.ResponseWriter, pp *photoprism.PhotoPrism, req ApplyRequest) {
	if req.FileUID == "" || len(req.BBoxRel) != 4 {
		respondError(w, http.StatusBadRequest, "file_uid and bbox_rel are required for create_marker")
		return
	}

	marker := photoprism.MarkerCreate{
		FileUID: req.FileUID,
		Type:    "face",
		X:       req.BBoxRel[0],
		Y:       req.BBoxRel[1],
		W:       req.BBoxRel[2],
		H:       req.BBoxRel[3],
		Name:    req.PersonName,
		Src:     "manual",
		SubjSrc: "manual",
	}

	created, err := pp.CreateMarker(marker)
	if err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, created.UID, created.SubjUID, req.PersonName)
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: created.UID})
}

// applyAssignPerson handles the assign_person action
func (h *FacesHandler) applyAssignPerson(w http.ResponseWriter, pp *photoprism.PhotoPrism, req ApplyRequest) {
	if req.MarkerUID == "" {
		respondError(w, http.StatusBadRequest, "marker_uid is required for assign_person")
		return
	}

	update := photoprism.MarkerUpdate{
		Name:    req.PersonName,
		SubjSrc: "manual",
	}

	updated, err := pp.UpdateMarker(req.MarkerUID, update)
	if err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, req.MarkerUID, updated.SubjUID, req.PersonName)
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: req.MarkerUID})
}

// applyUnassignPerson handles the unassign_person action
func (h *FacesHandler) applyUnassignPerson(w http.ResponseWriter, pp *photoprism.PhotoPrism, req ApplyRequest) {
	if req.MarkerUID == "" {
		respondError(w, http.StatusBadRequest, "marker_uid is required for unassign_person")
		return
	}

	_, err := pp.ClearMarkerSubject(req.MarkerUID)
	if err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, req.MarkerUID, "", "")
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: req.MarkerUID})
}

// Apply creates a marker or assigns a person to an existing marker
func (h *FacesHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.PhotoUID == "" || req.PersonName == "" {
		respondError(w, http.StatusBadRequest, "photo_uid and person_name are required")
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	switch req.Action {
	case ActionCreateMarker:
		h.applyCreateMarker(w, pp, req)
	case ActionAssignPerson:
		h.applyAssignPerson(w, pp, req)
	case ActionUnassignPerson:
		h.applyUnassignPerson(w, pp, req)
	default:
		respondError(w, http.StatusBadRequest, "invalid action")
	}
}

// syncFaceCache updates the face cache with new marker/subject data
func (h *FacesHandler) syncFaceCache(photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) {
	h.writerMu.Lock()
	defer h.writerMu.Unlock()

	if h.faceWriter == nil {
		return // Read-only mode
	}

	ctx := context.Background()
	// Update face marker data in PostgreSQL (persisted automatically)
	if err := h.faceWriter.UpdateFaceMarker(ctx, photoUID, faceIndex, markerUID, subjectUID, subjectName); err != nil {
		log.Printf("Warning: failed to update face cache for %s face %d: %v", photoUID, faceIndex, err)
	}
}

// ComputeFacesResponse represents the response after computing faces
type ComputeFacesResponse struct {
	PhotoUID   string `json:"photo_uid"`
	FacesCount int    `json:"faces_count"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// computeAndSaveImageEmbedding computes and saves the CLIP image embedding for a photo (best-effort)
func computeAndSaveImageEmbedding(ctx context.Context, embURL string, imageData []byte, photoUID string) {
	embClient := fingerprint.NewEmbeddingClient(embURL, "clip")
	resizedData, err := fingerprint.ResizeImage(imageData, 1920)
	if err != nil {
		return
	}
	result, err := embClient.ComputeEmbeddingWithMetadata(ctx, resizedData)
	if err != nil {
		return
	}
	embReader, err := database.GetEmbeddingReader(ctx)
	if err != nil {
		return
	}
	if embWriter, ok := embReader.(interface {
		Save(ctx context.Context, photoUID string, embedding []float32, model, pretrained string, dim int) error
	}); ok {
		embWriter.Save(ctx, photoUID, result.Embedding, result.Model, result.Pretrained, result.Dim)
	}
}

// computeFaceEmbeddings computes face embeddings and converts them to StoredFace structs
func computeFaceEmbeddings(ctx context.Context, embURL string, imageData []byte, photoUID string) ([]database.StoredFace, error) {
	faceClient := fingerprint.NewEmbeddingClient(embURL, "faces")
	faceResult, err := faceClient.ComputeFaceEmbeddings(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("computing face embeddings: %w", err)
	}
	faces := make([]database.StoredFace, len(faceResult.Faces))
	for i, f := range faceResult.Faces {
		faces[i] = database.StoredFace{
			PhotoUID:  photoUID,
			FaceIndex: f.FaceIndex,
			Embedding: f.Embedding,
			BBox:      f.BBox,
			DetScore:  f.DetScore,
			Model:     faceResult.Model,
			Dim:       f.Dim,
		}
	}
	return faces, nil
}

// ComputeFaces detects and stores face and image embeddings for a single photo.
// This recalculates embeddings even if they already exist (useful for reprocessing).
func (h *FacesHandler) ComputeFaces(w http.ResponseWriter, r *http.Request) {
	photoUID := chi.URLParam(r, "uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "photo_uid is required")
		return
	}

	if !database.IsInitialized() {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, Success: false, Error: "database not configured"})
		return
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	embURL := h.config.Embedding.URL
	if embURL == "" {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, Success: false, Error: "embedding service not configured (EMBEDDING_URL)"})
		return
	}

	imageData, _, err := pp.GetPhotoDownload(photoUID)
	if err != nil {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, Success: false, Error: fmt.Sprintf("failed to download photo: %v", err)})
		return
	}

	ctx := r.Context()

	// Compute and save image embedding (best-effort)
	computeAndSaveImageEmbedding(ctx, embURL, imageData, photoUID)

	// Compute face embeddings
	faces, err := computeFaceEmbeddings(ctx, embURL, imageData, photoUID)
	if err != nil {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, Success: false, Error: fmt.Sprintf("failed to compute faces: %v", err)})
		return
	}

	h.writerMu.Lock()
	if h.faceWriter != nil {
		if err := h.faceWriter.SaveFaces(ctx, photoUID, faces); err != nil {
			h.writerMu.Unlock()
			respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, Success: false, Error: fmt.Sprintf("failed to save faces: %v", err)})
			return
		}
		enrichFacesWithMarkerData(pp, h.faceWriter, photoUID, faces)
		h.faceWriter.MarkFacesProcessed(ctx, photoUID, len(faces))
	}
	h.writerMu.Unlock()

	respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, FacesCount: len(faces), Success: true})
}
