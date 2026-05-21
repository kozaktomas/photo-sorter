package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
)

// MatchRequest represents a face match request.
type MatchRequest struct {
	PersonName string  `json:"person_name"`
	Threshold  float64 `json:"threshold"`
	Limit      int     `json:"limit"`
}

// FaceMatchResult represents a single matched photo.
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

// MatchSummary provides counts by action type.
type MatchSummary struct {
	CreateMarker int `json:"create_marker"`
	AssignPerson int `json:"assign_person"`
	AlreadyDone  int `json:"already_done"`
}

// MatchResponse represents the face match response.
type MatchResponse struct {
	Person       string            `json:"person"`
	SourcePhotos int               `json:"source_photos"`
	SourceFaces  int               `json:"source_faces"`
	Matches      []FaceMatchResult `json:"matches"`
	Summary      MatchSummary      `json:"summary"`
}

// matchCandidate holds data about a candidate face match.
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

// matchSourceData holds a source face's embedding and photo info.
type matchSourceData struct {
	PhotoUID  string
	Embedding []float32
}

// buildMatchSourceData extracts unique per-photo source faces from the person's face list.
// First pass: add ALL person face photo UIDs to sourcePhotoSet (regardless of embedding).
// Second pass: extract one embedding per unique photo (skip faces without embeddings).
func buildMatchSourceData(allPersonFaces []database.StoredFace) ([]matchSourceData, map[string]bool) {
	sourcePhotoSet := make(map[string]bool)
	for i := range allPersonFaces {
		sourcePhotoSet[allPersonFaces[i].PhotoUID] = true
	}

	embeddingPhotoSet := make(map[string]bool)
	var sourceFaces []matchSourceData
	for i := range allPersonFaces {
		face := &allPersonFaces[i]
		if len(face.Embedding) == 0 || embeddingPhotoSet[face.PhotoUID] {
			continue
		}
		embeddingPhotoSet[face.PhotoUID] = true
		sourceFaces = append(sourceFaces, matchSourceData{
			PhotoUID:  face.PhotoUID,
			Embedding: face.Embedding,
		})
	}
	return sourceFaces, sourcePhotoSet
}

// computeMinMatchCount scales the minimum match count based on threshold and source count.
func computeMinMatchCount(sourceCount int, threshold float64) int {
	thresholdFactor := threshold / 0.5 * 0.05
	if thresholdFactor < 0.01 {
		thresholdFactor = 0.01
	}
	if thresholdFactor > 0.05 {
		thresholdFactor = 0.05
	}
	minMatchCount := min(max(int(float64(sourceCount)*thresholdFactor), 1), 5)
	return minMatchCount
}

// searchSimilarFaces runs parallel similarity searches and collects candidates into a map.
//
//nolint:gocognit // Multi-embedding face search with candidate deduplication.
func searchSimilarFaces(
	ctx context.Context, faceRepo database.FaceReader,
	sourceEmbeddings [][]float32, sourcePhotoSet map[string]bool,
	searchLimit int, threshold float64, personName string,
) map[string]*matchCandidate {
	normalizedPersonName := facematch.NormalizePersonName(personName)
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
			faces, distances, err := faceRepo.FindSimilarWithDistance(ctx, emb, searchLimit, threshold)
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

	matchMap := make(map[string]*matchCandidate)
	for result := range resultsChan {
		for i := range result.faces {
			face := &result.faces[i]
			if sourcePhotoSet[face.PhotoUID] {
				continue
			}
			// Skip faces assigned to a different person.
			if face.SubjectName != "" && face.SubjectUID != "" {
				if facematch.NormalizePersonName(face.SubjectName) != normalizedPersonName {
					continue
				}
			}
			mergeMatchCandidate(matchMap, face, result.distances[i])
		}
	}
	return matchMap
}

