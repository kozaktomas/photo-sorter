package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// PhotoFacesResponse represents the response for getting faces in a photo
type PhotoFacesResponse struct {
	PhotoUID        string      `json:"photo_uid"`
	FileUID         string      `json:"file_uid"`
	Width           int         `json:"width"`
	Height          int         `json:"height"`
	Orientation     int         `json:"orientation"`
	EmbeddingsCount int         `json:"embeddings_count"`
	MarkersCount    int         `json:"markers_count"`
	FacesProcessed  bool        `json:"faces_processed"`
	Faces           []PhotoFace `json:"faces"`
}

// PhotoFace represents a face in a photo with suggestions
type PhotoFace struct {
	FaceIndex   int              `json:"face_index"`
	BBox        []float64        `json:"bbox"`
	BBoxRel     []float64        `json:"bbox_rel"`
	DetScore    float64          `json:"det_score"`
	MarkerUID   string           `json:"marker_uid,omitempty"`
	MarkerName  string           `json:"marker_name,omitempty"`
	Action      MatchAction      `json:"action"`
	Suggestions []FaceSuggestion `json:"suggestions"`
}

// FaceSuggestion represents a suggested person for a face
type FaceSuggestion struct {
	PersonName string  `json:"person_name"`
	PersonUID  string  `json:"person_uid"`
	Distance   float64 `json:"distance"`
	Confidence float64 `json:"confidence"`
	PhotoCount int     `json:"photo_count"`
}

// GetPhotoFaces returns all faces in a photo with suggestions for unassigned ones
func (h *FacesHandler) GetPhotoFaces(w http.ResponseWriter, r *http.Request) {
	if h.faceReader == nil {
		respondError(w, http.StatusServiceUnavailable, "face data not available")
		return
	}

	photoUID := chi.URLParam(r, "uid")
	if photoUID == "" {
		respondError(w, http.StatusBadRequest, "photo_uid is required")
		return
	}

	// Parse query parameters
	threshold := 0.5
	if t, err := strconv.ParseFloat(r.URL.Query().Get("threshold"), 64); err == nil && t > 0 {
		threshold = t
	}
	limit := constants.DefaultFaceSuggestionLimit
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	ctx := r.Context()
	faceRepo := h.faceReader

	// Get faces from database
	dbFaces, err := faceRepo.GetFaces(ctx, photoUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get faces from database")
		return
	}

	// Get photo details from PhotoPrism
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get photo details")
		return
	}

	// Extract file UID, dimensions, and orientation from the PRIMARY file
	// (face detection runs on the primary file via GetPhotoDownload)
	fileInfo := extractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		respondError(w, http.StatusInternalServerError, "could not determine photo dimensions")
		return
	}
	fileUID := fileInfo.UID
	width, height, orientation := fileInfo.Width, fileInfo.Height, fileInfo.Orientation

	// Get markers from PhotoPrism
	markers, _ := pp.GetPhotoMarkers(photoUID)

	// Get all subjects for looking up person info
	subjects, _ := pp.GetSubjects(constants.DefaultSubjectCount, 0)
	subjectMap := make(map[string]photoprism.Subject)
	for _, s := range subjects {
		subjectMap[s.Name] = s
		subjectMap[s.UID] = s
	}

	// Build response faces
	faces := make([]PhotoFace, 0, len(dbFaces))
	matchedMarkerUIDs := make(map[string]bool)

	for _, dbFace := range dbFaces {
		if len(dbFace.BBox) != 4 {
			continue
		}

		// Convert pixel bbox to display-relative coordinates (handles orientation)
		displayBBox := convertPixelBBoxToDisplayRelative(dbFace.BBox, width, height, orientation)

		face := PhotoFace{
			FaceIndex:   dbFace.FaceIndex,
			BBox:        dbFace.BBox,
			BBoxRel:     displayBBox,
			DetScore:    dbFace.DetScore,
			Action:      ActionCreateMarker,
			Suggestions: []FaceSuggestion{},
		}

		// For IoU comparison, convert display [x, y, w, h] back to [x1, y1, x2, y2]
		displayBBoxCorners := []float64{
			displayBBox[0],
			displayBBox[1],
			displayBBox[0] + displayBBox[2],
			displayBBox[1] + displayBBox[3],
		}

		// Try to match with a PhotoPrism marker using IoU
		var bestMarker *photoprism.Marker
		bestIoU := 0.0

		for i := range markers {
			if markers[i].Type != "face" {
				continue
			}
			markerBBox := markerToRelativeBBox(markers[i])
			iou := computeIoU(displayBBoxCorners, markerBBox)
			if iou > bestIoU {
				bestIoU = iou
				bestMarker = &markers[i]
			}
		}

		if bestMarker != nil && bestIoU >= constants.IoUThreshold {
			face.MarkerUID = bestMarker.UID
			face.MarkerName = bestMarker.Name
			matchedMarkerUIDs[bestMarker.UID] = true

			if bestMarker.Name != "" && bestMarker.SubjUID != "" {
				face.Action = ActionAlreadyDone
			} else {
				face.Action = ActionAssignPerson
			}
		}

		// For faces that need assignment, find suggestions
		if face.Action != ActionAlreadyDone {
			suggestions := h.findFaceSuggestions(ctx, faceRepo, pp, dbFace.Embedding, threshold, limit, subjectMap)
			face.Suggestions = suggestions
		}

		faces = append(faces, face)
	}

	// Append unmatched markers (faces detected by PhotoPrism but not in embeddings database)
	unmatchedIdx := -1
	for _, m := range markers {
		if m.Type != "face" || matchedMarkerUIDs[m.UID] {
			continue
		}
		face := PhotoFace{
			FaceIndex:   unmatchedIdx,
			BBoxRel:     []float64{m.X, m.Y, m.W, m.H},
			MarkerUID:   m.UID,
			MarkerName:  m.Name,
			Suggestions: []FaceSuggestion{},
		}
		unmatchedIdx--
		if m.Name != "" && m.SubjUID != "" {
			face.Action = ActionAlreadyDone
		} else {
			face.Action = ActionAssignPerson
		}
		faces = append(faces, face)
	}

	// Count face markers from PhotoPrism
	faceMarkerCount := 0
	for _, m := range markers {
		if m.Type == "face" {
			faceMarkerCount++
		}
	}

	// Check if face detection has been run for this photo
	facesProcessed, _ := faceRepo.IsFacesProcessed(ctx, photoUID)

	respondJSON(w, http.StatusOK, PhotoFacesResponse{
		PhotoUID:        photoUID,
		FileUID:         fileUID,
		Width:           width,
		Height:          height,
		Orientation:     orientation,
		EmbeddingsCount: len(dbFaces),
		MarkersCount:    faceMarkerCount,
		FacesProcessed:  facesProcessed,
		Faces:           faces,
	})
}

