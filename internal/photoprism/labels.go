package photoprism

import "fmt"

// GetLabels retrieves labels from PhotoPrism
func (pp *PhotoPrism) GetLabels(count int, offset int, all bool) ([]Label, error) {
	endpoint := fmt.Sprintf("labels?count=%d&offset=%d", count, offset)
	if all {
		endpoint += "&all=true"
	}

	result, err := doGetJSON[[]Label](pp, endpoint)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

// UpdateLabel updates a label's metadata
func (pp *PhotoPrism) UpdateLabel(uid string, update LabelUpdate) (*Label, error) {
	return doPutJSON[Label](pp, "labels/"+uid, update)
}

// DeleteLabels deletes multiple labels by their UIDs
func (pp *PhotoPrism) DeleteLabels(labelUIDs []string) error {
	if len(labelUIDs) == 0 {
		return nil
	}

	selection := struct {
		Labels []string `json:"labels"`
	}{
		Labels: labelUIDs,
	}

	return doRequestRaw(pp, "POST", "batch/labels/delete", selection)
}

// AddPhotoLabel adds a label/tag to a photo
func (pp *PhotoPrism) AddPhotoLabel(photoUID string, label PhotoLabel) (*Photo, error) {
	return doPostJSON[Photo](pp, fmt.Sprintf("photos/%s/label", photoUID), label)
}

// RemovePhotoLabel removes a label/tag from a photo
func (pp *PhotoPrism) RemovePhotoLabel(photoUID string, labelID string) (*Photo, error) {
	return doDeleteJSON[Photo](pp, fmt.Sprintf("photos/%s/label/%s", photoUID, labelID), nil)
}

// UpdatePhotoLabel updates a label/tag on a photo (mainly used to change uncertainty)
func (pp *PhotoPrism) UpdatePhotoLabel(photoUID string, labelID string, label PhotoLabel) (*Photo, error) {
	return doPutJSON[Photo](pp, fmt.Sprintf("photos/%s/label/%s", photoUID, labelID), label)
}

// extractLabelIDs extracts label IDs from the Labels array in photo details.
func extractLabelIDs(details map[string]any) []string {
	labels, ok := details["Labels"].([]any)
	if !ok {
		return nil
	}

	var labelIDs []string
	for _, labelInterface := range labels {
		label, ok := labelInterface.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := label["LabelID"].(float64); ok {
			labelIDs = append(labelIDs, fmt.Sprintf("%.0f", id))
		} else if id, ok := label["ID"].(float64); ok {
			labelIDs = append(labelIDs, fmt.Sprintf("%.0f", id))
		} else if id, ok := label["LabelID"].(string); ok {
			labelIDs = append(labelIDs, id)
		} else if id, ok := label["ID"].(string); ok {
			labelIDs = append(labelIDs, id)
		}
	}
	return labelIDs
}

// RemoveAllPhotoLabels removes all labels/tags from a photo
// It first retrieves the photo details to get all label IDs, then removes each one
func (pp *PhotoPrism) RemoveAllPhotoLabels(photoUID string) error {
	// Get photo details to retrieve all labels
	details, err := pp.GetPhotoDetails(photoUID)
	if err != nil {
		return fmt.Errorf("could not get photo details: %w", err)
	}

	// Remove each label
	for _, labelID := range extractLabelIDs(details) {
		_, err := pp.RemovePhotoLabel(photoUID, labelID)
		if err != nil {
			return fmt.Errorf("could not remove label %s: %w", labelID, err)
		}
	}

	return nil
}
