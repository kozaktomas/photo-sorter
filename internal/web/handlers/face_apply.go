package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/audit"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/fingerprint"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// ApplyRequest represents a request to apply a face match (create marker or assign person).
type ApplyRequest struct {
	PhotoUID   string      `json:"photo_uid"`
	PersonName string      `json:"person_name"`
	SubjectUID string      `json:"subject_uid,omitempty"`
	Action     MatchAction `json:"action"`
	MarkerUID  string      `json:"marker_uid,omitempty"`
	FileUID    string      `json:"file_uid,omitempty"`
	BBoxRel    []float64   `json:"bbox_rel,omitempty"`
	FaceIndex  int         `json:"face_index,omitempty"` // For cache sync
}

// ApplyResponse represents the response after applying a face match.
type ApplyResponse struct {
	Success   bool   `json:"success"`
	MarkerUID string `json:"marker_uid,omitempty"`
	Error     string `json:"error,omitempty"`
}

// resolveSubjectForApply returns the subject UID for an apply action: if the
// request already carries one it is returned as-is; otherwise the supplied
// PersonName / SubjectName is upserted via SubjectWriter.EnsureSubject. An
// empty name combined with an empty subject UID returns ("", nil) so the
// caller can fall back to a name-only marker (no subject assignment).
func (h *FacesHandler) resolveSubjectForApply(
	ctx context.Context, req ApplyRequest,
) (string, error) {
	if req.SubjectUID != "" {
		return req.SubjectUID, nil
	}
	name := strings.TrimSpace(req.PersonName)
	if name == "" {
		return "", nil
	}
	if h.subjectRepo == nil {
		return "", errors.New("subject storage not available")
	}
	subj, err := h.subjectRepo.EnsureSubject(ctx, name, "person")
	if err != nil {
		return "", fmt.Errorf("ensure subject: %w", err)
	}
	return subj.UID, nil
}

// applyCreateMarker handles the create_marker action.
func (h *FacesHandler) applyCreateMarker(w http.ResponseWriter, r *http.Request, req ApplyRequest) {
	if len(req.BBoxRel) != 4 {
		respondError(w, http.StatusBadRequest, "file_uid and bbox_rel are required for create_marker")
		return
	}
	if h.markerRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "marker storage not available")
		return
	}

	subjectUID, err := h.resolveSubjectForApply(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	marker := &database.Marker{
		PhotoUID:   req.PhotoUID,
		SubjectUID: subjectUID,
		Type:       "face",
		X:          req.BBoxRel[0],
		Y:          req.BBoxRel[1],
		W:          req.BBoxRel[2],
		H:          req.BBoxRel[3],
		Reviewed:   true,
	}

	if err := h.markerRepo.CreateMarker(r.Context(), marker); err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, marker.UID, subjectUID, req.PersonName)
	logFaceApply(r, req, marker.UID, subjectUID)
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: marker.UID})
}

// applyAssignPerson handles the assign_person action.
func (h *FacesHandler) applyAssignPerson(w http.ResponseWriter, r *http.Request, req ApplyRequest) {
	if req.MarkerUID == "" {
		respondError(w, http.StatusBadRequest, "marker_uid is required for assign_person")
		return
	}
	if h.markerRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "marker storage not available")
		return
	}

	subjectUID, err := h.resolveSubjectForApply(r.Context(), req)
	if err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}
	if subjectUID == "" {
		respondError(w, http.StatusBadRequest, "subject_uid or person_name is required for assign_person")
		return
	}

	if err := h.markerRepo.AssignSubject(r.Context(), req.MarkerUID, subjectUID); err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, req.MarkerUID, subjectUID, req.PersonName)
	logFaceApply(r, req, req.MarkerUID, subjectUID)
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: req.MarkerUID})
}

// applyUnassignPerson handles the unassign_person action.
func (h *FacesHandler) applyUnassignPerson(w http.ResponseWriter, r *http.Request, req ApplyRequest) {
	if req.MarkerUID == "" {
		respondError(w, http.StatusBadRequest, "marker_uid is required for unassign_person")
		return
	}
	if h.markerRepo == nil {
		respondError(w, http.StatusServiceUnavailable, "marker storage not available")
		return
	}

	if err := h.markerRepo.UnassignSubject(r.Context(), req.MarkerUID); err != nil {
		respondJSON(w, http.StatusOK, ApplyResponse{Success: false, Error: err.Error()})
		return
	}

	h.syncFaceCache(req.PhotoUID, req.FaceIndex, req.MarkerUID, "", "")
	logFaceApply(r, req, req.MarkerUID, "")
	respondJSON(w, http.StatusOK, ApplyResponse{Success: true, MarkerUID: req.MarkerUID})
}

// Apply creates a marker or assigns a person to an existing marker.
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

	switch req.Action {
	case ActionCreateMarker:
		h.applyCreateMarker(w, r, req)
	case ActionAssignPerson:
		h.applyAssignPerson(w, r, req)
	case ActionUnassignPerson:
		h.applyUnassignPerson(w, r, req)
	default:
		respondError(w, http.StatusBadRequest, "invalid action")
	}
}

