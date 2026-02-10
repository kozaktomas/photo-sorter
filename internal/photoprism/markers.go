package photoprism


// GetPhotoMarkers extracts markers from photo details
// Returns markers found in the photo's files
func (pp *PhotoPrism) GetPhotoMarkers(photoUID string) ([]Marker, error) {
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return nil, err
	}

	var markers []Marker

	// Extract markers from Files
	if files, ok := details["Files"].([]interface{}); ok {
		for _, fileInterface := range files {
			if file, ok := fileInterface.(map[string]interface{}); ok {
				if fileMarkers, ok := file["Markers"].([]interface{}); ok {
					for _, markerInterface := range fileMarkers {
						if m, ok := markerInterface.(map[string]interface{}); ok {
							marker := Marker{}
							if v, ok := m["UID"].(string); ok {
								marker.UID = v
							}
							if v, ok := m["FileUID"].(string); ok {
								marker.FileUID = v
							}
							if v, ok := m["Type"].(string); ok {
								marker.Type = v
							}
							if v, ok := m["Src"].(string); ok {
								marker.Src = v
							}
							if v, ok := m["Name"].(string); ok {
								marker.Name = v
							}
							if v, ok := m["SubjUID"].(string); ok {
								marker.SubjUID = v
							}
							if v, ok := m["SubjSrc"].(string); ok {
								marker.SubjSrc = v
							}
							if v, ok := m["FaceID"].(string); ok {
								marker.FaceID = v
							}
							if v, ok := m["FaceDist"].(float64); ok {
								marker.FaceDist = v
							}
							if v, ok := m["X"].(float64); ok {
								marker.X = v
							}
							if v, ok := m["Y"].(float64); ok {
								marker.Y = v
							}
							if v, ok := m["W"].(float64); ok {
								marker.W = v
							}
							if v, ok := m["H"].(float64); ok {
								marker.H = v
							}
							if v, ok := m["Size"].(float64); ok {
								marker.Size = int(v)
							}
							if v, ok := m["Score"].(float64); ok {
								marker.Score = int(v)
							}
							if v, ok := m["Invalid"].(bool); ok {
								marker.Invalid = v
							}
							if v, ok := m["Review"].(bool); ok {
								marker.Review = v
							}
							// Skip invalid/deleted markers
							if marker.Invalid {
								continue
							}
							markers = append(markers, marker)
						}
					}
				}
			}
		}
	}

	return markers, nil
}

// CreateMarker creates a new face marker on a photo
func (pp *PhotoPrism) CreateMarker(marker MarkerCreate) (*Marker, error) {
	return doPostJSONCreated[Marker](pp, "markers", marker)
}

// UpdateMarker updates an existing marker (e.g., to assign a person)
func (pp *PhotoPrism) UpdateMarker(markerUID string, update MarkerUpdate) (*Marker, error) {
	return doPutJSON[Marker](pp, "markers/"+markerUID, update)
}

// DeleteMarker marks a marker as invalid (soft delete)
func (pp *PhotoPrism) DeleteMarker(markerUID string) (*Marker, error) {
	invalid := true
	return pp.UpdateMarker(markerUID, MarkerUpdate{Invalid: &invalid})
}

// ClearMarkerSubject removes the person assignment from a marker
func (pp *PhotoPrism) ClearMarkerSubject(markerUID string) (*Marker, error) {
	return doDeleteJSON[Marker](pp, "markers/"+markerUID+"/subject", nil)
}
