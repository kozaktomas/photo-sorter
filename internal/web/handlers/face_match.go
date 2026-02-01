package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/web/middleware"
)

// MatchRequest represents a face match request
type MatchRequest struct {
	PersonName string  `json:"person_name"`
	Threshold  float64 `json:"threshold"`
	Limit      int     `json:"limit"`
}

// FaceMatchResult represents a single matched photo
type FaceMatchResult struct {
	PhotoUID   string      `json:"photo_uid"`
	Distance   float64     `json:"distance"`
	FaceIndex  int         `json:"face_index"`
	BBox       []float64   `json:"bbox"`
	BBoxRel    []float64   `json:"bbox_rel,omitempty"`
	FileUID    string      `json:"file_uid,omitempty"`
	Action     MatchAction `json:"action"`
	MarkerUID  string      `json:"marker_uid,omitempty"`
	MarkerName string      `json:"marker_name,omitempty"`
	IoU        float64     `json:"iou,omitempty"`
}

// MatchSummary provides counts by action type
type MatchSummary struct {
	CreateMarker int `json:"create_marker"`
	AssignPerson int `json:"assign_person"`
	AlreadyDone  int `json:"already_done"`
}

// MatchResponse represents the face match response
type MatchResponse struct {
	Person       string            `json:"person"`
	SourcePhotos int               `json:"source_photos"`
	SourceFaces  int               `json:"source_faces"`
	Matches      []FaceMatchResult `json:"matches"`
	Summary      MatchSummary      `json:"summary"`
}

