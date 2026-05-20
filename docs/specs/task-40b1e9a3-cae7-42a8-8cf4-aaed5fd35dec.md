# Storage layer for originals and thumbnails

Create `internal/storage` — a self-contained Go package that owns the on-disk layout for photo originals and the thumbnail cache, mirroring the PhotoPrism convention so a future migration script can drop existing files in without renaming.

This task ONLY introduces the package and its tests. No HTTP handlers or DB code yet.

## Requirements

### Package layout

```
internal/storage/
  storage.go      // Storage struct + constructor + interfaces
  paths.go        // path generation (originals + cache)
  hash.go         // file hashing (SHA256)
  write.go        // atomic write helpers
  storage_test.go
```

### Configuration

Read two env vars in `internal/config`:
- `STORAGE_ORIGINALS_PATH` (default `/data/originals` in Docker, `./data/originals` for dev)
- `STORAGE_CACHE_PATH` (default `/data/cache`, `./data/cache` for dev)

Add a `Storage` field to the existing `Config` struct in `internal/config/config.go` exposing these as `OriginalsPath` and `CachePath`. Update `.env.dev` with sensible local values.

### Storage struct

```go
type Storage struct {
    originalsRoot string
    cacheRoot     string
}

func New(originalsRoot, cacheRoot string) (*Storage, error)
```

Constructor must:
- Validate both directories exist OR can be created (`os.MkdirAll`).
- Reject relative-path traversal in any later operation (`..`, absolute paths in inputs).

### Path conventions (mirror PhotoPrism)

For `OriginalRelPath(takenAt time.Time, filename string) string`:
- Returns `YYYY/MM/<filename>` (e.g. `2024/06/IMG_1234.jpg`).
- If `takenAt.IsZero()` use `unknown/<filename>`.
- Sanitize `filename` (strip path components, replace unsafe chars `[^\w.-]` with `_`).

For `ThumbRelPath(fileHash, sizeName string) string`:
- Returns `<aa>/<bb>/<cc>/<hash>_<size>.jpg` where `aa/bb/cc` are the first 6 hex chars of `fileHash` split into 3 dirs of 2 chars each.
- `sizeName` is one of: `fit_720`, `fit_1280`, `fit_1920`, `fit_2560`, `fit_3840`, `fit_7680`, `tile_50`, `tile_100`, `tile_224`, `tile_500`. Reject anything else.

For absolute paths, expose:
- `AbsOriginal(rel string) string` → `filepath.Join(originalsRoot, rel)`
- `AbsThumb(rel string) string` → `filepath.Join(cacheRoot, "thumb", rel)`

All path-construction functions must call `filepath.Clean` and reject results that escape the root.

### Hashing

```go
func HashFile(path string) (string, error)
func HashReader(r io.Reader) (string, int64, error)  // returns hex hash + bytes read
```

Both use SHA256 and return lowercase hex.

### Atomic write

```go
func (s *Storage) WriteOriginal(relPath string, r io.Reader) (written int64, hash string, err error)
func (s *Storage) WriteThumb(relPath string, r io.Reader) (written int64, err error)
```

Both must:
- Create parent directories (`os.MkdirAll` with `0o755`).
- Write to a temp file (`<dst>.tmp.<random>`) and `os.Rename` on success.
- Compute SHA256 during the write for `WriteOriginal`.
- Use `0o644` for files.
- Clean up the temp file on error.

### Existence/read helpers

```go
func (s *Storage) OriginalExists(rel string) bool
func (s *Storage) ThumbExists(rel string) bool
func (s *Storage) OpenOriginal(rel string) (*os.File, error)
func (s *Storage) OpenThumb(rel string) (*os.File, error)
func (s *Storage) DeleteOriginal(rel string) error
func (s *Storage) DeleteThumb(rel string) error
```

### Tests

`storage_test.go` covers:
- Path construction (originals + thumbs) for normal + unknown date.
- Path traversal rejection (`../etc/passwd`, absolute paths, `\0`).
- Hashing produces stable SHA256.
- Atomic write: temp file removed on writer error, final file matches input.
- Round-trip: write then `OpenOriginal` returns identical bytes.

Use `t.TempDir()` for all filesystem tests — no shared state.

## Verification

- `make test` passes.
- `make lint` and `make build-go` pass.
- New env vars documented in `CLAUDE.md` (under the Configuration section).