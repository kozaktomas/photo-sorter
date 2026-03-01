package photoprism

// mapString extracts a string value from a map by key.
func mapString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// mapFloat64 extracts a float64 value from a map by key.
func mapFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

// mapInt extracts an int value (stored as float64) from a map by key.
func mapInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// mapBool extracts a bool value from a map by key.
func mapBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// parseMarkerFromMap converts a raw map into a Marker struct.
func parseMarkerFromMap(m map[string]any) Marker {
	return Marker{
		UID:      mapString(m, "UID"),
		FileUID:  mapString(m, "FileUID"),
		Type:     mapString(m, "Type"),
		Src:      mapString(m, "Src"),
		Name:     mapString(m, "Name"),
		SubjUID:  mapString(m, "SubjUID"),
		SubjSrc:  mapString(m, "SubjSrc"),
		FaceID:   mapString(m, "FaceID"),
		FaceDist: mapFloat64(m, "FaceDist"),
		X:        mapFloat64(m, "X"),
		Y:        mapFloat64(m, "Y"),
		W:        mapFloat64(m, "W"),
		H:        mapFloat64(m, "H"),
		Size:     mapInt(m, "Size"),
		Score:    mapInt(m, "Score"),
		Invalid:  mapBool(m, "Invalid"),
		Review:   mapBool(m, "Review"),
	}
}

// extractMarkersFromFiles extracts markers from the Files array in photo details.
func extractMarkersFromFiles(files []any) []Marker {
	var markers []Marker
	for _, fileInterface := range files {
		file, ok := fileInterface.(map[string]any)
		if !ok {
			continue
		}
		fileMarkers, ok := file["Markers"].([]any)
		if !ok {
			continue
		}
		for _, markerInterface := range fileMarkers {
			m, ok := markerInterface.(map[string]any)
			if !ok {
				continue
			}
			marker := parseMarkerFromMap(m)
			if !marker.Invalid {
				markers = append(markers, marker)
			}
		}
	}
	return markers
}

// GetPhotoMarkers extracts markers from photo details.
// Returns markers found in the photo's files.
func (pp *PhotoPrism) GetPhotoMarkers(photoUID string) ([]Marker, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, err
	}

	files, ok := details["Files"].([]any)
	if !ok {
		return nil, nil
	}

	return extractMarkersFromFiles(files), nil
}

// CreateMarker creates a new face marker on a photo.
func (pp *PhotoPrism) CreateMarker(marker MarkerCreate) (*Marker, error) {
	return doPostJSONCreated[Marker](pp, "markers", marker)
}

// UpdateMarker updates an existing marker (e.g., to assign a person).
func (pp *PhotoPrism) UpdateMarker(markerUID string, update MarkerUpdate) (*Marker, error) {
	return doPutJSON[Marker](pp, "markers/"+markerUID, update)
}

// DeleteMarker marks a marker as invalid (soft delete).
func (pp *PhotoPrism) DeleteMarker(markerUID string) (*Marker, error) {
	invalid := true
	return pp.UpdateMarker(markerUID, MarkerUpdate{Invalid: &invalid})
}

// ClearMarkerSubject removes the person assignment from a marker.
func (pp *PhotoPrism) ClearMarkerSubject(markerUID string) (*Marker, error) {
	return doDeleteJSON[Marker](pp, "markers/"+markerUID+"/subject", nil)
}
