package verify

import (
	"context"
	"fmt"
	"io"
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
	report  *Report
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
func (v *verifier) run(ctx context.Context) error {
	v.photoMap = make(map[string]string)
	v.fileMap = make(map[string]string)

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
