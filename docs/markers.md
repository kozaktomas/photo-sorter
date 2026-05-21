# Markers System

This document explains how face markers work in photo-sorter, how they
differ from PhotoPrism's `markers` table, and how InsightFace embeddings
are matched against them.

## What is a Marker?

A marker is a region annotation on a photo, primarily used for face
detection and identification. Markers define a bounding box on a photo
and optionally link to a Subject (person).

**Key characteristics:**
- Stored in the native PostgreSQL `markers` table (managed by `internal/database/postgres/markers.go`)
- Coordinates are relative (0–1 range) in display space
- Can be created manually (via the API) or by the face-detection pipeline
- Link faces to people (Subjects) for identification

## Marker Data Structure

```go
type Marker struct {
    UID         string    // Unique identifier (preserved verbatim from PhotoPrism during migration)
    PhotoUID    string    // Photo row this marker belongs to
    FileUID     string    // Primary file UID
    Type        string    // "face" for face markers
    Source      string    // "manual" / "image" / "import"
    Name        string    // Cached person name (e.g., "Jan Novák")
    SubjectUID  string    // Subject (person) UID
    SubjectSrc  string    // "manual" if user-assigned
    X, Y, W, H  float64   // Relative position + size (0–1, display space)
    Size        int       // Face size in pixels
    Score       int       // Confidence score (0..100)
    Invalid     bool      // Soft-delete flag
    Reviewed    bool      // Has been reviewed by a human
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## How this differs from PhotoPrism's markers

photo-sorter's `markers` table preserves the PhotoPrism marker UID
verbatim during migration, but the schema itself is its own and is the
authoritative source of truth at runtime:

| PhotoPrism column | photo-sorter column | Notes |
|-------------------|---------------------|-------|
| `marker_uid` | `uid` | Preserved verbatim, no remap needed |
| `file_uid` | `file_uid` | Preserved (matches a row in `photo_files`) |
| `subj_uid` | `subject_uid` | Preserved (matches a row in `subjects`) |
| `subj_src` | `subject_src` | Preserved |
| `marker_src` | `source` | Renamed for clarity |
| `marker_type` | `type` | Renamed for clarity |
| `marker_invalid` | `invalid` | Renamed for clarity |
| `marker_review` | `reviewed` | Renamed for clarity |
| `marker_name` | `name` | Preserved |
| `x` / `y` / `w` / `h` | `x` / `y` / `w` / `h` | Same coordinate space |
| `embeddings_json` (per-marker) | — | Not mirrored. Face embeddings live in the separate `faces` table (one row per InsightFace detection) and are matched against markers via IoU. |
| `face_id` / `face_dist` | — | The clustering linkage is no longer needed once embeddings live in `faces` and HNSW handles similarity. |

There is no direct MariaDB access from runtime code. The
`migrate-from-photoprism` command reads the PhotoPrism MariaDB once at
migration time; after that, every read and write goes through the native
Postgres repositories (`internal/database/postgres/markers.go`).

## Coordinate Systems

Understanding coordinate systems is critical for marker–face matching.

### Marker Coordinates

- **Format:** `[X, Y, W, H]` — position and dimensions relative to the photo (0–1 range)
- **Space:** Display space (already accounts for EXIF orientation)
- **Origin:** Top-left corner of the displayed image

### Face Embedding Coordinates (InsightFace)

- **Format:** `[x1, y1, x2, y2]` — corner coordinates in pixels
- **Space:** Display space (InsightFace auto-rotates based on EXIF)
- **Stored in:** PostgreSQL `faces.bbox` column

### Coordinate Conversion

To match faces with markers, coordinates must be in the same space:

```
InsightFace bbox [x1, y1, x2, y2] (pixels, display space)
    ↓
ConvertPixelBBoxToDisplayRelative()
    ↓
Display-relative [x, y, w, h] (0–1 range, display space)
    ↓
Convert to corners for IoU: [x1, y1, x2, y2] (0–1 range)
```

**EXIF Orientation Handling:**

The cached `photo_width` / `photo_height` columns hold raw file
dimensions; display dimensions differ for rotated photos:

| Orientation | Rotation | Dimension Swap |
|-------------|----------|----------------|
| 1–4         | 0° or 180° | None (use raw dims) |
| 5–8         | 90° or 270° | Swap width/height |

## Matching Faces to Markers

The system uses **Intersection over Union (IoU)** to match face
embeddings with markers.

### IoU Calculation

```
IoU = Intersection Area / Union Area
```

### Matching Process

```
1. Get face bbox from the faces table (pixel coordinates)
2. Convert to display-relative coordinates
3. Get markers for the photo (markers WHERE photo_uid = ...)
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
| `already_done` | Marker exists with searched person assigned | (none — skip) |
| `unassign_person` | User wants to remove assignment | marker_uid |

**Filtering:** Faces already assigned to a *different* person are
excluded from match results entirely (filtered during similarity search
using normalized name comparison). Only faces assigned to the searched
person appear as `already_done`.

## API Operations

### Get Markers for a Photo

```
GET /api/v1/photos/{photoUID}/faces
```

Returns detected faces + their matched markers + person suggestions.
See [API.md](API.md#get-faces-in-photo) for the full schema.

### Apply a Face Action

```
POST /api/v1/faces/apply
```

The same endpoint handles `create_marker`, `assign_person`, and
`unassign_person`. See [API.md](API.md#apply-face-match) for the payload
shape.

## Cached Marker Data

To avoid joining across `markers` + `subjects` + `photos` on every face
query, the relevant columns are cached on the `faces` row:

```go
type StoredFace struct {
    // Face embedding data...

    // Cached marker data
    MarkerUID   string  // Matching marker UID
    SubjectUID  string  // Subject UID from marker
    SubjectName string  // Person name from subject row
    PhotoWidth  int     // Photo dimensions for coordinate conversion
    PhotoHeight int
    Orientation int     // EXIF orientation (1–8)
    FileUID     string  // Primary file UID
}
```

**Cache synchronization:**

- Updated during photo processing via `enrichFacesWithMarkerData()`
- Updated immediately when faces are assigned/unassigned via the web UI
- `UpdateFaceMarker()` updates the cached marker columns on the `faces` row; pgvector keeps its index in sync automatically
- Out-of-band fixes (bulk SQL, restore-from-backup, etc.) can be re-derived via `POST /api/v1/process/sync-cache`

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

When the `markers` table has a row that doesn't match any face embedding:

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
- The photo was processed before the marker was created
- A marker was added manually before face detection ran
- The InsightFace pass missed a face that an earlier model detected

## Marker Enrichment During Processing

When a photo is processed for face embeddings:

```
1. Detect faces via InsightFace → StoredFace records
2. Read photo metadata (dimensions, orientation)
3. Load markers for the photo from the markers table
4. Match faces to markers using IoU
5. Cache marker data on the face rows:
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
3. Ensure the cached `photo_width` / `photo_height` are the raw file dimensions
4. Check that dimension swap is applied for orientations 5–8

### No Marker Matches

If faces aren't matching markers despite visible overlap:

1. Lower the IoU threshold (but expect more false positives)
2. Reprocess the photo to regenerate face embeddings
3. Verify both sources are in display space (markers always are; faces should be too)
4. Confirm both systems use the same dimension values

### Cache Out of Sync

If cached marker data drifted (e.g. after bulk SQL):

1. Call `POST /api/v1/process/sync-cache` to re-derive every cached column
2. UI-driven assignments call `UpdateFaceMarker()` automatically, so day-to-day usage stays consistent
