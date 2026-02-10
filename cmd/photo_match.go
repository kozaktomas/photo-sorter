package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
	"github.com/spf13/cobra"
	"golang.org/x/image/draw"
)

var photoMatchCmd = &cobra.Command{
	Use:   "match <person-name>",
	Short: "Find photos containing a specific person using face embeddings",
	Long: `Find all photos containing a specific person by comparing face embeddings.

This command:
1. Fetches photos tagged with the person from PhotoPrism (q=person:<name>)
2. Retrieves face embeddings for those photos from PostgreSQL
3. Searches for similar faces across all stored embeddings
4. Outputs photos where the same person appears

Use --apply to create markers and assign the person in PhotoPrism.
Use --dry-run with --apply to preview changes without applying them.

Examples:
  # Find all photos containing john-doe
  photo-sorter photo match john-doe

  # Adjust similarity threshold (lower = stricter matching)
  photo-sorter photo match john-doe --threshold 0.4

  # Limit results
  photo-sorter photo match john-doe --limit 100

  # Output as JSON
  photo-sorter photo match john-doe --json

  # Preview what would be applied (dry-run)
  photo-sorter photo match john-doe --apply --dry-run

  # Actually apply changes to PhotoPrism
  photo-sorter photo match john-doe --apply`,
	Args: cobra.ExactArgs(1),
	RunE: runPhotoMatch,
}

func init() {
	photoCmd.AddCommand(photoMatchCmd)

	photoMatchCmd.Flags().Float64("threshold", 0.5, "Maximum cosine distance for face matching (lower = stricter)")
	photoMatchCmd.Flags().Int("limit", 0, "Limit number of results (0 = no limit)")
	photoMatchCmd.Flags().Bool("json", false, "Output as JSON")
	photoMatchCmd.Flags().Bool("apply", false, "Apply changes to PhotoPrism (create markers and assign person)")
	photoMatchCmd.Flags().Bool("dry-run", false, "Preview changes without applying them (use with --apply)")
	photoMatchCmd.Flags().Bool("save-matches", false, "Save matched photos with face boxes to test/ folder")
}

// MatchResult represents a photo that matches the person search
type MatchResult struct {
	PhotoUID   string                `json:"photo_uid"`
	Distance   float64               `json:"distance"`
	FaceIndex  int                   `json:"face_index"`
	BBox       []float64             `json:"bbox"`                  // Our detected bbox [x1, y1, x2, y2] in pixels
	BBoxRel    []float64             `json:"bbox_rel,omitempty"`    // Relative bbox [x, y, w, h] (0-1)
	FileUID    string                `json:"file_uid,omitempty"`    // File UID for creating markers
	Action     facematch.MatchAction `json:"action"`                // What action is needed
	MarkerUID  string                `json:"marker_uid,omitempty"`  // Existing marker UID if found
	MarkerName string                `json:"marker_name,omitempty"` // Existing marker name if assigned
	IoU        float64               `json:"iou,omitempty"`         // IoU with matched marker
	Applied    bool                  `json:"applied,omitempty"`     // Whether the change was applied
	ApplyError string                `json:"apply_error,omitempty"` // Error message if apply failed
}

// MatchOutput represents the JSON output structure
type MatchOutput struct {
	Person       string        `json:"person"`
	SourcePhotos int           `json:"source_photos"`
	SourceFaces  int           `json:"source_faces"`
	Matches      []MatchResult `json:"matches"`
	Summary      MatchSummary  `json:"summary"`
}

// MatchSummary provides counts by action type
type MatchSummary struct {
	CreateMarker int `json:"create_marker"`
	AssignPerson int `json:"assign_person"`
	AlreadyDone  int `json:"already_done"`
}

// resizeImageForMatch resizes an image to fit within maxSize while maintaining aspect ratio
func resizeImageForMatch(img image.Image, maxSize int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Calculate new dimensions
	var newWidth, newHeight int
	if width > height {
		if width <= maxSize {
			return img
		}
		newWidth = maxSize
		newHeight = height * maxSize / width
	} else {
		if height <= maxSize {
			return img
		}
		newHeight = maxSize
		newWidth = width * maxSize / height
	}

	// Create resized image
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	return dst
}

// drawHLine draws a horizontal line on the image.
func drawHLine(dst *image.RGBA, x1, x2, y int, c color.RGBA) {
	bounds := dst.Bounds()
	if y < 0 || y >= bounds.Dy() {
		return
	}
	for x := x1; x <= x2; x++ {
		if x >= 0 && x < bounds.Dx() {
			dst.Set(x, y, c)
		}
	}
}

