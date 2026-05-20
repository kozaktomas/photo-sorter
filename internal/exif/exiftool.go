package exif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"
)

// exiftoolTimeout caps a single exiftool invocation. Large RAW files on
// slow disks can take a few seconds; 20s leaves headroom while still
// preventing a runaway subprocess from blocking an upload.
const exiftoolTimeout = 20 * time.Second

// Sentinel errors returned by runExiftool so callers (and the JPEG
// fallback) can distinguish "exiftool not installed" from "exiftool
// rejected the file". They are wrapped with %w so errors.Is works.
var (
	errExiftoolMissing = errors.New("exiftool binary not found in PATH")
	errExiftoolFailed  = errors.New("exiftool returned non-zero exit")
)

// exiftoolMissingOnce ensures we log the "exiftool missing" warning at most
// once for the lifetime of the process — otherwise every uploaded photo on
// a CI machine without exiftool would print the same line.
var exiftoolMissingOnce sync.Once

// runExiftool invokes the system exiftool binary and parses the first
// object of its JSON output into a *Metadata. It returns
// errExiftoolMissing (wrapped) when the binary is not in PATH and
// errExiftoolFailed (wrapped) when the subprocess exits non-zero or
// returns unparseable output.
func runExiftool(ctx context.Context, path string) (*Metadata, error) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		exiftoolMissingOnce.Do(func() {
			log.Printf("exif: exiftool binary not found in PATH; falling back to pure-Go JPEG parser")
		})
		return nil, fmt.Errorf("%w: %w", errExiftoolMissing, err)
	}

	cctx, cancel := context.WithTimeout(ctx, exiftoolTimeout)
	defer cancel()

	// #nosec G204 -- path comes from a validated upload destination; the
	// command and all other arguments are constant literals.
	cmd := exec.CommandContext(cctx, "exiftool",
		"-json",
		"-n",
		"-fast2",
		"-api", "LargeFileSupport=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errExiftoolFailed, err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(out, &arr); err != nil {
		return nil, fmt.Errorf("%w: parse json: %w", errExiftoolFailed, err)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("%w: empty json array", errExiftoolFailed)
	}

	return parseExiftoolJSON(arr[0]), nil
}
