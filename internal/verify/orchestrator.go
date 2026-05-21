package verify

import (
	"context"
	"fmt"
	"io"

	"golang.org/x/sync/errgroup"
)

// verifier ties Options and a running Report together. It is constructed
// once per Run() call and is not safe for concurrent use across runs.
type verifier struct {
	opts *Options
	// photoMap maps PhotoPrism photo_uid → native photo UID. The photos
	// section populates it; albums/labels/markers consume it. A photo
	// missing from the map means the migration did not import it.
	photoMap map[string]string
	// fileMap maps PhotoPrism file_uid → native photo UID. The markers
	// section uses this to walk file_uid → photo_uid → native UID.
	fileMap map[string]string
	// nativeHashByPhotoUID caches the file_hash for each native photo
	// UID seen during photo matching. Field-diff helpers use it to print
	// human-readable identifiers (the operator recognises file_hash[:8]
	// faster than an opaque 22-char UID) and the junction diff uses it
	// to report missing pairs by hash. Populated by the photos phase
	// (or by ensurePhotoMap when --fields-only is set).
	nativeHashByPhotoUID map[string]string
	// comparer is the shared tolerance-aware field comparer. Built once
	// from Options.Strict.
	comparer *fieldComparer
	report   *Report
}

// progress writes a one-line section header to Options.Writer when set.
// nil Writer disables the chatter so JSON callers don't end up with
// stray bytes on stdout.
func (v *verifier) progress(format string, args ...any) {
	w := writer(v.opts.Writer)
	fmt.Fprintf(w, format, args...)
}

// writer returns Options.Writer when set, falling back to io.Discard so
// the verifier never accidentally prints to stdout.
func writer(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// run executes every verification section in order. Each section is
// independent and writes to its own slot in the report, so a later
// section's failure still leaves the earlier ones populated.
//
// Structural phase: photos / albums / labels / subjects-and-markers /
// disk — the original verify behaviour. The photos phase populates
// photoMap + fileMap which every later section consumes.
//
// Field-diff phase: per-entity column-level comparison. Runs after the
// structural phase so the field diff for a missing row is not also
// emitted (a row reported in MissingInSorter is skipped here). When
// Options.FieldsOnly is true the structural phase is replaced with a
// lightweight identity-mapping pass (ensurePhotoMap) that just builds
// the (PhotoPrism photo_uid → native photo UID) map by hash without
// running the orphan/disk walks.
func (v *verifier) run(ctx context.Context) error {
	v.photoMap = make(map[string]string)
	v.fileMap = make(map[string]string)
	v.nativeHashByPhotoUID = make(map[string]string)
	v.comparer = newFieldComparer(v.opts.Strict)

	if v.opts.FieldsOnly {
		v.progress("== identity-mapping ==\n")
		if err := v.ensurePhotoMap(ctx); err != nil {
			return fmt.Errorf("identity-mapping: %w", err)
		}
	} else {
		if err := v.runStructural(ctx); err != nil {
			return err
		}
	}

	v.progress("== field-diff ==\n")
	if err := v.runFieldDiff(ctx); err != nil {
		return fmt.Errorf("field-diff: %w", err)
	}
	return nil
}

// runStructural executes the original existence + count + geometry
// diff phases (photos, albums, labels, subjects-and-markers, disk).
func (v *verifier) runStructural(ctx context.Context) error {
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"photos", v.runPhotos},
		{"albums", v.runAlbums},
		{"labels", v.runLabels},
		{"subjects-and-markers", v.runSubjectsAndMarkers},
		{"disk", v.runDisk},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verify canceled: %w", err)
		}
		v.progress("== %s ==\n", step.name)
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

// runFieldDiff runs every per-entity field comparison concurrently. The
// errgroup caps DB pressure at 4 active goroutines per the spec, but
// since there are only 5 entity sections we just run them all in
// parallel and let the Postgres pool throttle. Each section is
// responsible for skipping rows that the structural phase already
// flagged as missing.
func (v *verifier) runFieldDiff(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	g.Go(func() error { return v.diffPhotoFields(gctx) })
	g.Go(func() error { return v.diffSubjectFields(gctx) })
	g.Go(func() error { return v.diffLabelFields(gctx) })
	g.Go(func() error { return v.diffAlbumFields(gctx) })
	g.Go(func() error { return v.diffMarkerFields(gctx) })
	g.Go(func() error { return v.diffJunctionTables(gctx) })
	if err := g.Wait(); err != nil {
		return fmt.Errorf("errgroup: %w", err)
	}
	return nil
}