// drawVLine draws a vertical line on the image.
func drawVLine(dst *image.RGBA, y1, y2, x int, c color.RGBA) {
	bounds := dst.Bounds()
	if x < 0 || x >= bounds.Dx() {
		return
	}
	for y := y1; y <= y2; y++ {
		if y >= 0 && y < bounds.Dy() {
			dst.Set(x, y, c)
		}
	}
}

// drawBoundingBox draws a red rectangle on the image at the given pixel coordinates
func drawBoundingBox(img image.Image, bbox []float64, lineWidth int, padding int) image.Image {
	if len(bbox) != 4 {
		return img
	}

	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)

	x1 := int(bbox[0]) - padding
	y1 := int(bbox[1]) - padding
	x2 := int(bbox[2]) + padding
	y2 := int(bbox[3]) + padding
	red := color.RGBA{255, 0, 0, 255}

	for w := range lineWidth {
		drawHLine(dst, x1, x2, y1+w, red)
		drawHLine(dst, x1, x2, y2-w, red)
		drawVLine(dst, y1, y2, x1+w, red)
		drawVLine(dst, y1, y2, x2-w, red)
	}

	return dst
}

// findPersonMarker finds a marker belonging to the given person in a list of markers.
// It matches by normalized name (exact match or all parts contained).
// Returns nil if no matching marker is found.
func findPersonMarker(markers []photoprism.Marker, personName string) *photoprism.Marker {
	personNameNorm := facematch.NormalizePersonName(personName)

	for i := range markers {
		if markers[i].Type != constants.MarkerTypeFace {
			continue
		}
		markerNameNorm := facematch.NormalizePersonName(markers[i].Name)

		// Exact match after normalization
		if markerNameNorm == personNameNorm {
			return &markers[i]
		}
		// Contains match - all parts of person name must be in marker name
		parts := strings.Split(personNameNorm, " ")
		allMatch := true
		for _, part := range parts {
			if part == "" {
				continue
			}
			if !strings.Contains(markerNameNorm, part) {
				allMatch = false
				break
			}
		}
		if allMatch && len(parts) > 0 {
			return &markers[i]
		}
	}
	return nil
}

// findBestFaceForMarker finds the database face that best matches a marker by IoU.
// Returns the face and IoU score, or nil/-1 if no match found.
func findBestFaceForMarker(faces []database.StoredFace, marker *photoprism.Marker, width, height int) (*database.StoredFace, float64) {
	if marker == nil || width == 0 || height == 0 {
		return nil, -1
	}

	markerBBox := facematch.MarkerToCornerBBox(marker.X, marker.Y, marker.W, marker.H)
	var bestFace *database.StoredFace
	bestIoU := -1.0

	for i := range faces {
		if len(faces[i].BBox) != 4 {
			continue
		}
		faceBBoxRel := facematch.ConvertPixelBBoxToRelative(faces[i].BBox, width, height)
		iou := facematch.ComputeIoU(markerBBox, faceBBoxRel)
		if iou > bestIoU {
			bestIoU = iou
			bestFace = &faces[i]
		}
	}

	return bestFace, bestIoU
}

// findBestMarkerForBBox finds the marker that best matches a bounding box by IoU.
// Returns the marker and IoU score, or nil/0 if no match exceeds the threshold.
func findBestMarkerForBBox(markers []photoprism.Marker, bboxRel []float64) (*photoprism.Marker, float64) {
	if len(bboxRel) != 4 {
		return nil, 0
	}

	// Convert [x,y,w,h] format to corner format [x1,y1,x2,y2] for IoU
	bboxCorners := []float64{
		bboxRel[0],
		bboxRel[1],
		bboxRel[0] + bboxRel[2],
		bboxRel[1] + bboxRel[3],
	}

	var bestMarker *photoprism.Marker
	bestIoU := 0.0

	for i := range markers {
		if markers[i].Type != constants.MarkerTypeFace {
			continue
		}
		markerBBox := facematch.MarkerToCornerBBox(markers[i].X, markers[i].Y, markers[i].W, markers[i].H)
		iou := facematch.ComputeIoU(bboxCorners, markerBBox)
		if iou > bestIoU {
			bestIoU = iou
			bestMarker = &markers[i]
		}
	}

	if bestMarker != nil && bestIoU >= constants.IoUThreshold {
		return bestMarker, bestIoU
	}
	return nil, 0
}