// logFaceApply records a single audit row for a successful face-apply
// action. Pulled into a helper so each apply* method can emit it after
// the mutation lands without duplicating the metadata shape.
func logFaceApply(r *http.Request, req ApplyRequest, markerUID, subjectUID string) {
	audit.FromContext(r.Context()).Log(
		r.Context(), audit.ActionFaceApply, audit.EntityPhoto, req.PhotoUID,
		map[string]any{
			"action":      string(req.Action),
			"marker_uid":  markerUID,
			"subject_uid": subjectUID,
			"person_name": req.PersonName,
		},
	)
}

// syncFaceCache updates the face cache with new marker/subject data.
func (h *FacesHandler) syncFaceCache(photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) {
	h.writerMu.Lock()
	defer h.writerMu.Unlock()

	if h.faceWriter == nil {
		return // Read-only mode
	}

	ctx := context.Background()
	// Update face marker data in PostgreSQL (persisted automatically).
	if err := h.faceWriter.UpdateFaceMarker(ctx, photoUID, faceIndex, markerUID, subjectUID, subjectName); err != nil {
		log.Printf("Warning: failed to update face cache for %s face %d: %v", photoUID, faceIndex, err)
	}
}

// ComputeFacesResponse represents the response after computing faces.
type ComputeFacesResponse struct {
	PhotoUID   string `json:"photo_uid"`
	FacesCount int    `json:"faces_count"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// computeAndSaveImageEmbedding computes and saves the CLIP image embedding for a photo (best-effort).
func computeAndSaveImageEmbedding(ctx context.Context, embURL string, imageData []byte, photoUID string) {
	embClient, err := fingerprint.NewEmbeddingClient(embURL, "clip")
	if err != nil {
		return
	}
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

// computeFaceEmbeddings computes face embeddings and converts them to StoredFace structs.
func computeFaceEmbeddings(
	ctx context.Context, embURL string, imageData []byte, photoUID string,
) ([]database.StoredFace, error) {
	faceClient, err := fingerprint.NewEmbeddingClient(embURL, "faces")
	if err != nil {
		return nil, fmt.Errorf("invalid embedding config: %w", err)
	}
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

func (h *FacesHandler) saveFacesAndEnrich(
	ctx context.Context, ppClient *photoprism.PhotoPrism, photoUID string, faces []database.StoredFace,
) error {
	h.writerMu.Lock()
	defer h.writerMu.Unlock()

	if h.faceWriter == nil {
		return nil
	}
	if err := h.faceWriter.SaveFaces(ctx, photoUID, faces); err != nil {
		return fmt.Errorf("failed to save faces: %w", err)
	}
	enrichFacesWithMarkerData(ppClient, h.faceWriter, photoUID, faces)
	h.faceWriter.MarkFacesProcessed(ctx, photoUID, len(faces))
	return nil
}

// resolvePrimaryFilePathForPhoto returns the storage path of the photo's
// primary file, or an error message describing why it is unavailable.
func (h *FacesHandler) resolvePrimaryFilePathForPhoto(
	ctx context.Context, photoUID string,
) (string, string) {
	files, err := h.photoReader.ListPhotoFiles(ctx, photoUID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return "", "photo not found"
		}
		return "", fmt.Sprintf("failed to list photo files: %v", err)
	}
	if len(files) == 0 {
		return "", "photo has no files"
	}
	return resolvePrimaryFilePath(files), ""
}

// loadPhotoImageBytes reads the bytes of a photo's primary file via the
// native photo repository + storage layer. Returns an error message
// suitable for the API response when the photo cannot be opened.
func (h *FacesHandler) loadPhotoImageBytes(ctx context.Context, photoUID string) ([]byte, string) {
	if h.photoReader == nil || h.storage == nil {
		return nil, "photo storage not available"
	}
	primary, errMsg := h.resolvePrimaryFilePathForPhoto(ctx, photoUID)
	if errMsg != "" {
		return nil, errMsg
	}
	f, err := h.storage.OpenOriginal(primary)
	if err != nil {
		return nil, fmt.Sprintf("failed to open photo: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Sprintf("failed to read photo: %v", err)
	}
	return data, ""
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
		respondJSON(w, http.StatusOK, ComputeFacesResponse{
			PhotoUID: photoUID, Success: false, Error: "database not configured",
		})
		return
	}

	ppClient := middleware.MustGetPhotoPrism(r.Context(), w)
	if ppClient == nil {
		return
	}

	embURL := h.config.Embedding.URL
	if embURL == "" {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{
			PhotoUID: photoUID, Success: false,
			Error: "embedding service not configured (EMBEDDING_URL)",
		})
		return
	}

	ctx := r.Context()
	imageData, errMsg := h.loadPhotoImageBytes(ctx, photoUID)
	if errMsg != "" {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{
			PhotoUID: photoUID, Success: false, Error: errMsg,
		})
		return
	}

	// Compute and save image embedding (best-effort).
	computeAndSaveImageEmbedding(ctx, embURL, imageData, photoUID)

	// Compute face embeddings.
	faces, err := computeFaceEmbeddings(ctx, embURL, imageData, photoUID)
	if err != nil {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{
			PhotoUID: photoUID, Success: false,
			Error: fmt.Sprintf("failed to compute faces: %v", err),
		})
		return
	}

	if err := h.saveFacesAndEnrich(ctx, ppClient, photoUID, faces); err != nil {
		respondJSON(w, http.StatusOK, ComputeFacesResponse{
			PhotoUID: photoUID, Success: false,
			Error: err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, ComputeFacesResponse{PhotoUID: photoUID, FacesCount: len(faces), Success: true})
}
