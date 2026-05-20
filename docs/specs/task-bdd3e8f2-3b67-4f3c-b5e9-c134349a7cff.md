# Labels repository + handler refactor

Replace the PhotoPrism-backed label endpoints with native Postgres-backed handlers.

## Requirements

### Files

- `internal/database/types.go` — add `Label` type.
- `internal/database/repository.go` — add `LabelReader` and `LabelWriter` interfaces.
- `internal/database/postgres/labels.go` — new file with implementation.
- `internal/database/postgres/labels_test.go` — tests.
- `internal/web/handlers/labels.go` — rewrite handlers.
- `internal/web/handlers/labels_test.go` — update tests.

### Types

```go
type Label struct {
    UID        string
    Slug       string
    Name       string
    Priority   int
    Favorite   bool
    PhotoCount int  // computed
    CreatedAt, UpdatedAt time.Time
}

type LabelQuery struct {
    MinPhotos int
    Search    string
    SortBy    string // "name" / "-name" / "count" / "-count"
    Limit     int
    Offset    int
}
```

### Interfaces

```go
type LabelReader interface {
    GetLabel(ctx context.Context, uid string) (*Label, error)
    GetLabelBySlug(ctx context.Context, slug string) (*Label, error)
    ListLabels(ctx context.Context, q LabelQuery) ([]Label, error)
    ListLabelsForPhoto(ctx context.Context, photoUID string) ([]Label, error)
}

type LabelWriter interface {
    EnsureLabel(ctx context.Context, name string) (*Label, error) // creates or returns existing
    UpdateLabel(ctx context.Context, l *Label) error
    DeleteLabels(ctx context.Context, uids []string) (deleted int, err error)
    AddPhotoLabel(ctx context.Context, photoUID, labelUID, source string, uncertainty int) error
    RemovePhotoLabel(ctx context.Context, photoUID, labelUID string) error
}
```

UID format: `"l" + 16 random base32 lowercase chars`. Slug derived from name (lowercase, ASCII-folded, spaces→`-`, collision-deduped).

`EnsureLabel` is intended for the AI sort pipeline to upsert labels by name without race conditions — implement as `INSERT ... ON CONFLICT (slug) DO UPDATE SET updated_at = NOW() RETURNING *`.

### Endpoints to rewrite

- `GET /api/v1/labels` → `ListLabels` (query: `q`, `min_photos`, `sort`, `limit`, `offset`)
- `GET /api/v1/labels/{uid}` → `GetLabel`
- `PUT /api/v1/labels/{uid}` → `UpdateLabel` (body: `{ name, priority?, favorite? }` — name change also re-slugs)
- `DELETE /api/v1/labels` → `DeleteLabels` (body: `{ uids: [...] }`)
- `POST /api/v1/photos/batch/labels` → for each photo, call `EnsureLabel` for each new name then `AddPhotoLabel` — used by bulk action bar. Continue on per-row errors.

Keep response JSON shape identical to today (snake_case, photo_count populated). Mirror the existing fields the frontend expects.

### Sort handler integration

The AI sort job (`internal/sorter`) currently calls `pp.AddPhotoLabel`. Update its dependency injection to take a `database.LabelWriter` and use `EnsureLabel` + `AddPhotoLabel` instead. Keep the sort job's public surface the same so tests in `internal/sorter` need only minimal changes.

### Tests

- Create + list + update + delete round-trip.
- EnsureLabel is idempotent and race-safe (two concurrent goroutines calling for the same name produce one row).
- ListLabels supports `min_photos` and the four sort modes.
- AddPhotoLabel is idempotent (re-adding same pair is a no-op via primary key).
- DeleteLabels with mixed valid+invalid UIDs returns the count of actually deleted rows.

## Verification

- `make test` passes.
- `make lint`, `make build-go` pass.
- `grep -n 'pp\.' internal/web/handlers/labels.go` returns nothing.