// saveMatchedPhoto downloads a photo, draws bbox, resizes, and saves to test/ folder
func saveMatchedPhoto(pp *photoprism.PhotoPrism, photoUID string, bbox []float64, outputDir string) error {
	// Download photo
	imgData, _, err := pp.GetPhotoDownload(photoUID)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Decode image
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	// Draw bounding box (6px line width, 10px padding)
	img = drawBoundingBox(img, bbox, 6, 10)

	// Resize to max 1080px
	img = resizeImageForMatch(img, 1080)

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

	// Save as JPEG
	outPath := filepath.Join(outputDir, photoUID+".jpg")
	f, err := os.Create(outPath) //nolint:gosec // path is constructed from sanitized photo UID
	if err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	defer f.Close()

	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}

	return nil
}

// matchCmdFlags holds the parsed flags for the photo match command.
type matchCmdFlags struct {
	personName  string
	threshold   float64
	limit       int
	jsonOutput  bool
	apply       bool
	dryRun      bool
	saveMatches bool
}

// matchDeps holds initialized dependencies for the photo match command.
type matchDeps struct {
	pp         *photoprism.PhotoPrism
	faceReader database.FaceReader
	cfg        *config.Config
}

// initMatchDeps initializes PostgreSQL and PhotoPrism dependencies.
func initMatchDeps(ctx context.Context, flags *matchCmdFlags) (*matchDeps, error) {
	cfg := config.Load()

	if cfg.Database.URL == "" {
		return nil, errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return nil, fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	if !flags.jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrismWithCapture(cfg.PhotoPrism.URL, cfg.PhotoPrism.Username, cfg.PhotoPrism.Password, captureDir)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}

	faceReader, err := database.GetFaceReader(ctx)
	if err != nil {
		pp.Logout()
		return nil, fmt.Errorf("failed to get face reader: %w", err)
	}
	if !flags.jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	return &matchDeps{pp: pp, faceReader: faceReader, cfg: cfg}, nil
}

// sourceData holds face embedding data from a source photo.
type sourceData struct {
	PhotoUID  string
	Embedding []float32
	BBox      []float64
}

// extractPhotoDimensions extracts width and height from photo details Files[0].
func extractPhotoDimensions(details map[string]any) (int, int) {
	files, ok := details["Files"].([]any)
	if !ok || len(files) == 0 {
		return 0, 0
	}
	file, ok := files[0].(map[string]any)
	if !ok {
		return 0, 0
	}
	width, _ := file["Width"].(float64)
	height, _ := file["Height"].(float64)
	return int(width), int(height)
}

// extractFileUID extracts the file UID from photo details Files[0].
func extractFileUID(details map[string]any) string {
	files, ok := details["Files"].([]any)
	if !ok || len(files) == 0 {
		return ""
	}
	file, ok := files[0].(map[string]any)
	if !ok {
		return ""
	}
	uid, _ := file["UID"].(string)
	return uid
}

// warnf prints a formatted warning message if not in JSON output mode.
func warnf(jsonOutput bool, format string, args ...any) {
	if !jsonOutput {
		fmt.Printf(format, args...)
	}
}

// collectFaceMarkerNames collects face marker names from a list of markers.
func collectFaceMarkerNames(markers []photoprism.Marker) []string {
	var names []string
	for _, m := range markers {
		if m.Type == constants.MarkerTypeFace {
			names = append(names, fmt.Sprintf("%q", m.Name))
		}
	}
	return names
}

// resolveSourceFaceForPhoto attempts to find the face embedding for a person in a single photo.
// Returns the source data if found, or nil if the photo should be skipped.
// fetchFacesAndPersonMarker fetches face data and finds the person's marker for a photo.
// Returns faces, personMarker, or nil values if not found.
func fetchFacesAndPersonMarker(ctx context.Context, pp *photoprism.PhotoPrism, faceReader database.FaceReader, photoUID string, personName string, jsonOutput bool) ([]database.StoredFace, *photoprism.Marker) {
	faces, err := faceReader.GetFaces(ctx, photoUID)
	if err != nil || len(faces) == 0 {
		if err != nil {
			warnf(jsonOutput, "Warning: could not get faces for photo %s: %v\n", photoUID, err)
		}
		return nil, nil
	}

	markers, err := pp.GetPhotoMarkers(photoUID)
	if err != nil || len(markers) == 0 {
		warnf(jsonOutput, "Warning: no markers for photo %s, skipping\n", photoUID)
		return nil, nil
	}

	personMarker := findPersonMarker(markers, personName)
	if personMarker == nil {
		warnf(jsonOutput, "Warning: no marker for %s in photo %s (available: %s), skipping\n",
			personName, photoUID, strings.Join(collectFaceMarkerNames(markers), ", "))
		return nil, nil
	}

	return faces, personMarker
}

