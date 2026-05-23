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

The runtime shape lives in `internal/database/types.go` and mirrors the
table created by migration `032_native_photo_management.sql`:

```go
type Marker struct {
    UID        string    // Unique identifier (preserved verbatim from PhotoPrism during migration)
    PhotoUID   string    // Photo row this marker belongs to (FK → photos.uid, ON DELETE CASCADE)
    SubjectUID string    // Subject (person) UID — empty when unassigned (FK → subjects.uid, ON DELETE SET NULL)
    Type       string    // "face" or "label"
    X, Y, W, H float64   // Top-left + size (0–1, display space)
    Score      int       // Detection score (0..100)
    Invalid    bool      // Operator-marked false positive
    Reviewed   bool      // Operator-reviewed
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

There is no `file_uid`, `source`, `name`, `subject_src`, or `size`
column on the native `markers` table — PhotoPrism's per-marker
`marker_src` / `subj_src` / `marker_name` / `size` columns are not
mirrored. The cached display-side data that the face UI needs (subject
name, photo dimensions, EXIF orientation, primary file UID) lives on
the `faces` row instead (see [Cached Marker Data](#cached-marker-data)
below).

## How this differs from PhotoPrism's markers

photo-sorter's `markers` table preserves the PhotoPrism marker UID
verbatim during migration, but the schema itself is its own and is the
authoritative source of truth at runtime:

| PhotoPrism column | photo-sorter column | Notes |
|-------------------|---------------------|-------|
| `marker_uid` | `uid` | Preserved verbatim, no remap needed |
| `subj_uid` | `subject_uid` | Preserved (matches a row in `subjects`); `ON DELETE SET NULL` |
| `marker_type` | `type` | Renamed for clarity |
| `marker_invalid` | `invalid` | Renamed for clarity |
| `marker_review` | `reviewed` | Renamed for clarity |
| `x` / `y` / `w` / `h` | `x` / `y` / `w` / `h` | Same coordinate space (0–1, display space) |
| `score` | `score` | Detection confidence (0..100) |
| `file_uid` | — | Not mirrored on the marker row; resolved via the photo's primary `photo_files` row when the API needs it, and cached on `faces.file_uid`. |
| `marker_src` | — | Not mirrored (the manual/image/import distinction is not used at runtime). |
| `subj_src` | — | Not mirrored. |
| `marker_name` | — | Not mirrored. The person name comes from joining `subjects.name` via `subject_uid` (cached on `faces.subject_name`). |
| `size` | — | Not mirrored; the upload pipeline computes face size from the embedding bbox when it matters. |
| `embeddings_json` (per-marker) | — | Not mirrored. Face embeddings live in the separate `faces` table (one row per InsightFace detection) and are matched against markers via IoU. |
| `face_id` / `face_dist` | — | The clustering linkage is no longer needed once embeddings live in `faces` and pgvector HNSW handles similarity. |

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
embeddings (rows in `faces`) with markers (rows in `markers`). The
matching logic lives in `internal/web/handlers/face_photos.go`
(`matchFaceToMarker`, `applyMarkerMatch`); the geometry helpers live
in `internal/facematch/geometry.go`.

### IoU Calculation

```
IoU = Intersection Area / Union Area
```

### Matching Process

```
1. Read the face row from the faces table; its bbox is raw pixel [x1,y1,x2,y2].
2. Convert to display-relative [x,y,w,h] via
   facematch.ConvertPixelBBoxToDisplayRelative(bbox, fileWidth, fileHeight, orientation)
3. List markers for the photo (markers WHERE photo_uid = $1)
4. For each face-type marker:
   a. Convert marker (X, Y, W, H) to corner format [x1, y1, x2, y2]
      via facematch.MarkerToCornerBBox
   b. Compute IoU against the face's display-relative corners
   c. Track best match (highest IoU)
5. If best IoU >= IoUThreshold (0.1), record the marker on the face response
6. Otherwise, no marker matches this face — leave MarkerUID empty and
   default the action to `create_marker`
```

### Threshold

- **IoU Threshold:** `IoUThreshold = 0.1` (10% minimum overlap)
- Defined in `internal/constants/constants.go`

## Face-to-Marker Actions

Based on the match result, the system determines what action is needed:

| Action | Condition | Required payload to `POST /faces/apply` |
|--------|-----------|---------------|
| `create_marker` | No marker matches by IoU | `photo_uid`, `bbox_rel` (display-relative [x,y,w,h]), `person_name`, optional `face_index` for cache sync |
| `assign_person` | Marker exists, no person assigned | `marker_uid`, `person_name` (or `subject_uid`) |
| `already_done` | Marker exists with searched person assigned | (none — UI skips the action) |
| `unassign_person` | User wants to remove a person assignment from a marker | `marker_uid` |

The four-state machine is implemented in
`internal/web/handlers/face_apply.go` (`Apply`, `applyCreateMarker`,
`applyAssignPerson`, `applyUnassignPerson`); see
[`API.md`](API.md#apply-face-match) for the full request/response shape.
`create_marker` upserts the subject row via
`SubjectWriter.EnsureSubject` before writing the marker, so a new
person can be tagged in one round trip.

**Filtering:** When listing match candidates for a known person via
`POST /faces/match`, faces already assigned to a *different* person are
excluded entirely (filtered during similarity search using
`facematch.NormalizePersonName` comparison). Only faces already
assigned to the searched person are surfaced as `already_done`.

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

    // Cached marker / photo data (populated during processing)
    MarkerUID   string  // Matching marker UID (empty if no marker matched)
    SubjectUID  string  // Subject UID from marker (empty if unassigned)
    SubjectName string  // Person name from the subject row (empty if unassigned)
    PhotoWidth  int     // Primary file width in pixels (raw — display dims may differ for orientations 5–8)
    PhotoHeight int     // Primary file height in pixels
    Orientation int     // EXIF orientation (1–8)
    FileUID     string  // Primary file UID
}
```

