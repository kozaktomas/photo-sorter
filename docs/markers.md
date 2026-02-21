# Markers System

This document explains how markers work in the Photo Sorter application, including their relationship with face embeddings, coordinate systems, and the matching process.

## What is a Marker?

A marker is a region annotation on a photo in PhotoPrism, primarily used for face detection and identification. Markers define a bounding box location on a photo and optionally link to a Subject (person).

**Key characteristics:**
- Stored in PhotoPrism, managed via REST API
- Coordinates are relative (0-1 range) in display space
- Can be created manually or by PhotoPrism's face detection
- Link faces to people (Subjects) for identification

## Marker Data Structure

```go
type Marker struct {
    UID      string  // Unique identifier
    FileUID  string  // Parent file UID
    Type     string  // "face" for face markers
    Src      string  // Source: "manual", "image", etc.
    Name     string  // Person name (e.g., "Jan Novak")
    SubjUID  string  // Subject (person) UID
    SubjSrc  string  // Subject source: "manual" if user-assigned
    FaceID   string  // Associated face cluster ID
    FaceDist float64 // Distance to face cluster
    X        float64 // Relative X position (0-1)
    Y        float64 // Relative Y position (0-1)
    W        float64 // Relative width (0-1)
    H        float64 // Relative height (0-1)
    Size     int     // Face size in pixels
    Score    int     // Confidence score
    Invalid  bool    // Soft delete flag
    Review   bool    // Needs review flag
}
```

## Coordinate Systems

Understanding coordinate systems is critical for marker-face matching.

### Marker Coordinates (PhotoPrism)

- **Format:** `[X, Y, W, H]` - position and dimensions relative to photo (0-1 range)
- **Space:** Display space (already accounts for EXIF orientation)
- **Origin:** Top-left corner of the displayed image

### Face Embedding Coordinates (InsightFace)

- **Format:** `[x1, y1, x2, y2]` - corner coordinates in pixels
- **Space:** Display space (InsightFace auto-rotates based on EXIF)
- **Stored in:** PostgreSQL `faces` table as `bbox` field

### Coordinate Conversion

To match faces with markers, coordinates must be in the same space:

```
InsightFace bbox [x1, y1, x2, y2] (pixels, display space)
    ↓
ConvertPixelBBoxToDisplayRelative()
    ↓
Display-relative [x, y, w, h] (0-1 range, display space)
    ↓
Convert to corners for IoU: [x1, y1, x2, y2] (0-1 range)
```

**EXIF Orientation Handling:**

PhotoPrism reports raw file dimensions, but display dimensions differ for rotated photos:

| Orientation | Rotation | Dimension Swap |
|-------------|----------|----------------|
| 1-4         | 0° or 180° | None (use raw dims) |
| 5-8         | 90° or 270° | Swap width/height |

## Matching Faces to Markers

The system uses **Intersection over Union (IoU)** to match face embeddings with PhotoPrism markers.

### IoU Calculation

```
IoU = Intersection Area / Union Area

Where:
- Intersection = overlapping area of both boxes
- Union = total area covered by either box
```

### Matching Process

```
1. Get face bbox from database (pixel coordinates)
2. Convert to display-relative coordinates
3. Get markers from PhotoPrism for the photo
4. For each face marker:
   a. Convert marker [X,Y,W,H] to corner format [x1,y1,x2,y2]
   b. Compute IoU with face bbox
   c. Track best match (highest IoU)
5. If best IoU >= threshold (0.1), return match
6. Otherwise, no marker matches this face
```

### Threshold

- **IoU Threshold:** 0.1 (10% minimum overlap)
- Defined in `internal/constants/constants.go`

## Face-to-Marker Actions

Based on the match result, the system determines what action is needed:

| Action | Condition | Required Data |
|--------|-----------|---------------|
| `create_marker` | No marker matches by IoU | file_uid, bbox_rel, person_name |
| `assign_person` | Marker exists, no person assigned | marker_uid, person_name |
| `already_done` | Marker exists with searched person assigned | (none - skip) |
| `unassign_person` | User wants to remove assignment | marker_uid |

**Filtering:** Faces already assigned to a *different* person are excluded from match results entirely (filtered during similarity search using normalized name comparison). Only faces assigned to the searched person appear as `already_done`.