func resolveSourceFaceForPhoto(ctx context.Context, pp *photoprism.PhotoPrism, faceReader database.FaceReader, photo photoprism.Photo, personName string, jsonOutput bool) *sourceData {
	faces, personMarker := fetchFacesAndPersonMarker(ctx, pp, faceReader, photo.UID, personName, jsonOutput)
	if personMarker == nil {
		return nil
	}

	details, err := pp.GetPhotoDetails(photo.UID)
	if err != nil {
		warnf(jsonOutput, "Warning: could not get details for %s, skipping\n", photo.UID)
		return nil
	}

	width, height := extractPhotoDimensions(details)
	if width == 0 || height == 0 {
		warnf(jsonOutput, "Warning: no dimensions for %s, skipping\n", photo.UID)
		return nil
	}

	bestFace, _ := findBestFaceForMarker(faces, personMarker, width, height)
	if bestFace == nil {
		warnf(jsonOutput, "Warning: no matching face for marker in %s\n", photo.UID)
		return nil
	}

	return &sourceData{PhotoUID: photo.UID, Embedding: bestFace.Embedding, BBox: bestFace.BBox}
}

// collectSourceFaces gathers face embeddings for source photos that match the person.
// Returns source face data, actual display name, and any errors (non-fatal).
func collectSourceFaces(ctx context.Context, pp *photoprism.PhotoPrism, faceReader database.FaceReader, sourcePhotos []photoprism.Photo, personName string, jsonOutput bool) ([]sourceData, string) {
	var sourceFaces []sourceData
	var actualPersonName string

	for _, photo := range sourcePhotos {
		if actualPersonName == "" {
			markers, _ := pp.GetPhotoMarkers(photo.UID)
			if marker := findPersonMarker(markers, personName); marker != nil && marker.Name != "" {
				actualPersonName = marker.Name
				warnf(jsonOutput, "DEBUG [%s]: Found person marker %q\n", photo.UID, marker.Name)
			}
		}

		sd := resolveSourceFaceForPhoto(ctx, pp, faceReader, photo, personName, jsonOutput)
		if sd != nil {
			sourceFaces = append(sourceFaces, *sd)
		}
	}

	return sourceFaces, actualPersonName
}

// matchCandidate tracks a candidate match with vote counting.
type matchCandidate struct {
	PhotoUID   string
	Distance   float64
	FaceIndex  int
	BBox       []float64
	MatchCount int
}

// matchSearchResult holds the result of a single similarity search.
type matchSearchResult struct {
	faces     []database.StoredFace
	distances []float64
	err       error
}

// runParallelFaceSearches runs parallel similarity searches and collects results.
func runParallelFaceSearches(ctx context.Context, faceReader database.FaceReader, sourceEmbeddings [][]float32, searchLimit int, threshold float64) chan matchSearchResult {
	resultsChan := make(chan matchSearchResult, len(sourceEmbeddings))
	var wg sync.WaitGroup

	for _, embedding := range sourceEmbeddings {
		if len(embedding) == 0 {
			continue
		}
		wg.Add(1)
		go func(emb []float32) {
			defer wg.Done()
			faces, distances, err := faceReader.FindSimilarWithDistance(ctx, emb, searchLimit, threshold)
			resultsChan <- matchSearchResult{faces: faces, distances: distances, err: err}
		}(embedding)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	return resultsChan
}

// accumulateMatchCandidates processes search results into a match map, excluding source photos.
func accumulateMatchCandidates(resultsChan chan matchSearchResult, sourcePhotoSet map[string]bool) (map[string]*matchCandidate, error) {
	matchMap := make(map[string]*matchCandidate)
	var searchErr error

	for result := range resultsChan {
		if result.err != nil {
			searchErr = result.err
			continue
		}
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
				}
			} else {
				matchMap[face.PhotoUID] = &matchCandidate{
					PhotoUID:   face.PhotoUID,
					Distance:   result.distances[i],
					FaceIndex:  face.FaceIndex,
					BBox:       face.BBox,
					MatchCount: 1,
				}
			}
		}
	}

	if len(matchMap) == 0 && searchErr != nil {
		return nil, fmt.Errorf("failed to search for similar faces: %w", searchErr)
	}
	return matchMap, nil
}

