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

// PhotoFacesResponse represents the response for getting faces in a photo.
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

// PhotoFace represents a face in a photo with suggestions.
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

// FaceSuggestion represents a suggested person for a face.
type FaceSuggestion struct {
	PersonName string  `json:"person_name"`
	PersonUID  string  `json:"person_uid"`
	Distance   float64 `json:"distance"`
	Confidence float64 `json:"confidence"`
	PhotoCount int     `json:"photo_count"`
}

// buildSubjectMap creates a lookup map from subjects indexed by both Name and UID.
func buildSubjectMap(subjects []photoprism.Subject) map[string]photoprism.Subject {
	subjectMap := make(map[string]photoprism.Subject, len(subjects)*2)
	for i := range subjects {
		subjectMap[subjects[i].Name] = subjects[i]
		subjectMap[subjects[i].UID] = subjects[i]
	}
	return subjectMap
}

// matchFaceToMarker finds the best matching marker for a face's display bbox.
func matchFaceToMarker(displayBBox []float64, markers []photoprism.Marker) (*photoprism.Marker, float64) {
	displayBBoxCorners := []float64{
		displayBBox[0],
		displayBBox[1],
		displayBBox[0] + displayBBox[2],
		displayBBox[1] + displayBBox[3],
	}

	var bestMarker *photoprism.Marker
	bestIoU := 0.0

	for i := range markers {
		if markers[i].Type != constants.MarkerTypeFace {
			continue
		}
		markerBBox := markerToRelativeBBox(markers[i])
		iou := computeIoU(displayBBoxCorners, markerBBox)
		if iou > bestIoU {
			bestIoU = iou
			bestMarker = &markers[i]
		}
	}
	return bestMarker, bestIoU
}

// buildDBFaces converts database faces to PhotoFace responses, matching with markers.
func (h *FacesHandler) buildDBFaces(ctx context.Context, dbFaces []database.StoredFace, markers []photoprism.Marker,
	width, height, orientation int, faceRepo database.FaceReader, pp *photoprism.PhotoPrism,
	threshold float64, limit int, subjectMap map[string]photoprism.Subject,
) ([]PhotoFace, map[string]bool) {
	faces := make([]PhotoFace, 0, len(dbFaces))
	matchedMarkerUIDs := make(map[string]bool)

	// Collect names already assigned on this photo to exclude from suggestions.
	assignedNames := make(map[string]bool)
	for _, m := range markers {
		if m.Name != "" && m.SubjUID != "" {
			assignedNames[m.Name] = true
		}
	}

	for _, dbFace := range dbFaces {
		if len(dbFace.BBox) != 4 {
			continue
		}

		displayBBox := convertPixelBBoxToDisplayRelative(dbFace.BBox, width, height, orientation)
		face := PhotoFace{
			FaceIndex: dbFace.FaceIndex, BBox: dbFace.BBox, BBoxRel: displayBBox,
			DetScore: dbFace.DetScore, Action: ActionCreateMarker, Suggestions: []FaceSuggestion{},
		}

		bestMarker, bestIoU := matchFaceToMarker(displayBBox, markers)
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

		face.Suggestions = h.findFaceSuggestions(
			ctx, faceRepo, pp, dbFace.Embedding,
			threshold, limit, subjectMap, assignedNames,
		)
		faces = append(faces, face)
	}
	return faces, matchedMarkerUIDs
}

// appendUnmatchedMarkers adds markers not matched to any database face.
func appendUnmatchedMarkers(
	faces []PhotoFace, markers []photoprism.Marker, matchedMarkerUIDs map[string]bool,
) []PhotoFace {
	unmatchedIdx := -1
	for i := range markers {
		m := &markers[i]
		if m.Type != constants.MarkerTypeFace || matchedMarkerUIDs[m.UID] {
			continue
		}
		face := PhotoFace{
			FaceIndex: unmatchedIdx, BBoxRel: []float64{m.X, m.Y, m.W, m.H},
			MarkerUID: m.UID, MarkerName: m.Name, Suggestions: []FaceSuggestion{},
		}
		unmatchedIdx--
		if m.Name != "" && m.SubjUID != "" {
			face.Action = ActionAlreadyDone
		} else {
			face.Action = ActionAssignPerson
		}
		faces = append(faces, face)
	}
	return faces
}

// countFaceMarkers counts markers of type "face".
func countFaceMarkers(markers []photoprism.Marker) int {
	count := 0
	for i := range markers {
		if markers[i].Type == constants.MarkerTypeFace {
			count++
		}
	}
	return count
}

// parsePhotoFacesParams extracts threshold and limit from query parameters.
func parsePhotoFacesParams(r *http.Request) (float64, int) {
	threshold := 0.5
	if t, err := strconv.ParseFloat(r.URL.Query().Get("threshold"), 64); err == nil && t > 0 {
		threshold = t
	}
	limit := constants.DefaultFaceSuggestionLimit
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	return threshold, limit
}

