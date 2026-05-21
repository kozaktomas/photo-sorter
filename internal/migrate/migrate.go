// Package migrate imports an existing PhotoPrism instance (MariaDB + on-disk
// originals) into photo-sorter's native PostgreSQL schema and storage tree.
//
// The migrator runs one-shot, is idempotent on re-run (photos are skipped by
// file_hash, subjects/labels/albums by name/slug), and supports a dry-run
// mode that walks the source data without copying files or touching the
// destination DB. It is intentionally read-only against PhotoPrism so a live
// instance can be migrated without taking the source offline.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/kozaktomas/photo-sorter/internal/storage"
)

// Stage names. Used by Options.Only to limit which stages run and by the
// summary so the operator can correlate counts to the stage that produced
// them. Stages always execute in this order: subjects → photos → labels →
// albums → markers → thumbnails. Earlier stages populate the photo UID
// mapping that later stages read.
const (
	StageSubjects   = "subjects"
	StagePhotos     = "photos"
	StageLabels     = "labels"
	StageAlbums     = "albums"
	StageMarkers    = "markers"
	StageThumbs     = "thumbs"
	StageThumbsLong = "thumbnails"
)

// stageOrder is the canonical execution order. Iteration over this slice
// (rather than a map) guarantees deterministic output for tests and logs.
var stageOrder = []string{
	StageSubjects,
	StagePhotos,
	StageLabels,
	StageAlbums,
	StageMarkers,
	StageThumbs,
}

// stageAliases lets the operator type either "thumbs" or "thumbnails" on
// the CLI and have them resolve to the same stage.
var stageAliases = map[string]string{
	StageThumbsLong: StageThumbs,
}

// Options bundles the runtime configuration of a migration. The zero value
// is not usable — callers must set MariaDB and OriginalsRoot at minimum.
type Options struct {
	// MariaDB is the open *sql.DB connected to the source PhotoPrism
	// database. The migrator never closes it; the caller owns the
	// lifecycle.
	MariaDB *sql.DB

	// OriginalsRoot is the absolute path on disk where PhotoPrism stores
	// originals. The file_name column in PhotoPrism includes the YYYY/MM
	// prefix, so a single root is enough.
	OriginalsRoot string

	// CacheRoot is the absolute path of PhotoPrism's storage/cache tree.
	// Currently unused — photo-sorter's thumb layout is incompatible so
	// thumbnails are regenerated from the originals — but the flag is
	// reserved for future "copy when layouts match" optimisations.
	CacheRoot string

	// UploaderUID is the native photo-sorter user UID written to every
	// imported photo's uploaded_by column. Empty disables attribution
	// (photos are persisted with NULL uploaded_by).
	UploaderUID string

	// DryRun walks PhotoPrism, prints counts, and exits without writing
	// to the destination DB or the storage tree.
	DryRun bool

	// SkipThumbs disables the thumbnail-regeneration stage. Useful when
	// the operator plans to run the existing `cache compute-phashes` or
	// similar backfill jobs after the migration completes.
	SkipThumbs bool

	// BatchSize is the number of rows the photo stage pulls per query.
	// Zero falls back to defaultBatchSize.
	BatchSize int

	// Concurrency is the number of parallel workers the thumbnail stage
	// uses. Zero falls back to defaultConcurrency. The other stages run
	// sequentially because their bottleneck is the source DB.
	Concurrency int

	// Only restricts which stages run. An empty slice runs every stage.
	// Unknown stage names are validated by Validate() before any work
	// starts.
	Only []string

	// Store is the destination storage layer (originals + cache root).
	// Required unless DryRun is true (and even then it must be set so the
	// migrator can compute destination paths and emit a realistic
	// summary).
	Store *storage.Storage

	// Repositories the migrator writes to. All are required for non-dry-
	// run migrations. The migrator reads each one lazily so a dry-run
	// can supply nils when convenient.
	Photos   database.PhotoWriter
	Subjects database.SubjectWriter
	Labels   database.LabelWriter
	Albums   database.AlbumWriter
	Markers  database.MarkerWriter

	// Writer receives human-readable progress output. Defaults to
	// os.Stdout when nil (set by the cmd layer); tests pass a buffer.
	Writer io.Writer
}

const (
	defaultBatchSize   = 200
	defaultConcurrency = 4
)

// Validate normalises the Options and rejects obvious misconfiguration. It
// does NOT touch the network or the disk — callers that need a richer check
// should follow up with a connectivity probe.
func (o *Options) Validate() error {
	if err := o.validateRequired(); err != nil {
		return err
	}
	o.applyDefaults()
	stages, err := resolveStages(o.Only)
	if err != nil {
		return err
	}
	o.Only = stages
	return nil
}

// validateRequired checks the must-have fields. Repositories are only
// required when DryRun is false — the dry-run path never touches them.
func (o *Options) validateRequired() error {
	if o.MariaDB == nil {
		return errors.New("migrate: Options.MariaDB is required")
	}
	if o.OriginalsRoot == "" {
		return errors.New("migrate: Options.OriginalsRoot is required")
	}
	if o.Store == nil {
		return errors.New("migrate: Options.Store is required")
	}
	if o.DryRun {
		return nil
	}
	if o.Photos == nil || o.Subjects == nil || o.Labels == nil ||
		o.Albums == nil || o.Markers == nil {
		return errors.New("migrate: all repositories are required for non-dry-run")
	}
	return nil
}

// applyDefaults backfills numeric fields whose zero value would behave
// incorrectly. Keeping this isolated from validateRequired keeps both
// functions inside the linter's complexity budget.
func (o *Options) applyDefaults() {
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
	if o.Concurrency <= 0 {
		o.Concurrency = defaultConcurrency
	}
}

// resolveStages canonicalises the user-supplied stage list (resolving the
// "thumbnails" alias) and returns the stages in canonical order. An empty
// input means "all stages". Unknown stage names return an error.
func resolveStages(only []string) ([]string, error) {
	if len(only) == 0 {
		return append([]string(nil), stageOrder...), nil
	}
	set := make(map[string]struct{}, len(only))
	for _, s := range only {
		canonical, ok := canonicalStage(s)
		if !ok {
			return nil, fmt.Errorf("migrate: unknown stage %q", s)
		}
		set[canonical] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for _, s := range stageOrder {
		if _, ok := set[s]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// canonicalStage normalises a user-typed stage name (resolves aliases).
// Returns false when the name does not match any known stage.
func canonicalStage(name string) (string, bool) {
	if canonical, ok := stageAliases[name]; ok {
		return canonical, true
	}
	for _, s := range stageOrder {
		if s == name {
			return s, true
		}
	}
	return "", false
}

// StageSummary records the counters for a single stage.
type StageSummary struct {
	Stage   string
	Read    int
	Created int
	Skipped int
	Failed  int
}

// Report is the aggregated outcome of a migration run.
type Report struct {
	Stages []StageSummary
}

// AppendStage appends or merges a per-stage summary into the report.
func (r *Report) AppendStage(s StageSummary) {
	for i := range r.Stages {
		if r.Stages[i].Stage == s.Stage {
			r.Stages[i].Read += s.Read
			r.Stages[i].Created += s.Created
			r.Stages[i].Skipped += s.Skipped
			r.Stages[i].Failed += s.Failed
			return
		}
	}
	r.Stages = append(r.Stages, s)
}

// Run executes the migration. The returned Report covers every stage that
// ran (even when a later stage errors); the error is the last stage's
// failure or nil. A canceled context aborts cooperatively.
func Run(ctx context.Context, opts *Options) (*Report, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	m := newMigrator(opts)
	return m.run(ctx)
}
