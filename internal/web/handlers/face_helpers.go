package handlers

import (
	"github.com/kozaktomas/photo-sorter/internal/facematch"
	"github.com/kozaktomas/photo-sorter/internal/photoprism"
)

// MatchAction is an alias for facematch.MatchAction used in API responses.
type MatchAction = facematch.MatchAction

// Action constants - re-exported from facematch package.
const (
	ActionCreateMarker   = facematch.ActionCreateMarker
	ActionAssignPerson   = facematch.ActionAssignPerson
	ActionAlreadyDone    = facematch.ActionAlreadyDone
	ActionUnassignPerson = facematch.ActionUnassignPerson
)

// Helper functions - delegating to facematch package.

func computeIoU(bbox1, bbox2 []float64) float64 {
	return facematch.ComputeIoU(bbox1, bbox2)
}

// convertPixelBBoxToDisplayRelative delegates to facematch.ConvertPixelBBoxToDisplayRelative.
// See that function for EXIF orientation handling documentation.
func convertPixelBBoxToDisplayRelative(bbox []float64, displayWidth, displayHeight, orientation int) []float64 {
	return facematch.ConvertPixelBBoxToDisplayRelative(bbox, displayWidth, displayHeight, orientation)
}

func markerToRelativeBBox(m photoprism.Marker) []float64 {
	return facematch.MarkerToCornerBBox(m.X, m.Y, m.W, m.H)
}

// primaryFileInfo holds extracted info from the primary file.
type primaryFileInfo struct {
	UID         string
	Width       int
	Height      int
	Orientation int
}

// findPrimaryFile locates the primary file map from the photo details Files list.
// Falls back to the first file if no primary is found.
func findPrimaryFile(details map[string]any) map[string]any {
	files, ok := details["Files"].([]any)
	if !ok || len(files) == 0 {
		return nil
	}

	for _, f := range files {
		if file, ok := f.(map[string]any); ok {
			if isPrimary, ok := file["Primary"].(bool); ok && isPrimary {
				return file
			}
		}
	}
	// Fall back to first file if no primary found.
	primaryFile, _ := files[0].(map[string]any)
	return primaryFile
}

// parseFileInfo extracts UID, dimensions, and orientation from a file map.
func parseFileInfo(file map[string]any) *primaryFileInfo {
	info := &primaryFileInfo{Orientation: 1} // Default orientation
	if uid, ok := file["UID"].(string); ok {
		info.UID = uid
	}
	if w, ok := file["Width"].(float64); ok {
		info.Width = int(w)
	}
	if h, ok := file["Height"].(float64); ok {
		info.Height = int(h)
	}
	if o, ok := file["Orientation"].(float64); ok {
		info.Orientation = int(o)
	}
	return info
}

// extractPrimaryFileInfo extracts dimensions and orientation from the primary file in photo details.
// Face detection runs on the primary file, so we must use its dimensions for coordinate conversion.
func extractPrimaryFileInfo(details map[string]any) *primaryFileInfo {
	primaryFile := findPrimaryFile(details)
	if primaryFile == nil {
		return nil
	}
	return parseFileInfo(primaryFile)
}
