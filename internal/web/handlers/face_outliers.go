package handlers

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// OutlierRequest represents a request to find face outliers for a person
type OutlierRequest struct {
	PersonName string  `json:"person_name"`
	Threshold  float64 `json:"threshold"` // min distance from centroid to include (0 = show all)
	Limit      int     `json:"limit"`     // 0 = no limit
}

// OutlierResult represents a single outlier face
type OutlierResult struct {
	PhotoUID         string    `json:"photo_uid"`
	DistFromCentroid float64   `json:"dist_from_centroid"`
	FaceIndex        int       `json:"face_index"`
	BBoxRel          []float64 `json:"bbox_rel,omitempty"`
	FileUID          string    `json:"file_uid,omitempty"`
	MarkerUID        string    `json:"marker_uid,omitempty"`
}

// OutlierResponse represents the response for face outlier detection
type OutlierResponse struct {
	Person            string          `json:"person"`
	TotalFaces        int             `json:"total_faces"`
	AvgDistance       float64         `json:"avg_distance"`
	Outliers          []OutlierResult `json:"outliers"`
	MissingEmbeddings []OutlierResult `json:"missing_embeddings"`
}

// FindOutliers detects wrongly assigned faces by computing centroid distance.
// This version uses cached marker/dimension data from StoredFace, eliminating API calls.
func (h *FacesHandler) FindOutliers(w http.ResponseWriter, r *http.Request) {
	if h.faceReader == nil {
		respondError(w, http.StatusServiceUnavailable, "face data not available")
		return
	}

	var req OutlierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PersonName == "" {
		respondError(w, http.StatusBadRequest, "person_name is required")
		return
	}

	// PhotoPrism client not needed for the optimized path, but keep for compatibility
	_ = middleware.MustGetPhotoPrism(r.Context(), w)

	ctx := r.Context()
	faceRepo := h.faceReader

	// Get all faces for this person directly from database (O(1) query)
	// This eliminates PhotoPrism API calls and N individual face queries
	allPersonFaces, err := faceRepo.GetFacesBySubjectName(ctx, req.PersonName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get faces for person")
		return
	}

	if len(allPersonFaces) == 0 {
		respondJSON(w, http.StatusOK, OutlierResponse{
			Person:      req.PersonName,
			TotalFaces:  0,
			AvgDistance: 0,
			Outliers:    []OutlierResult{},
		})
		return
	}

	// Collect face data from cached faces
	type faceData struct {
		PhotoUID         string
		Embedding        []float32
		FaceIndex        int
		BBoxRel          []float64
		FileUID          string
		MarkerUID        string
		MissingEmbedding bool
	}

	var faces []faceData
	var missingEmbFaces []faceData

	for _, face := range allPersonFaces {
		if len(face.Embedding) == 0 {
			// Face exists but no embedding (shouldn't happen, but handle it)
			missingEmbFaces = append(missingEmbFaces, faceData{
				PhotoUID:         face.PhotoUID,
				FaceIndex:        -1,
				MarkerUID:        face.MarkerUID,
				MissingEmbedding: true,
			})
			continue
		}

		// Use cached dimensions to compute BBoxRel
		width, height, orientation := face.PhotoWidth, face.PhotoHeight, face.Orientation
		fileUID := face.FileUID

		var bboxRel []float64
		if width > 0 && height > 0 && len(face.BBox) == 4 {
			bboxRel = convertPixelBBoxToDisplayRelative(face.BBox, width, height, orientation)
		}

		faces = append(faces, faceData{
			PhotoUID:  face.PhotoUID,
			Embedding: face.Embedding,
			FaceIndex: face.FaceIndex,
			BBoxRel:   bboxRel,
			FileUID:   fileUID,
			MarkerUID: face.MarkerUID,
		})
	}

	// Build missing embeddings list
	missingEmbeddings := make([]OutlierResult, 0, len(missingEmbFaces))
	for _, f := range missingEmbFaces {
		missingEmbeddings = append(missingEmbeddings, OutlierResult{
			PhotoUID:         f.PhotoUID,
			DistFromCentroid: -1,
			FaceIndex:        f.FaceIndex,
			BBoxRel:          f.BBoxRel,
			FileUID:          f.FileUID,
			MarkerUID:        f.MarkerUID,
		})
	}

	if len(faces) == 0 {
		respondJSON(w, http.StatusOK, OutlierResponse{
			Person:            req.PersonName,
			TotalFaces:        0,
			AvgDistance:       0,
			Outliers:          []OutlierResult{},
			MissingEmbeddings: missingEmbeddings,
		})
		return
	}

	// Compute centroid (element-wise mean of all embeddings)
	embDim := len(faces[0].Embedding)
	centroid := make([]float32, embDim)
	for _, f := range faces {
		for i := range centroid {
			if i < len(f.Embedding) {
				centroid[i] += f.Embedding[i]
			}
		}
	}
	for i := range centroid {
		centroid[i] /= float32(len(faces))
	}

	// Compute distance from centroid for each face
	type faceWithDist struct {
		data faceData
		dist float64
	}
	facesWithDist := make([]faceWithDist, len(faces))
	totalDist := 0.0

	for i, f := range faces {
		dist := database.CosineDistance(centroid, f.Embedding)
		facesWithDist[i] = faceWithDist{data: f, dist: dist}
		totalDist += dist
	}

	avgDistance := totalDist / float64(len(faces))

	// Sort by distance descending (most suspicious first)
	sort.Slice(facesWithDist, func(i, j int) bool {
		return facesWithDist[i].dist > facesWithDist[j].dist
	})

	// Apply threshold filter
	var filtered []faceWithDist
	for _, f := range facesWithDist {
		if req.Threshold > 0 && f.dist < req.Threshold {
			continue
		}
		filtered = append(filtered, f)
	}

	// Apply limit
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	// Build response
	outliers := make([]OutlierResult, 0, len(filtered))
	for _, f := range filtered {
		outliers = append(outliers, OutlierResult{
			PhotoUID:         f.data.PhotoUID,
			DistFromCentroid: f.dist,
			FaceIndex:        f.data.FaceIndex,
			BBoxRel:          f.data.BBoxRel,
			FileUID:          f.data.FileUID,
			MarkerUID:        f.data.MarkerUID,
		})
	}

	respondJSON(w, http.StatusOK, OutlierResponse{
		Person:            req.PersonName,
		TotalFaces:        len(faces),
		AvgDistance:       avgDistance,
		Outliers:          outliers,
		MissingEmbeddings: missingEmbeddings,
	})
}