// Match finds photos containing a specific person using face embeddings.
// This version uses cached marker/dimension data from StoredFace, eliminating most API calls.
func (h *FacesHandler) Match(w http.ResponseWriter, r *http.Request) {
	if h.faceReader == nil {
		respondError(w, http.StatusServiceUnavailable, "face data not available")
		return
	}

	var req MatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PersonName == "" {
		respondError(w, http.StatusBadRequest, "person_name is required")
		return
	}

	// Set defaults
	if req.Threshold <= 0 {
		req.Threshold = 0.5
	}
	// req.Limit = 0 means no limit

	pp := middleware.MustGetPhotoPrism(r.Context(), w)
	if pp == nil {
		return
	}

	ctx := r.Context()
	faceRepo := h.faceReader

	// Get all faces for this person directly from database (O(1) query)
	// This eliminates PhotoPrism API calls and N individual face queries
	allPersonFaces, err := faceRepo.GetFacesBySubjectName(ctx, req.PersonName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get faces for person")
		return
	}

	emptySummary := MatchSummary{}
	if len(allPersonFaces) == 0 {
		respondJSON(w, http.StatusOK, MatchResponse{
			Person:       req.PersonName,
			SourcePhotos: 0,
			SourceFaces:  0,
			Matches:      []FaceMatchResult{},
			Summary:      emptySummary,
		})
		return
	}

	// Build source data from cached faces, taking only one face per photo
	type sourceData struct {
		PhotoUID  string
		Embedding []float32
		BBox      []float64
	}
	var sourceFaces []sourceData
	sourcePhotoSet := make(map[string]bool)

	for _, face := range allPersonFaces {
		if len(face.Embedding) == 0 {
			continue
		}
		// Only take one face per photo for this person
		if sourcePhotoSet[face.PhotoUID] {
			continue
		}
		sourcePhotoSet[face.PhotoUID] = true
		sourceFaces = append(sourceFaces, sourceData{
			PhotoUID:  face.PhotoUID,
			Embedding: face.Embedding,
			BBox:      face.BBox,
		})
	}

	// Extract embeddings for search
	sourceEmbeddings := make([][]float32, len(sourceFaces))
	for i, sf := range sourceFaces {
		sourceEmbeddings[i] = sf.Embedding
	}

	if len(sourceEmbeddings) == 0 {
		respondJSON(w, http.StatusOK, MatchResponse{
			Person:       req.PersonName,
			SourcePhotos: len(sourcePhotoSet),
			SourceFaces:  0,
			Matches:      []FaceMatchResult{},
			Summary:      emptySummary,
		})
		return
	}

	// Scale minMatchCount based on threshold
	thresholdFactor := req.Threshold / 0.5 * 0.05
	if thresholdFactor < 0.01 {
		thresholdFactor = 0.01
	}
	if thresholdFactor > 0.05 {
		thresholdFactor = 0.05
	}
	minMatchCount := int(float64(len(sourceEmbeddings)) * thresholdFactor)
	if minMatchCount < 1 {
		minMatchCount = 1
	}
	if minMatchCount > 5 {
		minMatchCount = 5
	}

	// Search for similar faces using each source embedding (parallelized)
	// Extended candidate to include cached data
	type matchCandidate struct {
		PhotoUID    string
		Distance    float64
		FaceIndex   int
		BBox        []float64
		MatchCount  int
		FileUID     string
		PhotoWidth  int
		PhotoHeight int
		Orientation int
		MarkerUID   string
		SubjectName string
		SubjectUID  string
	}

	searchLimit := constants.DefaultSearchLimit
	if req.Limit > 0 && req.Limit < searchLimit {
		searchLimit = req.Limit * 10
	}

	// Run similarity searches in parallel
	type searchResult struct {
		faces     []database.StoredFace
		distances []float64
	}
	resultsChan := make(chan searchResult, len(sourceEmbeddings))

	var wg sync.WaitGroup
	for _, embedding := range sourceEmbeddings {
		if len(embedding) == 0 {
			continue
		}
		wg.Add(1)
		go func(emb []float32) {
			defer wg.Done()
			faces, distances, err := faceRepo.FindSimilarWithDistance(ctx, emb, searchLimit, req.Threshold)
			if err != nil {
				return
			}
			resultsChan <- searchResult{faces: faces, distances: distances}
		}(embedding)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results with cached data
	matchMap := make(map[string]*matchCandidate)
	for result := range resultsChan {
		for i, face := range result.faces {
			if sourcePhotoSet[face.PhotoUID] {
				continue
			}

			if existing, ok := matchMap[face.PhotoUID]; ok {
				existing.MatchCount++
				if result.distances[i] < existing.Distance {
					existing.Distance = result.distances[i]
					existing.FaceIndex = face.FaceIndex
					existing.BBox = face.BBox
					existing.FileUID = face.FileUID
					existing.PhotoWidth = face.PhotoWidth
					existing.PhotoHeight = face.PhotoHeight
					existing.Orientation = face.Orientation
					existing.MarkerUID = face.MarkerUID
					existing.SubjectName = face.SubjectName
					existing.SubjectUID = face.SubjectUID
				}
			} else {
				matchMap[face.PhotoUID] = &matchCandidate{
					PhotoUID:    face.PhotoUID,
					Distance:    result.distances[i],
					FaceIndex:   face.FaceIndex,
					BBox:        face.BBox,
					MatchCount:  1,
					FileUID:     face.FileUID,
					PhotoWidth:  face.PhotoWidth,
					PhotoHeight: face.PhotoHeight,
					Orientation: face.Orientation,
					MarkerUID:   face.MarkerUID,
					SubjectName: face.SubjectName,
					SubjectUID:  face.SubjectUID,
				}
			}
		}
	}

	// Filter by minimum match count
	for photoUID, candidate := range matchMap {
		if candidate.MatchCount < minMatchCount {
			delete(matchMap, photoUID)
		}
	}

	// Convert to slice and sort by distance
	candidates := make([]matchCandidate, 0, len(matchMap))
	for _, m := range matchMap {
		candidates = append(candidates, *m)
	}

	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Distance < candidates[i].Distance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if req.Limit > 0 && len(candidates) > req.Limit {
		candidates = candidates[:req.Limit]
	}

	// Determine action for each match using cached data (zero API calls)
	matches := make([]FaceMatchResult, 0, len(candidates))
	summary := MatchSummary{}

	for _, c := range candidates {
		// Use cached dimensions if available, otherwise need to fetch (fallback)
		width, height, orientation := c.PhotoWidth, c.PhotoHeight, c.Orientation
		fileUID := c.FileUID

		// Fallback: fetch from API if cached data is missing (pre-v3 data)
		if width == 0 || height == 0 {
			details, err := pp.GetPhotoDetails(c.PhotoUID)
			if err != nil {
				continue
			}
			fileInfo := extractPrimaryFileInfo(details)
			if fileInfo == nil || fileInfo.Width == 0 || fileInfo.Height == 0 {
				continue
			}
			width, height, orientation = fileInfo.Width, fileInfo.Height, fileInfo.Orientation
			fileUID = fileInfo.UID
		}

		if len(c.BBox) != 4 {
			continue
		}

		faceWidth := c.BBox[2] - c.BBox[0]
		faceWidthRel := faceWidth / float64(width)

		// Skip faces smaller than minimum size
		if faceWidth < database.MinFaceWidthPx || faceWidthRel < database.MinFaceWidthRel {
			continue
		}

		// Convert pixel bbox to display-relative coordinates
		bboxRel := convertPixelBBoxToDisplayRelative(c.BBox, width, height, orientation)

		result := FaceMatchResult{
			PhotoUID:  c.PhotoUID,
			Distance:  c.Distance,
			FaceIndex: c.FaceIndex,
			BBox:      c.BBox,
			BBoxRel:   bboxRel,
			FileUID:   fileUID,
			Action:    ActionCreateMarker,
		}

		// Use cached marker data if available
		if c.MarkerUID != "" {
			result.MarkerUID = c.MarkerUID
			result.MarkerName = c.SubjectName

			if c.SubjectName != "" && c.SubjectUID != "" {
				result.Action = ActionAlreadyDone
			} else {
				result.Action = ActionAssignPerson
			}
		}

		switch result.Action {
		case ActionCreateMarker:
			summary.CreateMarker++
		case ActionAssignPerson:
			summary.AssignPerson++
		case ActionAlreadyDone:
			summary.AlreadyDone++
		}

		matches = append(matches, result)
	}

	respondJSON(w, http.StatusOK, MatchResponse{
		Person:       req.PersonName,
		SourcePhotos: len(sourcePhotoSet),
		SourceFaces:  len(sourceEmbeddings),
		Matches:      matches,
		Summary:      summary,
	})
}