// filterAndSortMatchCandidates filters by min match count, sorts by distance, and applies limit.
func filterAndSortMatchCandidates(matchMap map[string]*matchCandidate, minMatchCount, limit int) []matchCandidate {
	for photoUID, candidate := range matchMap {
		if candidate.MatchCount < minMatchCount {
			delete(matchMap, photoUID)
		}
	}

	candidates := make([]matchCandidate, 0, len(matchMap))
	for _, m := range matchMap {
		candidates = append(candidates, *m)
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

// searchSimilarFaces runs parallel similarity searches and returns filtered candidates.
func searchSimilarFaces(ctx context.Context, faceReader database.FaceReader, sourceEmbeddings [][]float32, sourcePhotoUIDs []string, threshold float64, limit int, minMatchCount int) ([]matchCandidate, error) {
	sourcePhotoSet := make(map[string]bool)
	for _, uid := range sourcePhotoUIDs {
		sourcePhotoSet[uid] = true
	}

	searchLimit := constants.DefaultSearchLimit
	if limit > 0 && limit < searchLimit {
		searchLimit = limit * 10
	}

	resultsChan := runParallelFaceSearches(ctx, faceReader, sourceEmbeddings, searchLimit, threshold)
	matchMap, err := accumulateMatchCandidates(resultsChan, sourcePhotoSet)
	if err != nil {
		return nil, err
	}

	return filterAndSortMatchCandidates(matchMap, minMatchCount, limit), nil
}

// resolveMatchResultFromMarkers sets the action on a match result based on existing markers.
func resolveMatchResultFromMarkers(result *MatchResult, markers []photoprism.Marker) {
	if len(markers) == 0 {
		return
	}
	bestMarker, bestIoU := findBestMarkerForBBox(markers, result.BBoxRel)
	if bestMarker == nil {
		return
	}
	result.MarkerUID = bestMarker.UID
	result.MarkerName = bestMarker.Name
	result.IoU = bestIoU
	if bestMarker.Name != "" && bestMarker.SubjUID != "" {
		result.Action = facematch.ActionAlreadyDone
	} else {
		result.Action = facematch.ActionAssignPerson
	}
}

// isFaceTooSmall checks if a face bounding box is too small to match.
func isFaceTooSmall(bbox []float64, width int) bool {
	if len(bbox) != 4 {
		return true
	}
	faceWidth := bbox[2] - bbox[0]
	faceWidthRel := faceWidth / float64(width)
	return faceWidth < database.MinFaceWidthPx || faceWidthRel < database.MinFaceWidthRel
}

// determineCandidateAction fetches markers for a candidate and determines the action.
func determineCandidateAction(pp *photoprism.PhotoPrism, c matchCandidate, jsonOutput bool) MatchResult {
	result := MatchResult{
		PhotoUID:  c.PhotoUID,
		Distance:  c.Distance,
		FaceIndex: c.FaceIndex,
		BBox:      c.BBox,
		Action:    facematch.ActionCreateMarker,
	}

	details, err := pp.GetPhotoDetails(c.PhotoUID)
	if err != nil {
		warnf(jsonOutput, "Warning: could not get details for %s: %v\n", c.PhotoUID, err)
		return result
	}

	result.FileUID = extractFileUID(details)
	width, height := extractPhotoDimensions(details)
	if width == 0 || height == 0 || len(c.BBox) != 4 {
		return result
	}

	if isFaceTooSmall(c.BBox, width) {
		warnf(jsonOutput, "  Skipping %s: face too small\n", c.PhotoUID)
		result.Action = ""
		return result
	}

	relBBox := facematch.ConvertPixelBBoxToRelative(c.BBox, width, height)
	result.BBoxRel = []float64{relBBox[0], relBBox[1], relBBox[2] - relBBox[0], relBBox[3] - relBBox[1]}

	markers, err := pp.GetPhotoMarkers(c.PhotoUID)
	if err != nil {
		warnf(jsonOutput, "Warning: could not get markers for %s: %v\n", c.PhotoUID, err)
		return result
	}

	resolveMatchResultFromMarkers(&result, markers)
	return result
}

// buildMatchResults builds MatchResult and MatchSummary from candidates.
func buildMatchResults(pp *photoprism.PhotoPrism, candidates []matchCandidate, jsonOutput bool) ([]MatchResult, MatchSummary) {
	matches := make([]MatchResult, 0, len(candidates))
	summary := MatchSummary{}

	for _, c := range candidates {
		result := determineCandidateAction(pp, c, jsonOutput)
		if result.Action == "" {
			continue // Skipped (face too small)
		}

		switch result.Action {
		case facematch.ActionCreateMarker:
			summary.CreateMarker++
		case facematch.ActionAssignPerson:
			summary.AssignPerson++
		case facematch.ActionAlreadyDone:
			summary.AlreadyDone++
		case facematch.ActionUnassignPerson:
			// Not applicable
		}
		matches = append(matches, result)
	}

	return matches, summary
}

// applyMatchCreateMarker creates a new marker for a match.
func applyMatchCreateMarker(pp *photoprism.PhotoPrism, m *MatchResult, nameToApply string, jsonOutput bool) bool {
	if m.FileUID == "" || len(m.BBoxRel) != 4 {
		m.ApplyError = "missing file UID or bounding box"
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "  Error: %s - %s\n", m.PhotoUID, m.ApplyError)
		}
		return false
	}

	marker := photoprism.MarkerCreate{
		FileUID: m.FileUID,
		Type:    constants.MarkerTypeFace,
		X:       m.BBoxRel[0],
		Y:       m.BBoxRel[1],
		W:       m.BBoxRel[2],
		H:       m.BBoxRel[3],
		Name:    nameToApply,
		Src:     "manual",
		SubjSrc: "manual",
	}

	created, err := pp.CreateMarker(marker)
	if err != nil {
		m.ApplyError = err.Error()
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "  Error creating marker for %s: %v\n", m.PhotoUID, err)
		}
		return false
	}

	m.Applied = true
	m.MarkerUID = created.UID
	if !jsonOutput {
		fmt.Printf("  Created marker %s for %s\n", created.UID, m.PhotoUID)
	}
	return true
}