// mergeMatchCandidate adds or updates a candidate in the match map.
func mergeMatchCandidate(matchMap map[string]*matchCandidate, face *database.StoredFace, distance float64) {
	if existing, ok := matchMap[face.PhotoUID]; ok {
		existing.MatchCount++
		if distance < existing.Distance {
			existing.Distance = distance
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
			Distance:    distance,
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

// filterAndSortCandidates filters by min match count, sorts by distance, and applies a limit.
func filterAndSortCandidates(matchMap map[string]*matchCandidate, minMatchCount, limit int) []matchCandidate {
	candidates := make([]matchCandidate, 0, len(matchMap))
	for _, m := range matchMap {
		if m.MatchCount >= minMatchCount {
			candidates = append(candidates, *m)
		}
	}

	for i := range len(candidates) - 1 {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Distance < candidates[i].Distance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

// resolveCandidateDimensions resolves width, height, orientation, and fileUID for a candidate.
// Falls back to the native PhotoReader if cached data is missing. Returns false if resolution fails.
func resolveCandidateDimensions(
	ctx context.Context, c *matchCandidate, photoReader database.PhotoReader,
) (width, height, orientation int, fileUID string, ok bool) {
	width, height, orientation = c.PhotoWidth, c.PhotoHeight, c.Orientation
	fileUID = c.FileUID

	if width != 0 && height != 0 {
		if orientation == 0 {
			orientation = 1
		}
		return width, height, orientation, fileUID, true
	}

	fileInfo, err := fetchPhotoFileInfo(ctx, photoReader, c.PhotoUID)
	if err != nil || fileInfo == nil {
		return 0, 0, 0, "", false
	}
	return fileInfo.Width, fileInfo.Height, fileInfo.Orientation, fileInfo.UID, true
}

// determineMatchAction determines the action for a match based on cached marker data.
func determineMatchAction(c *matchCandidate) (MatchAction, string, string) {
	if c.MarkerUID == "" {
		return ActionCreateMarker, "", ""
	}
	if c.SubjectName != "" && c.SubjectUID != "" {
		return ActionAlreadyDone, c.MarkerUID, c.SubjectName
	}
	return ActionAssignPerson, c.MarkerUID, c.SubjectName
}

// candidateToMatchResult converts a matchCandidate to a FaceMatchResult,
// fetching dimensions from the native PhotoReader if not cached.
// Returns nil if the candidate should be skipped.
func candidateToMatchResult(
	ctx context.Context, c *matchCandidate, photoReader database.PhotoReader,
) *FaceMatchResult {
	width, height, orientation, fileUID, ok := resolveCandidateDimensions(ctx, c, photoReader)
	if !ok || len(c.BBox) != 4 {
		return nil
	}

	faceWidth := c.BBox[2] - c.BBox[0]
	faceWidthRel := faceWidth / float64(width)
	if faceWidth < database.MinFaceWidthPx || faceWidthRel < database.MinFaceWidthRel {
		return nil
	}

	action, markerUID, markerName := determineMatchAction(c)
	bboxRel := convertPixelBBoxToDisplayRelative(c.BBox, width, height, orientation)

	return &FaceMatchResult{
		PhotoUID: c.PhotoUID, Distance: c.Distance, FaceIndex: c.FaceIndex,
		BBox: c.BBox, BBoxRel: bboxRel, FileUID: fileUID,
		Action: action, MarkerUID: markerUID, MarkerName: markerName,
	}
}

// buildMatchResults converts candidates to match results and computes the summary.
func buildMatchResults(
	ctx context.Context, candidates []matchCandidate, photoReader database.PhotoReader,
) ([]FaceMatchResult, MatchSummary) {
	matches := make([]FaceMatchResult, 0, len(candidates))
	var summary MatchSummary

	for ci := range candidates {
		result := candidateToMatchResult(ctx, &candidates[ci], photoReader)
		if result == nil {
			continue
		}
		switch result.Action {
		case ActionCreateMarker:
			summary.CreateMarker++
		case ActionAssignPerson:
			summary.AssignPerson++
		case ActionAlreadyDone:
			summary.AlreadyDone++
		}
		matches = append(matches, *result)
	}
	return matches, summary
}

// markAlreadyAssignedPhotos checks the DB for candidate photos where the person is already.
// assigned, and updates those candidates so determineMatchAction returns AlreadyDone.
// This guards against stale cached SubjectName/SubjectUID on faces rows whose markers
// were reassigned by another path.
func markAlreadyAssignedPhotos(
	ctx context.Context, faceReader database.FaceReader,
	matchMap map[string]*matchCandidate, personName string,
) {
	if len(matchMap) == 0 {
		return
	}

	photoUIDs := make([]string, 0, len(matchMap))
	for uid := range matchMap {
		photoUIDs = append(photoUIDs, uid)
	}

	assigned, err := faceReader.GetPhotoUIDsWithSubjectName(ctx, photoUIDs, personName)
	if err != nil {
		return // best-effort; stale data is the fallback
	}

	for uid := range assigned {
		if c, ok := matchMap[uid]; ok {
			if c.SubjectName == "" {
				c.SubjectName = personName
			}
			if c.SubjectUID == "" {
				c.SubjectUID = "assigned"
			}
			if c.MarkerUID == "" {
				c.MarkerUID = "assigned"
			}
		}
	}
}

// emptyMatchResponse builds an empty MatchResponse for the given person.
func emptyMatchResponse(personName string, sourcePhotos, sourceFaces int) MatchResponse {
	return MatchResponse{
		Person:       personName,
		SourcePhotos: sourcePhotos,
		SourceFaces:  sourceFaces,
		Matches:      []FaceMatchResult{},
		Summary:      MatchSummary{},
	}
}

// parseMatchRequest parses and validates a match request, returning an error message if invalid.
func parseMatchRequest(r *http.Request) (MatchRequest, string) {
	var req MatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, errInvalidRequestBody
	}
	if req.PersonName == "" {
		return req, "person_name is required"
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.5
	}
	return req, ""
}

// extractSourceEmbeddings extracts the embedding vectors from source faces.
func extractSourceEmbeddings(sourceFaces []matchSourceData) [][]float32 {
	embeddings := make([][]float32, len(sourceFaces))
	for i, sf := range sourceFaces {
		embeddings[i] = sf.Embedding
	}
	return embeddings
}

// collectSubjectPhotos paginates through the markers for the given subject
// and folds every (non-invalid) photo UID into target. Errors are logged
// and end the iteration so a partial fill still benefits the caller.
func collectSubjectPhotos(
	ctx context.Context, markerRepo database.MarkerReader,
	subjectUID string, target map[string]bool,
) {
	const pageSize = 500
	for offset := 0; ; offset += pageSize {
		markers, err := markerRepo.ListMarkersForSubject(ctx, subjectUID, pageSize, offset)
		if err != nil {
			log.Printf("[face_match] augment: list markers %s: %v",
				sanitizeForLog(subjectUID), err)
			return
		}
		if len(markers) == 0 {
			return
		}
		for i := range markers {
			if !markers[i].Invalid {
				target[markers[i].PhotoUID] = true
			}
		}
		if len(markers) < pageSize {
			return
		}
	}
}

// augmentSourcePhotoSet adds every photo already tagged with the given
// subject to sourcePhotoSet via the native marker repository. This guards
// against stale local cache where the faces table does not have
// subject_name set, so the photo would otherwise be missed.
func augmentSourcePhotoSet(
	ctx context.Context, subjectRepo database.SubjectReader, markerRepo database.MarkerReader,
	subjectUID string, sourcePhotoSet map[string]bool,
) {
	if subjectUID == "" || subjectRepo == nil || markerRepo == nil {
		return
	}
	subj, err := subjectRepo.GetSubject(ctx, subjectUID)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Printf("[face_match] augment: get subject %s: %v",
				sanitizeForLog(subjectUID), err)
		}
		return
	}
	collectSubjectPhotos(ctx, markerRepo, subjectUID, sourcePhotoSet)
	log.Printf("[face_match] augmented sourcePhotoSet to %d photos for subject %s (%s)",
		len(sourcePhotoSet), sanitizeForLog(subj.Name), sanitizeForLog(subj.Slug))
}

// Match finds photos containing a specific person using face embeddings.
// This version uses cached marker/dimension data from StoredFace, eliminating most API calls.
func (h *FacesHandler) Match(w http.ResponseWriter, r *http.Request) {
	if h.faceReader == nil {
		respondError(w, http.StatusServiceUnavailable, "face data not available")
		return
	}

	req, errMsg := parseMatchRequest(r)
	if errMsg != "" {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}

	ctx := r.Context()
	allPersonFaces, err := h.faceReader.GetFacesBySubjectName(ctx, req.PersonName)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get faces for person")
		return
	}

	if len(allPersonFaces) == 0 {
		respondJSON(w, http.StatusOK, emptyMatchResponse(req.PersonName, 0, 0))
		return
	}

	sourceFaces, sourcePhotoSet := buildMatchSourceData(allPersonFaces)
	augmentSourcePhotoSet(ctx, h.subjectRepo, h.markerRepo, allPersonFaces[0].SubjectUID, sourcePhotoSet)
	sourceEmbeddings := extractSourceEmbeddings(sourceFaces)

	if len(sourceEmbeddings) == 0 {
		respondJSON(w, http.StatusOK, emptyMatchResponse(req.PersonName, len(sourcePhotoSet), 0))
		return
	}

	searchLimit := constants.DefaultSearchLimit
	if req.Limit > 0 && req.Limit < searchLimit {
		searchLimit = req.Limit * 10
	}

	matchMap := searchSimilarFaces(
		ctx, h.faceReader, sourceEmbeddings,
		sourcePhotoSet, searchLimit, req.Threshold, req.PersonName,
	)
	markAlreadyAssignedPhotos(ctx, h.faceReader, matchMap, req.PersonName)
	candidates := filterAndSortCandidates(matchMap, computeMinMatchCount(len(sourceEmbeddings), req.Threshold), req.Limit)
	matches, summary := buildMatchResults(ctx, candidates, h.photoReader)

	respondJSON(w, http.StatusOK, MatchResponse{
		Person: req.PersonName, SourcePhotos: len(sourcePhotoSet),
		SourceFaces: len(sourceEmbeddings), Matches: matches, Summary: summary,
	})
}