## API Operations

### Get Markers for a Photo

Markers are extracted from photo details:

```
GET /api/v1/photos/{photoUID}

Response includes Files[].Markers array with all markers
```

### Create a Marker

```
POST /api/v1/markers

Request:
{
  "FileUID": "fq8xyz...",
  "Type": "face",
  "X": 0.25,
  "Y": 0.10,
  "W": 0.15,
  "H": 0.20,
  "Name": "Jan Novak",
  "Src": "manual",
  "SubjSrc": "manual"
}
```

### Update a Marker (Assign Person)

```
PUT /api/v1/markers/{markerUID}

Request:
{
  "Name": "Jan Novak",
  "SubjSrc": "manual"
}
```

### Unassign Person from Marker

```
DELETE /api/v1/markers/{markerUID}/subject
```

### Delete a Marker (Soft Delete)

```
DELETE /api/v1/markers/{markerUID}

This sets Invalid: true (soft delete)
```

## Cached Marker Data

To avoid repeated PhotoPrism API calls, marker data is cached in the PostgreSQL `faces` table:

```go
type StoredFace struct {
    // Face embedding data...

    // Cached PhotoPrism data
    MarkerUID   string  // Matching marker UID
    SubjectUID  string  // Subject UID from marker
    SubjectName string  // Person name from marker
    PhotoWidth  int     // Photo dimensions for coordinate conversion
    PhotoHeight int
    Orientation int     // EXIF orientation (1-8)
    FileUID     string  // Primary file UID
}
```

**Cache synchronization:**
- Updated during photo processing via `enrichFacesWithMarkerData()`
- Updated when faces are assigned/unassigned via web UI
- Uses `UpdateFaceMarker()` to sync individual face records

## Name Normalization

Names are normalized for matching across different formats:

```
Input: "Jan Novák" or "jan-novak"
    ↓
Remove diacritics: "Jan Novak"
    ↓
Lowercase: "jan novak"
    ↓
Replace dashes: "jan novak"
```

**Matching logic:**
- Exact match after normalization
- Contains match: all parts of search name must be in marker name

## Handling Unmatched Markers

When PhotoPrism has markers that don't match any face embedding:

```json
{
  "face_index": -1,
  "bbox_rel": [0.25, 0.10, 0.15, 0.20],
  "marker_uid": "mq8def...",
  "action": "assign_person",
  "suggestions": []
}
```

These appear when:
- Photo was processed before marker creation
- PhotoPrism detected a face that InsightFace didn't
- Marker was added manually without reprocessing

## Marker Enrichment During Processing

When a photo is processed for face embeddings:

```
1. Detect faces via InsightFace → StoredFace records
2. Fetch photo metadata (dimensions, orientation)
3. Get markers from PhotoPrism
4. Match faces to markers using IoU
5. Cache marker data in PostgreSQL:
   - MarkerUID, SubjectUID, SubjectName
   - PhotoWidth, PhotoHeight, Orientation, FileUID
```

## Key Constants

```go
const (
    IoUThreshold         = 0.1   // 10% minimum overlap for matching
    MinFaceWidthPx       = 35    // Minimum face size in pixels
    MinFaceWidthRel      = 0.01  // Minimum face size as % of photo width
    DefaultSubjectCount  = 5000  // Max subjects to load for lookup
)
```

## Common Issues

### Misaligned Bounding Boxes

If face boxes don't align with displayed faces:

1. Check the photo's EXIF orientation value
2. Verify InsightFace is auto-rotating images
3. Ensure PhotoPrism reports raw file dimensions
4. Check that dimension swap is applied for orientations 5-8

### No Marker Matches

If faces aren't matching markers despite visible overlap:

1. Lower the IoU threshold (but may increase false positives)
2. Reprocess the photo to regenerate face embeddings
3. Check if marker coordinates are in correct space
4. Verify both systems use the same dimension values

### Cache Out of Sync

If cached marker data doesn't match PhotoPrism:

1. Reprocess affected photos via "Rebuild Index"
2. Or manually trigger `enrichFacesWithMarkerData()` via photo processing
3. Check that `UpdateFaceMarker()` is being called after API operations
