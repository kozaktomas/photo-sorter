package facematch

// PrimaryFileInfo holds extracted info from the primary file of a photo
type PrimaryFileInfo struct {
	UID         string
	Width       int
	Height      int
	Orientation int
}

// findPrimaryFile finds the primary file map from the Files array in photo details.
func findPrimaryFile(files []interface{}) map[string]interface{} {
	for _, f := range files {
		file, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		if isPrimary, ok := file["Primary"].(bool); ok && isPrimary {
			return file
		}
	}
	// Fall back to first file if no primary found
	if first, ok := files[0].(map[string]interface{}); ok {
		return first
	}
	return nil
}

// ExtractPrimaryFileInfo extracts dimensions and orientation from photo details.
// The details map is the JSON response from PhotoPrism's GetPhotoDetails endpoint.
// Face detection runs on the primary file, so we must use its dimensions.
func ExtractPrimaryFileInfo(details map[string]interface{}) *PrimaryFileInfo {
	files, ok := details["Files"].([]interface{})
	if !ok || len(files) == 0 {
		return nil
	}

	primaryFile := findPrimaryFile(files)
	if primaryFile == nil {
		return nil
	}

	info := &PrimaryFileInfo{Orientation: 1} // Default orientation
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

// MarkerInfo represents a PhotoPrism marker's relevant fields for matching
type MarkerInfo struct {
	UID      string
	Type     string
	Name     string
	SubjUID  string
	X, Y, W, H float64
}

// MatchResult represents the result of matching a face to a marker
type MatchResult struct {
	MarkerUID   string
	SubjectUID  string
	SubjectName string
	IoU         float64
}

// MatchFaceToMarkers finds the best matching marker for a face bounding box.
// faceBBox is in raw pixel coordinates [x1, y1, x2, y2].
// Returns nil if no marker matches above the IoU threshold.
func MatchFaceToMarkers(faceBBox []float64, markers []MarkerInfo, width, height, orientation int, iouThreshold float64) *MatchResult {
	if len(faceBBox) != 4 || width <= 0 || height <= 0 {
		return nil
	}

	// Convert pixel bbox to display-relative [x, y, w, h] format
	displayBBox := ConvertPixelBBoxToDisplayRelative(faceBBox, width, height, orientation)

	// Convert to corner format [x1, y1, x2, y2] for IoU comparison
	displayCorners := []float64{
		displayBBox[0],
		displayBBox[1],
		displayBBox[0] + displayBBox[2],
		displayBBox[1] + displayBBox[3],
	}

	var bestMarker *MarkerInfo
	bestIoU := 0.0

	for i := range markers {
		if markers[i].Type != "face" {
			continue
		}
		// Marker coordinates are already in display-relative [x, y, w, h] format
		markerCorners := MarkerToCornerBBox(markers[i].X, markers[i].Y, markers[i].W, markers[i].H)
		iou := ComputeIoU(displayCorners, markerCorners)
		if iou > bestIoU {
			bestIoU = iou
			bestMarker = &markers[i]
		}
	}

	if bestMarker == nil || bestIoU < iouThreshold {
		return nil
	}

	return &MatchResult{
		MarkerUID:   bestMarker.UID,
		SubjectUID:  bestMarker.SubjUID,
		SubjectName: bestMarker.Name,
		IoU:         bestIoU,
	}
}

// ConvertMarkersToInfo converts PhotoPrism marker data to MarkerInfo slice.
// This is a helper for callers that have raw marker data from API responses.
func ConvertMarkersToInfo(markers []map[string]interface{}) []MarkerInfo {
	result := make([]MarkerInfo, 0, len(markers))
	for _, m := range markers {
		info := MarkerInfo{}
		if uid, ok := m["UID"].(string); ok {
			info.UID = uid
		}
		if t, ok := m["Type"].(string); ok {
			info.Type = t
		}
		if name, ok := m["Name"].(string); ok {
			info.Name = name
		}
		if subjUID, ok := m["SubjUID"].(string); ok {
			info.SubjUID = subjUID
		}
		if x, ok := m["X"].(float64); ok {
			info.X = x
		}
		if y, ok := m["Y"].(float64); ok {
			info.Y = y
		}
		if w, ok := m["W"].(float64); ok {
			info.W = w
		}
		if h, ok := m["H"].(float64); ok {
			info.H = h
		}
		result = append(result, info)
	}
	return result
}