// applyMatchAssignPerson assigns a person to an existing marker.
func applyMatchAssignPerson(pp *photoprism.PhotoPrism, m *MatchResult, nameToApply string, jsonOutput bool) bool {
	if m.MarkerUID == "" {
		m.ApplyError = "missing marker UID"
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "  Error: %s - %s\n", m.PhotoUID, m.ApplyError)
		}
		return false
	}

	update := photoprism.MarkerUpdate{Name: nameToApply, SubjSrc: "manual"}
	_, err := pp.UpdateMarker(m.MarkerUID, update)
	if err != nil {
		m.ApplyError = err.Error()
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "  Error updating marker %s: %v\n", m.MarkerUID, err)
		}
		return false
	}

	m.Applied = true
	if !jsonOutput {
		fmt.Printf("  Assigned %s to marker %s on %s\n", nameToApply, m.MarkerUID, m.PhotoUID)
	}
	return true
}

// printMatchDryRun prints dry-run info for a single match.
func printMatchDryRun(m *MatchResult, nameToApply string, jsonOutput bool) {
	if jsonOutput {
		return
	}
	switch m.Action {
	case facematch.ActionCreateMarker:
		fmt.Printf("  [DRY-RUN] Would create marker for %s and assign %s\n", m.PhotoUID, nameToApply)
	case facematch.ActionAssignPerson:
		fmt.Printf("  [DRY-RUN] Would assign %s to marker %s on %s\n", nameToApply, m.MarkerUID, m.PhotoUID)
	case facematch.ActionAlreadyDone, facematch.ActionUnassignPerson:
		// Not applicable
	}
}

// applyMatchSingle applies a single match action and returns success/failure.
func applyMatchSingle(pp *photoprism.PhotoPrism, m *MatchResult, nameToApply string, jsonOutput bool) bool {
	switch m.Action {
	case facematch.ActionCreateMarker:
		return applyMatchCreateMarker(pp, m, nameToApply, jsonOutput)
	case facematch.ActionAssignPerson:
		return applyMatchAssignPerson(pp, m, nameToApply, jsonOutput)
	default:
		return false
	}
}

