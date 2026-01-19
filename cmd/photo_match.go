package cmd

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/tomas/photo-sorter/internal/config"
	"github.com/tomas/photo-sorter/internal/database"
	"github.com/tomas/photo-sorter/internal/photoprism"
	"golang.org/x/image/draw"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var photoMatchCmd = &cobra.Command{
	Use:   "match <person-name>",
	Short: "Find photos containing a specific person using face embeddings",
	Long: `Find all photos containing a specific person by comparing face embeddings.

This command:
1. Fetches photos tagged with the person from PhotoPrism (q=person:<name>)
2. Retrieves face embeddings for those photos from PostgreSQL
3. Searches for similar faces across all photos in the database
4. Outputs photos where the same person appears

Use --apply to create markers and assign the person in PhotoPrism.
Use --dry-run with --apply to preview changes without applying them.

Examples:
  # Find all photos containing tomas-kozak
  photo-sorter photo match tomas-kozak

  # Adjust similarity threshold (lower = stricter matching)
  photo-sorter photo match tomas-kozak --threshold 0.4

  # Limit results
  photo-sorter photo match tomas-kozak --limit 100

  # Output as JSON
  photo-sorter photo match tomas-kozak --json

  # Preview what would be applied (dry-run)
  photo-sorter photo match tomas-kozak --apply --dry-run

  # Actually apply changes to PhotoPrism
  photo-sorter photo match tomas-kozak --apply`,
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

// MatchAction represents what action is needed for a matched face
type MatchAction string

const (
	ActionCreateMarker MatchAction = "create_marker" // No marker exists, need to create one
	ActionAssignPerson MatchAction = "assign_person" // Marker exists but no person assigned
	ActionAlreadyDone  MatchAction = "already_done"  // Marker exists with person already assigned
)

// MatchResult represents a photo that matches the person search
type MatchResult struct {
	PhotoUID   string      `json:"photo_uid"`
	Distance   float64     `json:"distance"`
	FaceIndex  int         `json:"face_index"`
	BBox       []float64   `json:"bbox"`                  // Our detected bbox [x1, y1, x2, y2] in pixels
	BBoxRel    []float64   `json:"bbox_rel,omitempty"`    // Relative bbox [x, y, w, h] (0-1)
	FileUID    string      `json:"file_uid,omitempty"`    // File UID for creating markers
	Action     MatchAction `json:"action"`                // What action is needed
	MarkerUID  string      `json:"marker_uid,omitempty"`  // Existing marker UID if found
	MarkerName string      `json:"marker_name,omitempty"` // Existing marker name if assigned
	IoU        float64     `json:"iou,omitempty"`         // IoU with matched marker
	Applied    bool        `json:"applied,omitempty"`     // Whether the change was applied
	ApplyError string      `json:"apply_error,omitempty"` // Error message if apply failed
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

// computeIoU calculates Intersection over Union between two bounding boxes
// bbox1 and bbox2 are [x1, y1, x2, y2] in the same coordinate system
func computeIoU(bbox1, bbox2 []float64) float64 {
	if len(bbox1) != 4 || len(bbox2) != 4 {
		return 0
	}

	// Calculate intersection
	x1 := max(bbox1[0], bbox2[0])
	y1 := max(bbox1[1], bbox2[1])
	x2 := min(bbox1[2], bbox2[2])
	y2 := min(bbox1[3], bbox2[3])

	if x2 <= x1 || y2 <= y1 {
		return 0 // No intersection
	}

	intersection := (x2 - x1) * (y2 - y1)

	// Calculate union
	area1 := (bbox1[2] - bbox1[0]) * (bbox1[3] - bbox1[1])
	area2 := (bbox2[2] - bbox2[0]) * (bbox2[3] - bbox2[1])
	union := area1 + area2 - intersection

	if union <= 0 {
		return 0
	}

	return intersection / union
}

// convertPixelBBoxToRelative converts pixel bbox to relative (0-1) coordinates
func convertPixelBBoxToRelative(bbox []float64, width, height int) []float64 {
	if len(bbox) != 4 || width <= 0 || height <= 0 {
		return bbox
	}
	return []float64{
		bbox[0] / float64(width),
		bbox[1] / float64(height),
		bbox[2] / float64(width),
		bbox[3] / float64(height),
	}
}

// markerToRelativeBBox converts PhotoPrism marker (X, Y, W, H) to [x1, y1, x2, y2]
func markerToRelativeBBox(m photoprism.Marker) []float64 {
	return []float64{
		m.X,
		m.Y,
		m.X + m.W,
		m.Y + m.H,
	}
}

// removeDiacritics removes diacritical marks from a string (e.g., "Tomáš" -> "Tomas")
func removeDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, s)
	return result
}

