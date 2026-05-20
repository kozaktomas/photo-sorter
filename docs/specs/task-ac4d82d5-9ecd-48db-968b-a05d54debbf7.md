# Photos repository

Add a Postgres-backed repository for the new `photos` and `photo_files` tables introduced in the native photo management schema (migration `032_native_photo_management.sql`). This is the data-access layer used by the future upload pipeline and photo handlers.

## Requirements

### Files

- `internal/database/types.go` — add the new types.
- `internal/database/repository.go` — add `PhotoReader` and `PhotoWriter` interfaces.
- `internal/database/postgres/photos.go` — new file with the implementation.
- `internal/database/postgres/photos_test.go` — integration tests using the existing testcontainers setup.

### Types (add to `internal/database/types.go`)

```go
type Photo struct {
    UID            string
    FileHash       string
    FilePath       string
    FileName       string
    FileSize       int64
    FileMime       string
    FileWidth      int
    FileHeight     int
    FileOrientation int
    TakenAt        *time.Time
    TakenAtSource  string
    Title          string
    Description    string
    Notes          string
    Lat, Lng       *float64
    Altitude       *float64
    CameraMake     string
    CameraModel    string
    LensModel      string
    ISO            *int
    Aperture       *float64
    Exposure       string
    FocalLength    *float64
    Exif           map[string]any
    Favorite       bool
    Private        bool
    ArchivedAt     *time.Time
    UploadedBy     string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type PhotoFile struct {
    ID         int64
    PhotoUID   string
    FilePath   string
    FileHash   string
    FileSize   int64
    FileMime   string
    IsPrimary  bool
    Role       string
    CreatedAt  time.Time
}

type PhotoFilter struct {
    AlbumUID     string   // empty = any
    LabelUIDs    []string // AND semantics
    SubjectUIDs  []string
    Favorite     *bool
    Private      *bool
    Archived     *bool    // nil = exclude archived
    TakenFrom    *time.Time
    TakenTo      *time.Time
    BBox         *BBox    // optional
    UploadedBy   string
    Search       string   // matches title/description/file_name ILIKE
    SortBy       string   // "newest" (default) / "oldest" / "name"
    Limit        int      // 0 = default 50, max 500
    Offset       int
}

type BBox struct{ MinLat, MinLng, MaxLat, MaxLng float64 }
```

### Interfaces

```go
type PhotoReader interface {
    GetPhoto(ctx context.Context, uid string) (*Photo, error)
    GetPhotoByHash(ctx context.Context, hash string) (*Photo, error)
    ListPhotos(ctx context.Context, filter PhotoFilter) ([]Photo, int, error) // photos + total count
    ListPhotoFiles(ctx context.Context, photoUID string) ([]PhotoFile, error)
}

type PhotoWriter interface {
    CreatePhoto(ctx context.Context, p *Photo) error
    UpdatePhoto(ctx context.Context, p *Photo) error
    DeletePhoto(ctx context.Context, uid string) error // hard delete
    ArchivePhoto(ctx context.Context, uid string) error
    RestorePhoto(ctx context.Context, uid string) error
    AddPhotoFile(ctx context.Context, f *PhotoFile) error
    DeletePhotoFile(ctx context.Context, photoUID, filePath string) error
}
```

`ErrNotFound` (already exported by `internal/database`) is returned when a single-row lookup misses.

### Implementation guidelines

- Use `pgx`-compatible SQL via existing `*sql.DB` pool — match the style of other files in `internal/database/postgres/`.
- UIDs: generate inside the repository when `p.UID == ""`. Format: `"p" + random base32 16 chars` (lowercase). Provide a small helper `NewPhotoUID()` in `internal/database/postgres/photos.go`.
- `ListPhotos`: default `archived` filter excludes archived (`archived_at IS NULL`); pass `Archived: ptr(true)` to fetch only archived; pass `ptr(false)` for explicit non-archived. Be sure pagination + ordering is deterministic — order by the sort column then by UID DESC to break ties.
- Wrap raw EXIF map into/out of `jsonb` using `json.Marshal`/`Unmarshal`.

### Provider

Update `internal/database/provider.go`: add `PhotoReader()` and `PhotoWriter()` to the `Provider` interface and the existing PG implementation.

### Tests

Use the existing testcontainers helper in `internal/database/postgres/`. Cover:
- Create + GetPhoto round-trip with all fields set (including EXIF jsonb).
- GetPhotoByHash returns the photo; duplicate insert (same hash) returns a `pq` unique-violation error.
- ListPhotos filters: by date range, by archived, by search string.
- Archive + Restore toggles `archived_at`.
- AddPhotoFile + ListPhotoFiles; uniqueness of `(photo_uid, file_path)`.
- Hard `DeletePhoto` cascades `photo_files`.

## Verification

- `make test` passes.
- `make lint`, `make build-go` pass.
- No new HTTP/handler code in this task.