package imgconvert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// heifTimeout caps a single heif-convert invocation. The largest HEIC
// originals decode in a few seconds; 30 s is a generous upper bound that
// still prevents a runaway subprocess from blocking an upload indefinitely.
const heifTimeout = 30 * time.Second

// heifQuality is the JPEG quality (1-100) passed to heif-convert. 92
// matches the visual quality of the fit_3840 thumbnail tier so callers see
// no perceptible loss after the convert step.
const heifQuality = 92

// convertHEIC runs heif-convert against srcPath and returns the path of a
// freshly written temporary JPEG plus a once-only cleanup function. The
// temp file lives under os.TempDir() with a random name and a ".jpg"
// suffix so downstream decoders see the right extension.
//
// If heif-convert is not on PATH the returned error wraps
// ErrConverterMissing.
func convertHEIC(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath("heif-convert"); err != nil {
		return "", nil, fmt.Errorf("%w: heif-convert lookup: %w", ErrConverterMissing, err)
	}

	tmpPath, cleanup, err := createTempJPEG("imgconvert-heic-*.jpg")
	if err != nil {
		return "", nil, err
	}

	cctx, cancel := context.WithTimeout(ctx, heifTimeout)
	defer cancel()

	// #nosec G204 -- srcPath is the same caller-supplied path EnsureDecodable
	// stat'ed before dispatch; the other args are constant literals.
	cmd := exec.CommandContext(cctx, "heif-convert",
		"-q", strconv.Itoa(heifQuality),
		srcPath, tmpPath,
	)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: heif-convert %s: %w (output: %s)",
			filepath.Base(srcPath), runErr, string(out))
	}

	info, statErr := os.Stat(tmpPath)
	if statErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: stat heif-convert output: %w", statErr)
	}
	if info.Size() == 0 {
		cleanup()
		return "", nil, errors.New("imgconvert: heif-convert produced empty output")
	}
	return tmpPath, cleanup, nil
}

// createTempJPEG creates a new empty temporary file with the supplied
// pattern under os.TempDir() and immediately closes it so an external
// process can open and write to it. It returns the absolute path plus a
// once-only cleanup function; if file creation succeeds but Close fails
// the partial file is removed before the error is returned.
func createTempJPEG(pattern string) (string, func(), error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("imgconvert: create temp jpeg: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := onceRemove(tmpPath)
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: close temp jpeg: %w", closeErr)
	}
	return tmpPath, cleanup, nil
}

// onceRemove returns a cleanup function that os.Removes path on first
// invocation and is a no-op on every subsequent call. The returned closure
// is therefore safe to call multiple times, satisfying the public cleanup
// contract on EnsureDecodable.
func onceRemove(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { _ = os.Remove(path) })
	}
}