// normalizePersonName normalizes a name for comparison (lowercase, no diacritics, spaces for dashes)
func normalizePersonName(name string) string {
	name = removeDiacritics(name)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", " ")
	return name
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
	for w := 0; w < lineWidth; w++ {
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
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

	// Save as JPEG
	outPath := filepath.Join(outputDir, photoUID+".jpg")
	f, err := os.Create(outPath)
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
	threshold, _ := cmd.Flags().GetFloat64("threshold")
	limit, _ := cmd.Flags().GetInt("limit")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	apply, _ := cmd.Flags().GetBool("apply")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	saveMatches, _ := cmd.Flags().GetBool("save-matches")

	ctx := context.Background()
	cfg := config.Load()

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

	// Connect to PostgreSQL
	if !jsonOutput {
		fmt.Println("Connecting to PostgreSQL...")
	}
	pool, err := database.Connect(ctx, &cfg.Postgres)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	// Run migrations
	if err := database.Migrate(ctx, pool, cfg.Embedding.Dim); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	faceRepo := database.NewFaceRepository(pool)

	// Fetch photos for the person from PhotoPrism
	query := fmt.Sprintf("person:%s", personName)
	if !jsonOutput {
		fmt.Printf("Searching for photos with query: %s\n", query)
	}

	var sourcePhotos []photoprism.Photo
	pageSize := 1000
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

	// Get face embeddings for source photos from PostgreSQL
	// Only use the face that matches the person's marker (by bounding box IoU)
	type sourceData struct {
		PhotoUID  string
		Embedding []float32
		BBox      []float64 // pixel coordinates for saving
	}
	var sourceFaces []sourceData
	var actualPersonName string // The real display name from PhotoPrism (e.g., "Tomáš Kozák")

	for _, photo := range sourcePhotos {
		// Get faces from our database
		faces, err := faceRepo.GetFaces(ctx, photo.UID)
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
		// Normalize names for comparison (remove diacritics, lowercase, dashes to spaces)
		var personMarker *photoprism.Marker
		personNameNorm := normalizePersonName(personName)

		for i := range markers {
			if markers[i].Type != "face" {
				continue
			}
			markerNameNorm := normalizePersonName(markers[i].Name)

			// Exact match after normalization
			if markerNameNorm == personNameNorm {
				personMarker = &markers[i]
				break
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
				personMarker = &markers[i]
				break
			}
		}

		if personMarker == nil {
			if !jsonOutput {
				// List available marker names for debugging
				var names []string
				for _, m := range markers {
					if m.Type == "face" {
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
		markerBBox := markerToRelativeBBox(*personMarker)
		var bestFace *database.StoredFace
		bestIoU := -1.0

		for i := range faces {
			if len(faces[i].BBox) != 4 {
				continue
			}
			faceBBoxRel := convertPixelBBoxToRelative(faces[i].BBox, width, height)
			iou := computeIoU(markerBBox, faceBBoxRel)
			if iou > bestIoU {
				bestIoU = iou
				bestFace = &faces[i]
			}
		}

		if bestFace != nil {
			sourceFaces = append(sourceFaces, sourceData{
				PhotoUID:  photo.UID,
				Embedding: bestFace.Embedding,
				BBox:      bestFace.BBox,
			})
		} else {
			if !jsonOutput {
				fmt.Printf("Warning: no matching face for marker in %s\n", photo.UID)
			}
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

	// Search for similar faces using each source embedding
	// Track how many source embeddings match each candidate
	type matchCandidate struct {
		PhotoUID   string
		Distance   float64 // Best (lowest) distance
		FaceIndex  int
		BBox       []float64
		MatchCount int // Number of source embeddings that matched
	}
	matchMap := make(map[string]*matchCandidate)
	sourcePhotoSet := make(map[string]bool)
	for _, uid := range sourcePhotoUIDs {
		sourcePhotoSet[uid] = true
	}

	searchLimit := 1000 // Search limit per embedding
	if limit > 0 && limit < searchLimit {
		searchLimit = limit * 10 // Get more results to allow for dedup
	}

	for _, embedding := range sourceEmbeddings {
		faces, distances, err := faceRepo.FindSimilarWithDistance(ctx, embedding, searchLimit, threshold)
		if err != nil {
			return fmt.Errorf("failed to search for similar faces: %w", err)
		}

		for i, face := range faces {
			// Skip source photos
			if sourcePhotoSet[face.PhotoUID] {
				continue
			}

			// Track match count and keep best distance
			if existing, ok := matchMap[face.PhotoUID]; ok {
				existing.MatchCount++
				if distances[i] < existing.Distance {
					existing.Distance = distances[i]
					existing.FaceIndex = face.FaceIndex
					existing.BBox = face.BBox
				}
			} else {
				matchMap[face.PhotoUID] = &matchCandidate{
					PhotoUID:   face.PhotoUID,
					Distance:   distances[i],
					FaceIndex:  face.FaceIndex,
					BBox:       face.BBox,
					MatchCount: 1,
				}
			}
		}
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
	for i := 0; i < len(candidates)-1; i++ {
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
	const iouThreshold = 0.3 // IoU threshold to consider markers as matching
	matches := make([]MatchResult, 0, len(candidates))
	summary := MatchSummary{}

	for _, c := range candidates {
		result := MatchResult{
			PhotoUID:  c.PhotoUID,
			Distance:  c.Distance,
			FaceIndex: c.FaceIndex,
			BBox:      c.BBox,
			Action:    ActionCreateMarker, // Default: no marker found
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

				// Skip faces smaller than 1.5% of photo width or 35px
				if faceWidth < 35 || faceWidthRel < 0.015 {
					if !jsonOutput {
						fmt.Printf("  Skipping %s: face too small (%.0fpx, %.1f%%)\n", c.PhotoUID, faceWidth, faceWidthRel*100)
					}
					continue
				}

				relBBox := convertPixelBBoxToRelative(c.BBox, width, height)
				// Store as [x, y, w, h] format for marker creation
				result.BBoxRel = []float64{
					relBBox[0],
					relBBox[1],
					relBBox[2] - relBBox[0], // width
					relBBox[3] - relBBox[1], // height
				}

				// Fetch markers from PhotoPrism
				markers, err := pp.GetPhotoMarkers(c.PhotoUID)
				if err != nil {
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "Warning: could not get markers for %s: %v\n", c.PhotoUID, err)
					}
				} else if len(markers) > 0 {
					// Find best matching marker by IoU
					var bestMarker *photoprism.Marker
					bestIoU := 0.0

					for i := range markers {
						if markers[i].Type != "face" {
							continue
						}
						markerBBox := markerToRelativeBBox(markers[i])
						iou := computeIoU(relBBox, markerBBox)
						if iou > bestIoU {
							bestIoU = iou
							bestMarker = &markers[i]
						}
					}

					if bestMarker != nil && bestIoU >= iouThreshold {
						result.MarkerUID = bestMarker.UID
						result.MarkerName = bestMarker.Name
						result.IoU = bestIoU

						if bestMarker.Name != "" && bestMarker.SubjUID != "" {
							// Marker exists and has a person assigned
							result.Action = ActionAlreadyDone
						} else {
							// Marker exists but no person assigned
							result.Action = ActionAssignPerson
						}
					}
				}
			}
		}

		// Update summary
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
			if m.Action == ActionAlreadyDone {
				continue
			}

			if dryRun {
				// Just mark as would be applied
				if !jsonOutput {
					switch m.Action {
					case ActionCreateMarker:
						fmt.Printf("  [DRY-RUN] Would create marker for %s and assign %s\n", m.PhotoUID, nameToApply)
					case ActionAssignPerson:
						fmt.Printf("  [DRY-RUN] Would assign %s to marker %s on %s\n", nameToApply, m.MarkerUID, m.PhotoUID)
					}
				}
				continue
			}

			// Actually apply
			switch m.Action {
			case ActionCreateMarker:
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
					Type:    "face",
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

			case ActionAssignPerson:
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
			}
		}

		if !jsonOutput && !dryRun {
			fmt.Printf("\nApplied: %d, Errors: %d\n", appliedCount, errorCount)
		}

		// Add successfully applied photos to album "Osoba <name>"
		if !dryRun && appliedCount > 0 {
			// Collect applied photo UIDs
			var appliedPhotoUIDs []string
			for _, m := range matches {
				if m.Applied {
					appliedPhotoUIDs = append(appliedPhotoUIDs, m.PhotoUID)
				}
			}

			if len(appliedPhotoUIDs) > 0 {
				albumTitle := fmt.Sprintf("Osoba %s", nameToApply)

				// Search for existing album
				var albumUID string
				albums, err := pp.GetAlbums(100, 0, "", albumTitle)
				if err != nil {
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "Warning: could not search for album: %v\n", err)
					}
				} else {
					// Find exact match
					for _, a := range albums {
						if a.Title == albumTitle {
							albumUID = a.UID
							break
						}
					}
				}

				// Create album if not found
				if albumUID == "" {
					if !jsonOutput {
						fmt.Printf("Creating album %q...\n", albumTitle)
					}
					album, err := pp.CreateAlbum(albumTitle)
					if err != nil {
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "Error creating album: %v\n", err)
						}
					} else {
						albumUID = album.UID
						if !jsonOutput {
							fmt.Printf("Created album %s\n", albumUID)
						}
					}
				} else {
					if !jsonOutput {
						fmt.Printf("Using existing album %q (%s)\n", albumTitle, albumUID)
					}
				}

				// Add photos to album
				if albumUID != "" {
					if !jsonOutput {
						fmt.Printf("Adding %d photos to album...\n", len(appliedPhotoUIDs))
					}
					if err := pp.AddPhotosToAlbum(albumUID, appliedPhotoUIDs); err != nil {
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "Error adding photos to album: %v\n", err)
						}
					} else {
						if !jsonOutput {
							fmt.Printf("Added %d photos to album %q\n", len(appliedPhotoUIDs), albumTitle)
						}
					}
				}
			}
		}
	}

	// Save matched photos with bounding boxes
	if saveMatches && len(matches) > 0 {
		outputDir := filepath.Join("test", "matches")

		if !jsonOutput {
			fmt.Printf("\nSaving %d matched photos to %s/...\n", len(matches), outputDir)
		}

		savedCount := 0
		for _, m := range matches {
			if len(m.BBox) != 4 {
				continue
			}

			err := saveMatchedPhoto(pp, m.PhotoUID, m.BBox, outputDir)
			if err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "  Error saving %s: %v\n", m.PhotoUID, err)
				}
			} else {
				savedCount++
				if !jsonOutput {
					fmt.Printf("  Saved %s.jpg\n", m.PhotoUID)
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
	fmt.Fprintln(w, "PHOTO UID\tDISTANCE\tACTION\tMARKER UID\tMARKER NAME\tIoU")
	fmt.Fprintln(w, "---------\t--------\t------\t----------\t-----------\t---")

	for _, m := range matches {
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
			m.PhotoUID, m.Distance, m.Action, markerUID, markerName, iouStr)
	}

	w.Flush()

	// Print summary
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Create marker:  %d\n", summary.CreateMarker)
	fmt.Printf("  Assign person:  %d\n", summary.AssignPerson)
	fmt.Printf("  Already done:   %d\n", summary.AlreadyDone)

	return nil
}