// applyMatchChanges applies create/assign changes to PhotoPrism.
func applyMatchChanges(pp *photoprism.PhotoPrism, matches []MatchResult, nameToApply string, dryRun bool, jsonOutput bool, summary MatchSummary) {
	if !jsonOutput {
		if dryRun {
			fmt.Printf("\n[DRY-RUN] Would apply changes to %d photos (as %q):\n", summary.CreateMarker+summary.AssignPerson, nameToApply)
		} else {
			fmt.Printf("\nApplying changes to %d photos (as %q)...\n", summary.CreateMarker+summary.AssignPerson, nameToApply)
		}
	}

	appliedCount := 0
	errorCount := 0

	for i := range matches {
		m := &matches[i]
		if m.Action == facematch.ActionAlreadyDone {
			continue
		}
		if dryRun {
			printMatchDryRun(m, nameToApply, jsonOutput)
			continue
		}
		if applyMatchSingle(pp, m, nameToApply, jsonOutput) {
			appliedCount++
		} else {
			errorCount++
		}
	}

	if !jsonOutput && !dryRun {
		fmt.Printf("\nApplied: %d, Errors: %d\n", appliedCount, errorCount)
	}
}

// saveMatchedPhotos saves matched photos with bounding boxes to disk.
func saveMatchedPhotos(pp *photoprism.PhotoPrism, matches []MatchResult, jsonOutput bool) {
	outputDir := filepath.Join("test", "matches")
	if !jsonOutput {
		fmt.Printf("\nSaving %d matched photos to %s/...\n", len(matches), outputDir)
	}

	savedCount := 0
	for i := range matches {
		if len(matches[i].BBox) != 4 {
			continue
		}
		err := saveMatchedPhoto(pp, matches[i].PhotoUID, matches[i].BBox, outputDir)
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "  Error saving %s: %v\n", matches[i].PhotoUID, err)
			}
		} else {
			savedCount++
			if !jsonOutput {
				fmt.Printf("  Saved %s.jpg\n", matches[i].PhotoUID)
			}
		}
	}

	if !jsonOutput {
		fmt.Printf("Saved %d matched photos\n", savedCount)
	}
}

// printMatchTable prints the human-readable match results table.
func printMatchTable(matches []MatchResult, summary MatchSummary, personName string, cfg *config.Config) {
	if len(matches) == 0 {
		fmt.Printf("No matching photos found for %s\n", personName)
		return
	}

	fmt.Printf("Found %d photos matching %s:\n\n", len(matches), personName)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PHOTO\tDISTANCE\tACTION\tMARKER UID\tMARKER NAME\tIoU")
	fmt.Fprintln(w, "-----\t--------\t------\t----------\t-----------\t---")

	for i := range matches {
		m := &matches[i]
		photoRef := m.PhotoUID
		if url := cfg.PhotoPrism.PhotoURL(m.PhotoUID); url != "" {
			photoRef = url
		}
		markerUID := "-"
		if m.MarkerUID != "" {
			markerUID = m.MarkerUID
		}
		markerName := "-"
		if m.MarkerName != "" {
			markerName = m.MarkerName
		}
		iouStr := "-"
		if m.IoU > 0 {
			iouStr = fmt.Sprintf("%.2f", m.IoU)
		}
		fmt.Fprintf(w, "%s\t%.4f\t%s\t%s\t%s\t%s\n",
			photoRef, m.Distance, m.Action, markerUID, markerName, iouStr)
	}

	w.Flush()

	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Create marker:  %d\n", summary.CreateMarker)
	fmt.Printf("  Assign person:  %d\n", summary.AssignPerson)
	fmt.Printf("  Already done:   %d\n", summary.AlreadyDone)
}

