package facematch

// ComputeIoU calculates Intersection over Union between two bounding boxes.
// bbox1 and bbox2 are [x1, y1, x2, y2] in the same coordinate system.
func ComputeIoU(bbox1, bbox2 []float64) float64 {
	if len(bbox1) != 4 || len(bbox2) != 4 {
		return 0
	}

	// Calculate intersection.
	x1 := max(bbox1[0], bbox2[0])
	y1 := max(bbox1[1], bbox2[1])
	x2 := min(bbox1[2], bbox2[2])
	y2 := min(bbox1[3], bbox2[3])

	if x2 <= x1 || y2 <= y1 {
		return 0 // No intersection
	}

	intersection := (x2 - x1) * (y2 - y1)

	// Calculate union.
	area1 := (bbox1[2] - bbox1[0]) * (bbox1[3] - bbox1[1])
	area2 := (bbox2[2] - bbox2[0]) * (bbox2[3] - bbox2[1])
	union := area1 + area2 - intersection

	if union <= 0 {
		return 0
	}

	return intersection / union
}

// ConvertPixelBBoxToRelative converts pixel bbox to relative (0-1) coordinates.
// Input bbox is [x1, y1, x2, y2] in pixels, output is [x1, y1, x2, y2] in relative coords.
func ConvertPixelBBoxToRelative(bbox []float64, width, height int) []float64 {
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

// ConvertPixelBBoxToDisplayRelative converts a pixel bounding box [x1, y1, x2, y2].
// to relative [x, y, w, h] coordinates in display space.
//
// PhotoPrism reports raw file dimensions (Width, Height) which may need to be swapped.
// for orientations 5-8 (90° rotations) to match display dimensions.
//
// The embedding service (InsightFace) auto-rotates images based on EXIF orientation.
// before face detection, so bbox coordinates are already in display space. We just need.
// to convert from display pixels to relative coordinates (0-1) - no coordinate.
// transformation is needed since InsightFace already handles the rotation.
func ConvertPixelBBoxToDisplayRelative(bbox []float64, fileWidth, fileHeight, orientation int) []float64 {
	if len(bbox) != 4 || fileWidth <= 0 || fileHeight <= 0 {
		return bbox
	}

	// Get display dimensions from raw file dimensions.
	// PhotoPrism reports raw file dimensions, but for orientations 5-8 (90° rotations),.
	// the display dimensions are swapped. InsightFace sees the rotated image, so bbox.
	// coordinates are in display space - we need display dimensions to convert correctly.
	displayWidth, displayHeight := fileWidth, fileHeight
	if orientation >= 5 && orientation <= 8 {
		displayWidth, displayHeight = fileHeight, fileWidth
	}

	// Convert pixel bbox [x1, y1, x2, y2] to relative coordinates using display dimensions.
	// No coordinate transformation needed - InsightFace already rotated the image.
	x1 := bbox[0] / float64(displayWidth)
	y1 := bbox[1] / float64(displayHeight)
	x2 := bbox[2] / float64(displayWidth)
	y2 := bbox[3] / float64(displayHeight)

	// Convert to [x, y, w, h] format.
	return []float64{x1, y1, x2 - x1, y2 - y1}
}

// MarkerToCornerBBox converts PhotoPrism marker (X, Y, W, H) to [x1, y1, x2, y2] corner format.
// This is useful for IoU calculations which expect corner coordinates.
func MarkerToCornerBBox(x, y, w, h float64) []float64 {
	return []float64{
		x,
		y,
		x + w,
		y + h,
	}
}