// fetchPhotoFacesData fetches faces, details, markers, and subjects for a photo.
// Returns an error message if any required data cannot be fetched.
func fetchPhotoFacesData(
	ctx context.Context, faceRepo database.FaceReader,
	pp *photoprism.PhotoPrism, photoUID string,
) (
	dbFaces []database.StoredFace, fileInfo *primaryFileInfo,
	markers []photoprism.Marker, subjects []photoprism.Subject,
	errMsg string,
) {
	dbFaces, err := faceRepo.GetFaces(ctx, photoUID)
	if err != nil {
		return nil, nil, nil, nil, "failed to get faces from database"
	}

	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, nil, nil, nil, "failed to get photo details"
	}

	fileInfo = extractPrimaryFileInfo(details)
	if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
		return nil, nil, nil, nil, "could not determine photo dimensions"
	}

	markers, _ = pp.GetPhotoMarkers(photoUID)
	subjects, _ = pp.GetSubjects(constants.DefaultSubjectCount, 0)
	return dbFaces, fileInfo, markers, subjects, ""
}

// GetPhotoFaces returns all faces in a photo with suggestions for unassigned ones.
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

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	ctx := r.Context()
	threshold, limit := parsePhotoFacesParams(r)

	dbFaces, fileInfo, markers, subjects, errMsg := fetchPhotoFacesData(ctx, h.faceReader, pp, photoUID)
	if errMsg != "" {
		respondError(w, http.StatusInternalServerError, errMsg)
		return
	}

	subjectMap := buildSubjectMap(subjects)
	faces, matchedMarkerUIDs := h.buildDBFaces(ctx, dbFaces, markers,
		fileInfo.Width, fileInfo.Height, fileInfo.Orientation,
		h.faceReader, pp, threshold, limit, subjectMap)
	faces = appendUnmatchedMarkers(faces, markers, matchedMarkerUIDs)
	facesProcessed, _ := h.faceReader.IsFacesProcessed(ctx, photoUID)

	respondJSON(w, http.StatusOK, PhotoFacesResponse{
		PhotoUID: photoUID, FileUID: fileInfo.UID,
		Width: fileInfo.Width, Height: fileInfo.Height, Orientation: fileInfo.Orientation,
		EmbeddingsCount: len(dbFaces), MarkersCount: countFaceMarkers(markers),
		FacesProcessed: facesProcessed, Faces: faces,
	})
}

// personMatch aggregates face match data for a single person.
type personMatch struct {
	name       string
	uid        string
	totalDist  float64
	count      int
	photoCount int
}

// aggregatePersonMatches groups similar faces by person using cached subject data.
// excludeNames, if non-nil, skips faces whose resolved person name is in the set.
//
//nolint:gocognit // Person match aggregation with subject resolution.
func aggregatePersonMatches(
	similarFaces []database.StoredFace, distances []float64,
	subjectMap map[string]photoprism.Subject, excludeNames map[string]bool,
) map[string]*personMatch {
	personMatches := make(map[string]*personMatch)

	for i, face := range similarFaces {
		personName := face.SubjectName
		personUID := face.SubjectUID

		if personName == "" && personUID != "" {
			if subj, ok := subjectMap[personUID]; ok {
				personName = subj.Name
			}
		}
		if personName == "" {
			continue
		}
		if excludeNames[personName] {
			continue
		}

		if pm, ok := personMatches[personName]; ok {
			pm.totalDist += distances[i]
			pm.count++
		} else {
			photoCount := 0
			if subj, ok := subjectMap[personName]; ok {
				photoCount = subj.PhotoCount
			}
			personMatches[personName] = &personMatch{
				name: personName, uid: personUID,
				totalDist: distances[i], count: 1, photoCount: photoCount,
			}
		}
	}
	return personMatches
}

// personMatchesToSuggestions converts aggregated person matches to sorted suggestions.
func personMatchesToSuggestions(personMatches map[string]*personMatch, limit int) []FaceSuggestion {
	suggestions := make([]FaceSuggestion, 0, len(personMatches))
	for _, pm := range personMatches {
		avgDist := pm.totalDist / float64(pm.count)
		confidence := 1.0 - avgDist
		if confidence < 0 {
			confidence = 0
		}
		suggestions = append(suggestions, FaceSuggestion{
			PersonName: pm.name, PersonUID: pm.uid,
			Distance: avgDist, Confidence: confidence, PhotoCount: pm.photoCount,
		})
	}

	for i := range len(suggestions) - 1 {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Confidence > suggestions[i].Confidence {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}

	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}

// findFaceSuggestions finds people who have similar faces and returns them as suggestions.
// This version uses cached SubjectName/SubjectUID from StoredFace, making zero API calls.
// excludeNames, if non-nil, filters out people already assigned on the same photo.
func (h *FacesHandler) findFaceSuggestions(
	ctx context.Context,
	faceRepo database.FaceReader,
	_ *photoprism.PhotoPrism,
	embedding []float32,
	threshold float64,
	limit int,
	subjectMap map[string]photoprism.Subject,
	excludeNames map[string]bool,
) []FaceSuggestion {
	if len(embedding) == 0 {
		return []FaceSuggestion{}
	}

	similarFaces, distances, err := faceRepo.FindSimilarWithDistance(
		ctx, embedding, constants.DefaultFaceSimilarSearchLimit, threshold,
	)
	if err != nil || len(similarFaces) == 0 {
		return []FaceSuggestion{}
	}

	personMatches := aggregatePersonMatches(similarFaces, distances, subjectMap, excludeNames)
	return personMatchesToSuggestions(personMatches, limit)
}