// fetchAllPersonPhotos fetches all photos for a person query from PhotoPrism.
func fetchAllPersonPhotos(pp *photoprism.PhotoPrism, personName string) ([]photoprism.Photo, error) {
	query := "person:" + personName
	var sourcePhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0

	for {
		photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
		if err != nil {
			return nil, fmt.Errorf("failed to get photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		sourcePhotos = append(sourcePhotos, photos...)
		offset += len(photos)
	}
	return sourcePhotos, nil
}

// emptyMatchOutput returns a MatchOutput with no matches for the given state.
func emptyMatchOutput(personName string, sourcePhotos, sourceFaces int) MatchOutput {
	return MatchOutput{
		Person: personName, SourcePhotos: sourcePhotos, SourceFaces: sourceFaces,
		Matches: []MatchResult{}, Summary: MatchSummary{},
	}
}

// extractSourceEmbeddingsAndUIDs extracts embeddings and photo UIDs from source face data.
func extractSourceEmbeddingsAndUIDs(sourceFaces []sourceData) ([][]float32, []string) {
	sourceEmbeddings := make([][]float32, len(sourceFaces))
	sourcePhotoUIDs := make([]string, len(sourceFaces))
	for i, sf := range sourceFaces {
		sourceEmbeddings[i] = sf.Embedding
		sourcePhotoUIDs[i] = sf.PhotoUID
	}
	return sourceEmbeddings, sourcePhotoUIDs
}

// processMatchResults handles applying, saving, and outputting match results.
func processMatchResults(deps *matchDeps, flags *matchCmdFlags, matches []MatchResult, summary MatchSummary, actualPersonName string, sourcePhotos int, sourceEmbeddingCount int) error {
	if flags.apply {
		nameToApply := actualPersonName
		if nameToApply == "" {
			nameToApply = flags.personName
		}
		applyMatchChanges(deps.pp, matches, nameToApply, flags.dryRun, flags.jsonOutput, summary)
	}

	if flags.saveMatches && len(matches) > 0 {
		saveMatchedPhotos(deps.pp, matches, flags.jsonOutput)
	}

	if flags.jsonOutput {
		return outputJSON(MatchOutput{
			Person: flags.personName, SourcePhotos: sourcePhotos, SourceFaces: sourceEmbeddingCount,
			Matches: matches, Summary: summary,
		})
	}

	printMatchTable(matches, summary, flags.personName, deps.cfg)
	return nil
}

func runPhotoMatch(cmd *cobra.Command, args []string) error {
	flags := &matchCmdFlags{
		personName:  args[0],
		threshold:   mustGetFloat64(cmd, "threshold"),
		limit:       mustGetInt(cmd, "limit"),
		jsonOutput:  mustGetBool(cmd, "json"),
		apply:       mustGetBool(cmd, "apply"),
		dryRun:      mustGetBool(cmd, "dry-run"),
		saveMatches: mustGetBool(cmd, "save-matches"),
	}

	ctx := context.Background()
	deps, err := initMatchDeps(ctx, flags)
	if err != nil {
		return err
	}
	defer deps.pp.Logout()

	warnf(!flags.jsonOutput, "Searching for photos with query: person:%s\n", flags.personName)
	sourcePhotos, err := fetchAllPersonPhotos(deps.pp, flags.personName)
	if err != nil {
		return err
	}

	if len(sourcePhotos) == 0 {
		if flags.jsonOutput {
			return outputJSON(emptyMatchOutput(flags.personName, 0, 0))
		}
		fmt.Printf("No photos found for person: %s\n", flags.personName)
		return nil
	}

	warnf(!flags.jsonOutput, "Found %d source photos for %s\n", len(sourcePhotos), flags.personName)
	sourceFaces, actualPersonName := collectSourceFaces(ctx, deps.pp, deps.faceReader, sourcePhotos, flags.personName, flags.jsonOutput)
	sourceEmbeddings, sourcePhotoUIDs := extractSourceEmbeddingsAndUIDs(sourceFaces)

	if len(sourceEmbeddings) == 0 {
		if flags.jsonOutput {
			return outputJSON(emptyMatchOutput(flags.personName, len(sourcePhotos), 0))
		}
		fmt.Printf("No face embeddings found in database for %s's photos. Run 'photo faces' first.\n", flags.personName)
		return nil
	}

	minMatchCount := max((len(sourceEmbeddings)+19)/20, 5)
	warnf(!flags.jsonOutput, "Found %d face embeddings from source photos\nSearching for similar faces (threshold: %.2f, min matches: %d/%d)...\n",
		len(sourceEmbeddings), flags.threshold, minMatchCount, len(sourceEmbeddings))

	candidates, err := searchSimilarFaces(ctx, deps.faceReader, sourceEmbeddings, sourcePhotoUIDs, flags.threshold, flags.limit, minMatchCount)
	if err != nil {
		return err
	}

	warnf(!flags.jsonOutput && len(candidates) > 0, "Fetching marker info for %d matches...\n\n", len(candidates))
	matches, summary := buildMatchResults(deps.pp, candidates, flags.jsonOutput)

	return processMatchResults(deps, flags, matches, summary, actualPersonName, len(sourcePhotos), len(sourceEmbeddings))
}