**Cache synchronization:**

- Populated during photo processing via `enrichFacesWithMarkerData()`
  in the `internal/web/handlers` package, which runs the IoU match
  documented above and writes the cached columns back into `faces`.
- `UpdateFaceMarker()` updates the cached marker columns on the `faces`
  row whenever the web UI applies a face action (assign / unassign /
  create_marker). pgvector keeps its HNSW index in sync automatically.
- Out-of-band fixes (bulk SQL, restore-from-backup, etc.) can be
  re-derived in bulk via `POST /api/v1/process/sync-cache`.

## Name Normalization

Names are normalized for matching across different formats. The Go
side is `facematch.NormalizePersonName` (`internal/facematch/normalize.go`):

```
Input: "Jan Novák" or "jan-novak"
    ↓ RemoveDiacritics (Unicode NFD + strip Mn marks)
"Jan Novak"
    ↓ strings.ToLower
"jan novak"
    ↓ strings.ReplaceAll "-" → " "
"jan novak"
```

`FaceRepository.GetFacesBySubjectName` (and `GetPhotoUIDsWithSubjectName`)
runs the same transform in PostgreSQL via
`LOWER(REPLACE(unaccent(subject_name), '-', ' '))` so the Go-normalized
input compares equal to whatever was originally stored. The `unaccent`
extension is installed by migration `005_create_unaccent.sql`.

## Handling Unmatched Markers

When the `markers` table has a row that doesn't match any face
embedding, `appendUnmatchedMarkers` (in
`internal/web/handlers/face_photos.go`) appends a synthetic face
entry to the response. The first unmatched marker gets `face_index =
-1`, the next `-2`, and so on; `bbox_rel` is read straight from the
marker's `(X, Y, W, H)` (already display-relative); `suggestions` is
empty.

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

## Minimum Face Size

`GetPhotoFaces` (the `/api/v1/photos/{uid}/faces` endpoint) intentionally
does **not** filter faces by size — every detected face is returned so
the operator can inspect tiny faces and decide manually. The
`POST /api/v1/faces/match` endpoint *does* apply a minimum face size
filter when ranking match candidates, dropping any candidate whose
display-pixel width is below `database.MinFaceWidthPx` *or* whose
relative width is below `database.MinFaceWidthRel`. The filter lives
on `candidateToMatchResult` in
`internal/web/handlers/face_match.go`.

## Outlier Detection

`POST /api/v1/faces/outliers` ranks a person's assigned faces by how
far they sit from the person's centroid in face-embedding space
(`internal/web/handlers/face_outliers.go`). The flow:

1. Load every face row whose normalized `subject_name` matches the
   request, via `FaceRepository.GetFacesBySubjectName`.
2. Split into faces with a stored embedding and faces with
   `len(Embedding) == 0` — the latter become `missing_embeddings`
   entries with `face_index: -1` and `dist_from_centroid: -1`.
3. Compute the element-wise mean of the remaining embeddings
   (`computeFaceCentroid`) — note this is a Go-side average over the
   already-loaded subset, not a pgvector `AVG()` query.
4. Score each face with `database.CosineDistance(centroid, embedding)`
   and sort descending so the furthest faces (likely
   misassignments) appear first.

Display-relative bbox is computed lazily for the outlier response
using the cached `PhotoWidth` / `PhotoHeight` / `Orientation` columns
on the face row.

## Key Constants

```go
// internal/constants/constants.go
IoUThreshold        = 0.1    // 10% minimum overlap for face↔marker matching

// internal/constants/handlers.go
DefaultSubjectCount             = 1000  // Subjects page size for face handlers
DefaultFaceSuggestionLimit      = 5     // Max suggestions per face in GetPhotoFaces
DefaultFaceSimilarSearchLimit   = 500   // Max similar-face rows per pgvector query
FallbackFaceSuggestionThreshold = 2.0   // Widened distance cap when the primary threshold returns < limit

// internal/database/constants.go (applied by POST /faces/match)
MinFaceWidthPx  = 35     // Absolute minimum face width in pixels
MinFaceWidthRel = 0.01   // Minimum face width relative to photo width
```

For pgvector query details (HNSW indexes, `ef_search`, the cosine
operator) see [`similarity-search.md`](similarity-search.md).

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
