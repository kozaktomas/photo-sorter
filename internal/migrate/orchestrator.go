package migrate

import (
	"context"
	"fmt"
	"io"
	"os"
)

// migrator is the internal handle that ties Options, the photo UID mapping,
// and the running Report together. It is constructed once per Run() call
// and is not safe for concurrent use across migration runs.
type migrator struct {
	opts *Options
	// photoMap maps PhotoPrism photo_uid → native photo UID, populated by
	// the photos stage and consumed by labels/albums/markers. Empty when
	// the photos stage is skipped via --only.
	photoMap map[string]string
	// fileMap maps PhotoPrism file_uid → native photo UID. The markers
	// stage looks markers up by file_uid; we cache the (file_uid →
	// photo_uid → native_uid) chain so we don't re-query MariaDB for
	// every marker.
	fileMap map[string]string
	// newPhotos records native UIDs of photos that were created in this
	// run (as opposed to skipped because they already existed). Markers
	// are only inserted for these — without this guard a re-run would
	// duplicate every marker since the markers table has no natural
	// uniqueness constraint we can rely on.
	newPhotos map[string]bool
	report    *Report
	out       io.Writer
}

func newMigrator(opts *Options) *migrator {
	out := opts.Writer
	if out == nil {
		out = os.Stdout
	}
	return &migrator{
		opts:      opts,
		photoMap:  make(map[string]string),
		fileMap:   make(map[string]string),
		newPhotos: make(map[string]bool),
		report:    &Report{},
		out:       out,
	}
}

// run dispatches each requested stage in canonical order.
func (m *migrator) run(ctx context.Context) (*Report, error) {
	for _, stage := range m.opts.Only {
		if err := ctx.Err(); err != nil {
			return m.report, fmt.Errorf("migrate canceled: %w", err)
		}
		if err := m.runStage(ctx, stage); err != nil {
			return m.report, fmt.Errorf("stage %s: %w", stage, err)
		}
	}
	m.printSummary()
	return m.report, nil
}

// runStage looks up the stage function by name and runs it. The "users"
// stage is intentionally a no-op (see spec — the operator provisions the
// uploader account via the admin CLI before running the migration); we
// keep no entry for it because Validate filters unknown stages.
func (m *migrator) runStage(ctx context.Context, stage string) error {
	fmt.Fprintf(m.out, "\n== Stage: %s ==\n", stage)
	switch stage {
	case StageSubjects:
		return m.stageSubjects(ctx)
	case StagePhotos:
		if err := m.stagePhotos(ctx); err != nil {
			return err
		}
		return m.emitPhotoMap()
	case StageLabels:
		return m.stageLabels(ctx)
	case StageAlbums:
		return m.stageAlbums(ctx)
	case StageMarkers:
		return m.stageMarkers(ctx)
	case StageThumbs:
		if m.opts.SkipThumbs {
			fmt.Fprintln(m.out, "Skipped (--skip-thumbs)")
			return nil
		}
		return m.stageThumbs(ctx)
	default:
		return fmt.Errorf("unhandled stage %q", stage)
	}
}

// emitPhotoMap writes the PhotoPrism→native photo UID mapping to disk
// when Options.EmitPhotoMapPath is set. Failure to write is treated as a
// fatal error: the operator explicitly asked for the file and a half-
// migration with no map leaves them with nothing to feed into
// migrate-remap-references should they need it.
func (m *migrator) emitPhotoMap() error {
	if m.opts.EmitPhotoMapPath == "" {
		return nil
	}
	if err := writePhotoMap(m.opts.EmitPhotoMapPath, m.photoMap, m.fileMap); err != nil {
		return fmt.Errorf("emit photo map: %w", err)
	}
	fmt.Fprintf(m.out, "Photo UID map written to %s (%d photos, %d files)\n",
		m.opts.EmitPhotoMapPath, len(m.photoMap), len(m.fileMap))
	return nil
}

// printSummary writes the aggregated counts. Each stage gets one line; the
// totals row makes it easy to grep for the final outcome.
func (m *migrator) printSummary() {
	fmt.Fprintln(m.out, "\n== Summary ==")
	var totals StageSummary
	totals.Stage = "total"
	for _, s := range m.report.Stages {
		fmt.Fprintf(m.out, "  %-10s read=%-5d created=%-5d skipped=%-5d failed=%d\n",
			s.Stage, s.Read, s.Created, s.Skipped, s.Failed)
		totals.Read += s.Read
		totals.Created += s.Created
		totals.Skipped += s.Skipped
		totals.Failed += s.Failed
	}
	fmt.Fprintf(m.out, "  %-10s read=%-5d created=%-5d skipped=%-5d failed=%d\n",
		totals.Stage, totals.Read, totals.Created, totals.Skipped, totals.Failed)
	if m.opts.DryRun {
		fmt.Fprintln(m.out, "(dry-run — no files copied, no DB writes)")
	}
}
