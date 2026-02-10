package handlers

import (
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
