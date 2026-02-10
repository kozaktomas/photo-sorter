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

	"github.com/spf13/cobra"
	"github.com/kozaktomas/photo-sorter/internal/config"
	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/database/postgres"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
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
	PhotoUID   string               `json:"photo_uid"`
	Distance   float64              `json:"distance"`
	FaceIndex  int                  `json:"face_index"`
	BBox       []float64            `json:"bbox"`                  // Our detected bbox [x1, y1, x2, y2] in pixels
	BBoxRel    []float64            `json:"bbox_rel,omitempty"`    // Relative bbox [x, y, w, h] (0-1)
	FileUID    string               `json:"file_uid,omitempty"`    // File UID for creating markers
	Action     facematch.MatchAction `json:"action"`                // What action is needed
	MarkerUID  string               `json:"marker_uid,omitempty"`  // Existing marker UID if found
	MarkerName string               `json:"marker_name,omitempty"` // Existing marker name if assigned
	IoU        float64              `json:"iou,omitempty"`         // IoU with matched marker
	Applied    bool                 `json:"applied,omitempty"`     // Whether the change was applied
	ApplyError string               `json:"apply_error,omitempty"` // Error message if apply failed
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

// drawBoundingBox draws a red rectangle on the image at the given pixel coordinates
func drawBoundingBox(img image.Image, bbox []float64, lineWidth int, padding int) image.Image {
	if len(bbox) != 4 {
		return img
	}

	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)

	// Copy original image
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)

	// Bounding box coordinates (pixels) with padding
	x1 := int(bbox[0]) - padding
	y1 := int(bbox[1]) - padding
	x2 := int(bbox[2]) + padding
	y2 := int(bbox[3]) + padding

	red := color.RGBA{255, 0, 0, 255}

	// Draw rectangle lines
	for w := range lineWidth {
		// Top line
		for x := x1; x <= x2; x++ {
			if y1+w >= 0 && y1+w < bounds.Dy() && x >= 0 && x < bounds.Dx() {
				dst.Set(x, y1+w, red)
			}
		}
		// Bottom line
		for x := x1; x <= x2; x++ {
			if y2-w >= 0 && y2-w < bounds.Dy() && x >= 0 && x < bounds.Dx() {
				dst.Set(x, y2-w, red)
			}
		}
		// Left line
		for y := y1; y <= y2; y++ {
			if x1+w >= 0 && x1+w < bounds.Dx() && y >= 0 && y < bounds.Dy() {
				dst.Set(x1+w, y, red)
			}
		}
		// Right line
		for y := y1; y <= y2; y++ {
			if x2-w >= 0 && x2-w < bounds.Dx() && y >= 0 && y < bounds.Dy() {
				dst.Set(x2-w, y, red)
			}
		}
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

func runPhotoMatch(cmd *cobra.Command, args []string) error {
	personName := args[0]
	threshold := mustGetFloat64(cmd, "threshold")
	limit := mustGetInt(cmd, "limit")
	jsonOutput := mustGetBool(cmd, "json")
	apply := mustGetBool(cmd, "apply")
	dryRun := mustGetBool(cmd, "dry-run")
	saveMatches := mustGetBool(cmd, "save-matches")

	ctx := context.Background()
	cfg := config.Load()

	// Initialize PostgreSQL database
	if cfg.Database.URL == "" {
		return errors.New("DATABASE_URL environment variable is required")
	}
	if err := postgres.Initialize(&cfg.Database); err != nil {
		return fmt.Errorf("failed to initialize PostgreSQL: %w", err)
	}

	// Create singleton repositories and register with database package
	pool := postgres.GetGlobalPool()
	faceRepo := postgres.NewFaceRepository(pool)
	embeddingRepo := postgres.NewEmbeddingRepository(pool)
	database.RegisterPostgresBackend(
		func() database.EmbeddingReader { return embeddingRepo },
		func() database.FaceReader { return faceRepo },
		func() database.FaceWriter { return faceRepo },
	)

	// Connect to PhotoPrism
	if !jsonOutput {
		fmt.Println("Connecting to PhotoPrism...")
	}
	pp, err := photoprism.NewPhotoPrismWithCapture(
		cfg.PhotoPrism.URL,
		cfg.PhotoPrism.Username,
		cfg.PhotoPrism.Password,
		captureDir,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PhotoPrism: %w", err)
	}
	defer pp.Logout()

	// Get face reader from PostgreSQL
	faceReader, err := database.GetFaceReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to get face reader: %w", err)
	}
	if !jsonOutput {
		fmt.Println("Using PostgreSQL data source")
	}

	// Fetch photos for the person from PhotoPrism
	query := "person:" + personName
	if !jsonOutput {
		fmt.Printf("Searching for photos with query: %s\n", query)
	}

	var sourcePhotos []photoprism.Photo
	pageSize := constants.DefaultPageSize
	offset := 0

	for {
		photos, err := pp.GetPhotosWithQuery(pageSize, offset, query)
		if err != nil {
			return fmt.Errorf("failed to get photos: %w", err)
		}
		if len(photos) == 0 {
			break
		}
		sourcePhotos = append(sourcePhotos, photos...)
		offset += len(photos)
	}

	emptySummary := MatchSummary{}
	if len(sourcePhotos) == 0 {
		if jsonOutput {
			return outputJSON(MatchOutput{
				Person:       personName,
				SourcePhotos: 0,
				SourceFaces:  0,
				Matches:      []MatchResult{},
				Summary:      emptySummary,
			})
		}
		fmt.Printf("No photos found for person: %s\n", personName)
		return nil
	}

	if !jsonOutput {
		fmt.Printf("Found %d source photos for %s\n", len(sourcePhotos), personName)
	}

	// Get face embeddings for source photos from datafile
	// Only use the face that matches the person's marker (by bounding box IoU)
	type sourceData struct {
		PhotoUID  string
		Embedding []float32
		BBox      []float64 // pixel coordinates for saving
	}
	var sourceFaces []sourceData
	var actualPersonName string // The real display name from PhotoPrism (e.g., "Jan Novák")

	for _, photo := range sourcePhotos {
		// Get faces from our database
		faces, err := faceReader.GetFaces(ctx, photo.UID)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("Warning: could not get faces for photo %s: %v\n", photo.UID, err)
			}
			continue
		}

		if len(faces) == 0 {
			continue
		}

		// Always use marker matching to find the correct person's face
		markers, err := pp.GetPhotoMarkers(photo.UID)
		if err != nil || len(markers) == 0 {
			if !jsonOutput {
				fmt.Printf("Warning: no markers for photo %s, skipping\n", photo.UID)
			}
			continue
		}

		// Find the marker that belongs to our person
		personMarker := findPersonMarker(markers, personName)

		if personMarker == nil {
			if !jsonOutput {
				// List available marker names for debugging
				var names []string
				for _, m := range markers {
					if m.Type == constants.MarkerTypeFace {
						names = append(names, fmt.Sprintf("%q", m.Name))
					}
				}
				fmt.Printf("Warning: no marker for %s in photo %s (available: %s), skipping\n",
					personName, photo.UID, strings.Join(names, ", "))
			}
			continue
		}

		// Capture the actual display name from the first valid marker
		if actualPersonName == "" && personMarker.Name != "" {
			actualPersonName = personMarker.Name
			if !jsonOutput {
				fmt.Printf("DEBUG [%s]: Found person marker %q\n", photo.UID, personMarker.Name)
			}
		}

		// Get photo dimensions for bbox conversion
		details, err := pp.GetPhotoDetails(photo.UID)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("Warning: could not get details for %s, skipping\n", photo.UID)
			}
			continue
		}

		width, height := 0, 0
		// Dimensions are inside Files[0], not at top level
		if files, ok := details["Files"].([]interface{}); ok && len(files) > 0 {
			if file, ok := files[0].(map[string]interface{}); ok {
				if w, ok := file["Width"].(float64); ok {
					width = int(w)
				}
				if h, ok := file["Height"].(float64); ok {
					height = int(h)
				}
			}
		}

		if width == 0 || height == 0 {
			if !jsonOutput {
				fmt.Printf("Warning: no dimensions for %s, skipping\n", photo.UID)
			}
			continue
		}

		// Find the face in our database that best matches this person's marker
		bestFace, _ := findBestFaceForMarker(faces, personMarker, width, height)

		if bestFace != nil {
			sourceFaces = append(sourceFaces, sourceData{
				PhotoUID:  photo.UID,
				Embedding: bestFace.Embedding,
				BBox:      bestFace.BBox,
			})
		} else if !jsonOutput {
			fmt.Printf("Warning: no matching face for marker in %s\n", photo.UID)
		}
	}

	// Extract embeddings and UIDs for search
	sourceEmbeddings := make([][]float32, len(sourceFaces))
	sourcePhotoUIDs := make([]string, len(sourceFaces))
	for i, sf := range sourceFaces {
		sourceEmbeddings[i] = sf.Embedding
		sourcePhotoUIDs[i] = sf.PhotoUID
	}

	if len(sourceEmbeddings) == 0 {
		if jsonOutput {
			return outputJSON(MatchOutput{
				Person:       personName,
				SourcePhotos: len(sourcePhotos),
				SourceFaces:  0,
				Matches:      []MatchResult{},
				Summary:      emptySummary,
			})
		}
		fmt.Printf("No face embeddings found in database for %s's photos. Run 'photo faces' first.\n", personName)
		return nil
	}

	minMatchCount := (len(sourceEmbeddings) + 19) / 20 // At least 5% (rounded up)
	if minMatchCount < 5 {
		minMatchCount = 5
	}

	if !jsonOutput {
		fmt.Printf("Found %d face embeddings from source photos\n", len(sourceEmbeddings))
		fmt.Printf("Searching for similar faces (threshold: %.2f, min matches: %d/%d)...\n",
			threshold, minMatchCount, len(sourceEmbeddings))
	}

	// Search for similar faces using each source embedding (parallelized)
	// Track how many source embeddings match each candidate
	type matchCandidate struct {
		PhotoUID   string
		Distance   float64 // Best (lowest) distance
		FaceIndex  int
		BBox       []float64
		MatchCount int // Number of source embeddings that matched
	}

	sourcePhotoSet := make(map[string]bool)
	for _, uid := range sourcePhotoUIDs {
		sourcePhotoSet[uid] = true
	}

	searchLimit := constants.DefaultSearchLimit // Search limit per embedding
	if limit > 0 && limit < searchLimit {
		searchLimit = limit * 10 // Get more results to allow for dedup
	}

	// Run similarity searches in parallel
	type searchResult struct {
		faces     []database.StoredFace
		distances []float64
		err       error
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
			faces, distances, err := faceReader.FindSimilarWithDistance(ctx, emb, searchLimit, threshold)
			resultsChan <- searchResult{faces: faces, distances: distances, err: err}
		}(embedding)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	matchMap := make(map[string]*matchCandidate)
	var searchErr error
	for result := range resultsChan {
		if result.err != nil {
			searchErr = result.err
			continue
		}

		for i, face := range result.faces {
			// Skip source photos
			if sourcePhotoSet[face.PhotoUID] {
				continue
			}

			// Track match count and keep best distance
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

	// Report first error if all searches failed
	if len(matchMap) == 0 && searchErr != nil {
		return fmt.Errorf("failed to search for similar faces: %w", searchErr)
	}

	// Filter: only keep candidates that matched at least half of source embeddings
	for photoUID, candidate := range matchMap {
		if candidate.MatchCount < minMatchCount {
			delete(matchMap, photoUID)
		}
	}

	// Convert map to slice and sort by distance
	candidates := make([]matchCandidate, 0, len(matchMap))
	for _, m := range matchMap {
		candidates = append(candidates, *m)
	}

	// Sort by distance (ascending)
	for i := range len(candidates) - 1 {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Distance < candidates[i].Distance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	if !jsonOutput && len(candidates) > 0 {
		fmt.Printf("Fetching marker info for %d matches...\n\n", len(candidates))
	}

	// For each candidate, fetch markers from PhotoPrism and determine action
	matches := make([]MatchResult, 0, len(candidates))
	summary := MatchSummary{}

	for _, c := range candidates {
		result := MatchResult{
			PhotoUID:  c.PhotoUID,
			Distance:  c.Distance,
			FaceIndex: c.FaceIndex,
			BBox:      c.BBox,
			Action:    facematch.ActionCreateMarker, // Default: no marker found
		}

		// Get photo details for dimensions and file UID
		details, err := pp.GetPhotoDetails(c.PhotoUID)
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Warning: could not get details for %s: %v\n", c.PhotoUID, err)
			}
		} else {
			width, height := 0, 0

			// Extract file UID and dimensions from Files[0]
			if files, ok := details["Files"].([]interface{}); ok && len(files) > 0 {
				if file, ok := files[0].(map[string]interface{}); ok {
					if uid, ok := file["UID"].(string); ok {
						result.FileUID = uid
					}
					if w, ok := file["Width"].(float64); ok {
						width = int(w)
					}
					if h, ok := file["Height"].(float64); ok {
						height = int(h)
					}
				}
			}

			// Convert our pixel bbox to relative coordinates for marker creation
			if width > 0 && height > 0 && len(c.BBox) == 4 {
				faceWidth := c.BBox[2] - c.BBox[0]
				faceWidthRel := (c.BBox[2] - c.BBox[0]) / float64(width)

				// Skip faces smaller than minimum size
				if faceWidth < database.MinFaceWidthPx || faceWidthRel < database.MinFaceWidthRel {
					if !jsonOutput {
						fmt.Printf("  Skipping %s: face too small (%.0fpx, %.1f%%)\n", c.PhotoUID, faceWidth, faceWidthRel*100)
					}
					continue
				}

				relBBox := facematch.ConvertPixelBBoxToRelative(c.BBox, width, height)
				// Store as [x, y, w, h] format for marker creation
				result.BBoxRel = []float64{
					relBBox[0],
					relBBox[1],
					relBBox[2] - relBBox[0], // width
					relBBox[3] - relBBox[1], // height
				}

				// Fetch markers from PhotoPrism and find best match
				markers, err := pp.GetPhotoMarkers(c.PhotoUID)
				if err != nil {
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "Warning: could not get markers for %s: %v\n", c.PhotoUID, err)
					}
				} else if len(markers) > 0 {
					bestMarker, bestIoU := findBestMarkerForBBox(markers, result.BBoxRel)
					if bestMarker != nil {
						result.MarkerUID = bestMarker.UID
						result.MarkerName = bestMarker.Name
						result.IoU = bestIoU

						if bestMarker.Name != "" && bestMarker.SubjUID != "" {
							// Marker exists and has a person assigned
							result.Action = facematch.ActionAlreadyDone
						} else {
							// Marker exists but no person assigned
							result.Action = facematch.ActionAssignPerson
						}
					}
				}
			}
		}

		// Update summary
		switch result.Action {
		case facematch.ActionCreateMarker:
			summary.CreateMarker++
		case facematch.ActionAssignPerson:
			summary.AssignPerson++
		case facematch.ActionAlreadyDone:
			summary.AlreadyDone++
		case facematch.ActionUnassignPerson:
			// Not applicable in match context
		}

		matches = append(matches, result)
	}

	// Apply changes if requested
	if apply {
		// Use the actual display name from PhotoPrism, fall back to CLI arg
		nameToApply := actualPersonName
		if nameToApply == "" {
			nameToApply = personName
		}

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

			// Skip already done
			if m.Action == facematch.ActionAlreadyDone {
				continue
			}

			if dryRun {
				// Just mark as would be applied
				if !jsonOutput {
					switch m.Action {
					case facematch.ActionCreateMarker:
						fmt.Printf("  [DRY-RUN] Would create marker for %s and assign %s\n", m.PhotoUID, nameToApply)
					case facematch.ActionAssignPerson:
						fmt.Printf("  [DRY-RUN] Would assign %s to marker %s on %s\n", nameToApply, m.MarkerUID, m.PhotoUID)
					case facematch.ActionAlreadyDone, facematch.ActionUnassignPerson:
						// Not applicable
					}
				}
				continue
			}

			// Actually apply
			switch m.Action {
			case facematch.ActionCreateMarker:
				if m.FileUID == "" || len(m.BBoxRel) != 4 {
					m.ApplyError = "missing file UID or bounding box"
					errorCount++
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "  Error: %s - %s\n", m.PhotoUID, m.ApplyError)
					}
					continue
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
					errorCount++
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "  Error creating marker for %s: %v\n", m.PhotoUID, err)
					}
				} else {
					m.Applied = true
					m.MarkerUID = created.UID
					appliedCount++
					if !jsonOutput {
						fmt.Printf("  Created marker %s for %s\n", created.UID, m.PhotoUID)
					}
				}

			case facematch.ActionAssignPerson:
				if m.MarkerUID == "" {
					m.ApplyError = "missing marker UID"
					errorCount++
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "  Error: %s - %s\n", m.PhotoUID, m.ApplyError)
					}
					continue
				}

				update := photoprism.MarkerUpdate{
					Name:    nameToApply,
					SubjSrc: "manual",
				}

				_, err := pp.UpdateMarker(m.MarkerUID, update)
				if err != nil {
					m.ApplyError = err.Error()
					errorCount++
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "  Error updating marker %s: %v\n", m.MarkerUID, err)
					}
				} else {
					m.Applied = true
					appliedCount++
					if !jsonOutput {
						fmt.Printf("  Assigned %s to marker %s on %s\n", nameToApply, m.MarkerUID, m.PhotoUID)
					}
				}

			case facematch.ActionAlreadyDone, facematch.ActionUnassignPerson:
				// Skipped above
			}
		}

		if !jsonOutput && !dryRun {
			fmt.Printf("\nApplied: %d, Errors: %d\n", appliedCount, errorCount)
		}
	}

	// Save matched photos with bounding boxes
	if saveMatches && len(matches) > 0 {
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

	if jsonOutput {
		return outputJSON(MatchOutput{
			Person:       personName,
			SourcePhotos: len(sourcePhotos),
			SourceFaces:  len(sourceEmbeddings),
			Matches:      matches,
			Summary:      summary,
		})
	}

	// Human-readable output
	if len(matches) == 0 {
		fmt.Printf("No matching photos found for %s\n", personName)
		return nil
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

	// Print summary
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Create marker:  %d\n", summary.CreateMarker)
	fmt.Printf("  Assign person:  %d\n", summary.AssignPerson)
	fmt.Printf("  Already done:   %d\n", summary.AlreadyDone)

	return nil
}
