package handlers

import (
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/constants"
	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

// MatchAction is an alias for facematch.MatchAction used in API responses
type MatchAction = facematch.MatchAction

// Action constants - re-exported from facematch package
const (
	ActionCreateMarker   = facematch.ActionCreateMarker
	ActionAssignPerson   = facematch.ActionAssignPerson
	ActionAlreadyDone    = facematch.ActionAlreadyDone
	ActionUnassignPerson = facematch.ActionUnassignPerson
)

// Helper functions - delegating to facematch package

func normalizePersonName(name string) string {
	return facematch.NormalizePersonName(name)
}

func computeIoU(bbox1, bbox2 []float64) float64 {
	return facematch.ComputeIoU(bbox1, bbox2)
}

func convertPixelBBoxToRelative(bbox []float64, width, height int) []float64 {
	return facematch.ConvertPixelBBoxToRelative(bbox, width, height)
}

// convertPixelBBoxToDisplayRelative delegates to facematch.ConvertPixelBBoxToDisplayRelative.
// See that function for EXIF orientation handling documentation.
func convertPixelBBoxToDisplayRelative(bbox []float64, displayWidth, displayHeight, orientation int) []float64 {
	return facematch.ConvertPixelBBoxToDisplayRelative(bbox, displayWidth, displayHeight, orientation)
}

func markerToRelativeBBox(m photoprism.Marker) []float64 {
	return facematch.MarkerToCornerBBox(m.X, m.Y, m.W, m.H)
}

// primaryFileInfo holds extracted info from the primary file
type primaryFileInfo struct {
	UID         string
	Width       int
	Height      int
	Orientation int
}

// extractPrimaryFileInfo extracts dimensions and orientation from the primary file in photo details.
// Face detection runs on the primary file, so we must use its dimensions for coordinate conversion.
func extractPrimaryFileInfo(details map[string]interface{}) *primaryFileInfo {
	files, ok := details["Files"].([]interface{})
	if !ok || len(files) == 0 {
		return nil
	}

	// Find the primary file
	var primaryFile map[string]interface{}
	for _, f := range files {
		if file, ok := f.(map[string]interface{}); ok {
			if isPrimary, ok := file["Primary"].(bool); ok && isPrimary {
				primaryFile = file
				break
			}
		}
	}
	// Fall back to first file if no primary found
	if primaryFile == nil {
		primaryFile, _ = files[0].(map[string]interface{})
	}
	if primaryFile == nil {
		return nil
	}

	info := &primaryFileInfo{Orientation: 1} // Default orientation
	if uid, ok := primaryFile["UID"].(string); ok {
		info.UID = uid
	}
	if w, ok := primaryFile["Width"].(float64); ok {
		info.Width = int(w)
	}
	if h, ok := primaryFile["Height"].(float64); ok {
		info.Height = int(h)
	}
	if o, ok := primaryFile["Orientation"].(float64); ok {
		info.Orientation = int(o)
	}
	return info
}

// findPersonMarker finds a marker belonging to the given person in a list of markers.
// It matches by normalized name (exact match or all parts contained).
// Returns nil if no matching marker is found.
func findPersonMarker(markers []photoprism.Marker, personName string) *photoprism.Marker {
	personNameNorm := normalizePersonName(personName)

	for i := range markers {
		if markers[i].Type != "face" {
			continue
		}
		markerNameNorm := normalizePersonName(markers[i].Name)

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

	markerBBox := markerToRelativeBBox(*marker)
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

	return bestFace, bestIoU
}

// findBestMarkerForBBox finds the marker that best matches a bounding box by IoU.
// The bbox should be in [x,y,w,h] format (relative coordinates).
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
		if markers[i].Type != "face" {
			continue
		}
		markerBBox := markerToRelativeBBox(markers[i])
		iou := computeIoU(bboxCorners, markerBBox)
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