// findFaceSuggestions finds people who have similar faces and returns them as suggestions.
// This version uses cached SubjectName/SubjectUID from StoredFace, making zero API calls.
func (h *FacesHandler) findFaceSuggestions(
	ctx context.Context,
	faceRepo database.FaceReader,
	_ *photoprism.PhotoPrism, // kept for interface compatibility, not used
	embedding []float32,
	threshold float64,
	limit int,
	subjectMap map[string]photoprism.Subject,
) []FaceSuggestion {
	// Skip if embedding is nil or empty (photo may not have been processed yet)
	if len(embedding) == 0 {
		return []FaceSuggestion{}
	}

	// Find similar faces
	similarFaces, distances, err := faceRepo.FindSimilarWithDistance(ctx, embedding, constants.DefaultFaceSimilarSearchLimit, threshold)
	if err != nil || len(similarFaces) == 0 {
		return []FaceSuggestion{}
	}

	// Group similar faces by person using cached SubjectName (zero API calls)
	type personMatch struct {
		name       string
		uid        string
		totalDist  float64
		count      int
		photoCount int
	}
	personMatches := make(map[string]*personMatch)

	for i, face := range similarFaces {
		// Use cached subject data from the face record
		personName := face.SubjectName
		personUID := face.SubjectUID

		// Fallback: if SubjectName is empty but SubjectUID exists, look it up
		if personName == "" && personUID != "" {
			if subj, ok := subjectMap[personUID]; ok {
				personName = subj.Name
			}
		}

		if personName == "" {
			continue // No assigned person for this face
		}

		// Aggregate by person
		if pm, ok := personMatches[personName]; ok {
			pm.totalDist += distances[i]
			pm.count++
		} else {
			photoCount := 0
			if subj, ok := subjectMap[personName]; ok {
				photoCount = subj.PhotoCount
			}
			personMatches[personName] = &personMatch{
				name:       personName,
				uid:        personUID,
				totalDist:  distances[i],
				count:      1,
				photoCount: photoCount,
			}
		}
	}

	// Convert to suggestions and sort by confidence (descending)
	suggestions := make([]FaceSuggestion, 0, len(personMatches))
	for _, pm := range personMatches {
		avgDist := pm.totalDist / float64(pm.count)
		confidence := 1.0 - avgDist // Simple confidence from distance
		if confidence < 0 {
			confidence = 0
		}
		suggestions = append(suggestions, FaceSuggestion{
			PersonName: pm.name,
			PersonUID:  pm.uid,
			Distance:   avgDist,
			Confidence: confidence,
			PhotoCount: pm.photoCount,
		})
	}

	// Sort by confidence (descending)
	for i := 0; i < len(suggestions)-1; i++ {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Confidence > suggestions[i].Confidence {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}

	// Limit results
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}

	return suggestions
}
