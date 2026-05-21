# EXIF edit endpoint

Add a PUT endpoint that lets editors/admins correct GPS, taken-at date, camera info, and free-text fields. The change is persisted to:
1. The Postgres `photos` row (already supported by the photo-update task).
2. An XMP sidecar file alongside the original, so the change survives a future export/migration of the on-disk files.

The XMP sidecar is the standard PhotoPrism location: same directory + same basename + `.xmp` extension (e.g. `originals/2024/06/IMG_1234.jpg` → `originals/2024/06/IMG_1234.xmp`).

## Requirements

### Endpoint

`PUT /api/v1/photos/{uid}/exif`

Body (all optional):

```json
{
  "taken_at": "2024-06-15T14:30:00Z",
  "lat": 50.08, "lng": 14.43, "altitude": 220,
  "camera_make": "Canon", "camera_model": "EOS R5",
  "lens_model": "RF 50mm f/1.2", "iso": 100,
  "aperture": 1.8, "exposure": "1/250", "focal_length": 50,
  "title": "...", "description": "...", "notes": "..."
}
```

Validation:
- Year ∈ [1900, 2100]
- Lat/lng ranges
- ISO > 0

Authorization: `HasWriteAccess` (editor or admin).

### XMP sidecar writer

Add `internal/exif/sidecar.go`:

```go
type SidecarFields struct {
    TakenAt        *time.Time
    Lat, Lng       *float64
    Altitude       *float64
    CameraMake     string
    CameraModel    string
    LensModel      string
    ISO            *int
    Aperture       *float64
    Exposure       string
    FocalLength    *float64
    Title          string
    Description    string
    Notes          string
}

func WriteSidecar(ctx context.Context, sidecarPath string, fields SidecarFields) error
```

Implementation:
- Prefer `exiftool` subprocess: `exiftool -overwrite_original -P <flag...> -<tag>=<value> ... <sidecar_path>`. If the sidecar doesn't exist, exiftool creates it. Use `-srcfile @ -tagsFromFile @ <orig>` if needed.
- Map fields to standard XMP tags: `XMP-dc:Title`, `XMP-dc:Description`, `XMP-exif:DateTimeOriginal`, `XMP-exif:GPSLatitude` + `GPSLatitudeRef`, etc.
- Atomic: write to `<path>.tmp` first, rename on success.
- Timeout 20s.

If `exiftool` is unavailable, log a warning and skip the sidecar write — the DB is the source of truth, so the API call should still succeed.

### Handler flow

1. Validate input.
2. Load existing photo (404 if missing/archived).
3. Apply changes to the photo row via `PhotoWriter.UpdatePhoto`.
4. Compute the sidecar path next to the original: `filepath.Join(originalsRoot, dir, basename+".xmp")` where `dir` and `basename` come from `photo.FilePath`.
5. Call `exif.WriteSidecar` — log error but do not fail the request.
6. Return the updated photo.

### Tests

- PUT updates the DB row.
- Sidecar file is created next to the original (skip the sidecar assertion if `exiftool` not in PATH).
- Invalid date / lat → 400.

## Verification

- `make test` passes.
- `make lint`, `make build-go` pass.