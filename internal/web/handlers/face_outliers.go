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

// outlierFaceData holds processed face data for outlier detection
type outlierFaceData struct {
	PhotoUID  string
	Embedding []float32
	FaceIndex int
	BBoxRel   []float64
	FileUID   string
	MarkerUID string
}

// classifyOutlierFaces splits person faces into faces with embeddings and those missing embeddings
func classifyOutlierFaces(allPersonFaces []database.StoredFace) (faces []outlierFaceData, missingEmbeddings []OutlierResult) {
	for i := range allPersonFaces {
		face := &allPersonFaces[i]
		if len(face.Embedding) == 0 {
			missingEmbeddings = append(missingEmbeddings, OutlierResult{
				PhotoUID: face.PhotoUID, DistFromCentroid: -1,
				FaceIndex: -1, MarkerUID: face.MarkerUID,
			})
			continue
		}

		var bboxRel []float64
		if face.PhotoWidth > 0 && face.PhotoHeight > 0 && len(face.BBox) == 4 {
			bboxRel = convertPixelBBoxToDisplayRelative(face.BBox, face.PhotoWidth, face.PhotoHeight, face.Orientation)
		}

		faces = append(faces, outlierFaceData{
			PhotoUID: face.PhotoUID, Embedding: face.Embedding,
			FaceIndex: face.FaceIndex, BBoxRel: bboxRel,
			FileUID: face.FileUID, MarkerUID: face.MarkerUID,
		})
	}
	return faces, missingEmbeddings
}

// computeFaceCentroid computes the element-wise mean of face embeddings
func computeFaceCentroid(faces []outlierFaceData) []float32 {
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
	return centroid
}

// rankFacesByDistance computes distances from centroid and returns sorted outlier results
func rankFacesByDistance(faces []outlierFaceData, centroid []float32, threshold float64, limit int) ([]OutlierResult, float64) {
	type faceWithDist struct {
		data outlierFaceData
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

	sort.Slice(facesWithDist, func(i, j int) bool {
		return facesWithDist[i].dist > facesWithDist[j].dist
	})

	var filtered []faceWithDist
	for _, f := range facesWithDist {
		if threshold > 0 && f.dist < threshold {
			continue
		}
		filtered = append(filtered, f)
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	outliers := make([]OutlierResult, 0, len(filtered))
	for _, f := range filtered {
		outliers = append(outliers, OutlierResult{
			PhotoUID: f.data.PhotoUID, DistFromCentroid: f.dist,
			FaceIndex: f.data.FaceIndex, BBoxRel: f.data.BBoxRel,
			FileUID: f.data.FileUID, MarkerUID: f.data.MarkerUID,
		})
	}
	return outliers, avgDistance
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
		respondError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	if req.PersonName == "" {
		respondError(w, http.StatusBadRequest, "person_name is required")
		return
	}

	_ = middleware.MustGetPhotoPrism(r.Context(), w)

	ctx := r.Context()
	allPersonFaces, err := h.faceReader.GetFacesBySubjectName(ctx, req.PersonName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get faces for person")
		return
	}

	if len(allPersonFaces) == 0 {
		respondJSON(w, http.StatusOK, OutlierResponse{
			Person: req.PersonName, TotalFaces: 0, Outliers: []OutlierResult{},
		})
		return
	}

	faces, missingEmbeddings := classifyOutlierFaces(allPersonFaces)

	if len(faces) == 0 {
		respondJSON(w, http.StatusOK, OutlierResponse{
			Person: req.PersonName, TotalFaces: 0,
			Outliers: []OutlierResult{}, MissingEmbeddings: missingEmbeddings,
		})
		return
	}

	centroid := computeFaceCentroid(faces)
	outliers, avgDistance := rankFacesByDistance(faces, centroid, req.Threshold, req.Limit)

	respondJSON(w, http.StatusOK, OutlierResponse{
		Person:            req.PersonName,
		TotalFaces:        len(faces),
		AvgDistance:        avgDistance,
		Outliers:          outliers,
		MissingEmbeddings: missingEmbeddings,
	})
}
