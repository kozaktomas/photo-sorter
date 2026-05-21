package handlers

import (
	"context"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/facematch"
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

// markerToRelativeBBox converts a native database.Marker into the corner-form
// bounding box [x1, y1, x2, y2] used by computeIoU. Native markers store
// x/y/w/h directly in display-relative space (0..1), so the helper is a
// straight conversion with no orientation handling.
func markerToRelativeBBox(m database.Marker) []float64 {
	return facematch.MarkerToCornerBBox(m.X, m.Y, m.W, m.H)
}

// primaryFileInfo holds extracted info from the primary file. Width/Height
// are in raw pixel space; Orientation is the EXIF orientation tag (1-8).
type primaryFileInfo struct {
	UID         string
	Width       int
	Height      int
	Orientation int
}

// resolvePrimaryFilePath returns the storage path of the primary
// PhotoFile, falling back to the first row when no IsPrimary flag is set.
// Returns the empty string when the slice is empty.
func resolvePrimaryFilePath(files []database.PhotoFile) string {
	for i := range files {
		if files[i].IsPrimary {
			return files[i].FilePath
		}
	}
	if len(files) > 0 {
		return files[0].FilePath
	}
	return ""
}

// fetchPhotoFileInfo loads the native Photo + primary PhotoFile for the
// given UID and folds them into primaryFileInfo. Returns nil + a non-nil
// error if either lookup fails; returns nil + nil if the photo has no
// usable primary file (no rows in photo_files, or zero dimensions).
func fetchPhotoFileInfo(
	ctx context.Context, reader database.PhotoReader, photoUID string,
) (*primaryFileInfo, error) {
	if reader == nil {
		return nil, nil //nolint:nilnil // No reader configured — caller treats as missing.
	}
	photo, err := reader.GetPhoto(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("get photo: %w", err)
	}
	info := &primaryFileInfo{
		Width:       photo.FileWidth,
		Height:      photo.FileHeight,
		Orientation: photo.FileOrientation,
	}
	if info.Orientation == 0 {
		info.Orientation = 1
	}
	files, err := reader.ListPhotoFiles(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("list photo files: %w", err)
	}
	info.UID = resolvePrimaryFilePath(files)
	if info.Width == 0 || info.Height == 0 {
		return nil, nil //nolint:nilnil // Photo lacks usable dimensions.
	}
	return info, nil
}
