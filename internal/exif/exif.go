// Package exif extracts EXIF and basic image metadata from photo files.
//
// It prefers the system exiftool binary (which understands JPEG, PNG, HEIC,
// and a long tail of RAW formats) invoked via subprocess, and falls back to
// a pure-Go JPEG parser when exiftool is unavailable or refuses the file.
//
// The package has no dependencies on database, HTTP, or storage code so it
// can be reused from the upload pipeline as well as offline tooling.
package exif

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Metadata captures the subset of EXIF and image-config information the
// photo-sorter needs from an uploaded photo.
//
// A zero TakenAt indicates the date is unknown; TakenAtSource records where
// the date came from ("exif", "filename", or "unknown"). Pointer fields are
// nil when the corresponding EXIF tag is absent. Raw holds the unparsed
// exiftool JSON object so callers can persist it to a jsonb column.
type Metadata struct {
	TakenAt       time.Time
	TakenAtSource string
	Width         int
	Height        int
	Orientation   int
	Mime          string
	Lat, Lng      *float64
	Altitude      *float64
	CameraMake    string
	CameraModel   string
	LensModel     string
	ISO           *int
	Aperture      *float64
	Exposure      string
	FocalLength   *float64
	Raw           map[string]any
}

// Extract reads metadata from the file at path. It first invokes exiftool
// and falls back to the pure-Go JPEG parser for JPEG files when exiftool is
// missing or fails. A non-nil Metadata is always returned on success, even
// if no EXIF data could be recovered (in which case TakenAtSource is
// "unknown" and most fields are zero).
func Extract(ctx context.Context, path string) (*Metadata, error) {
	if path == "" {
		return nil, errors.New("exif: path must not be empty")
	}
	return doExtract(ctx, path, filepath.Base(path))
}

// ExtractFromReader buffers r into a temporary file (cleaned up before
// return) and then runs Extract against that file. The original filename is
// used for the filename-date heuristic, not the temp filename, so callers
// preserve the date even though the on-disk path is randomised.
func ExtractFromReader(ctx context.Context, r io.Reader, filename string) (*Metadata, error) {
	if r == nil {
		return nil, errors.New("exif: reader must not be nil")
	}
	tmp, err := os.CreateTemp("", "exif-*"+filepath.Ext(filename))
	if err != nil {
		return nil, fmt.Errorf("exif: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("exif: buffer to temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("exif: close temp: %w", err)
	}
	return doExtract(ctx, tmpPath, filename)
}

// doExtract is the shared core of Extract and ExtractFromReader. The
// displayName is the original filename presented to callers (used for the
// filename-date heuristic) which may differ from the on-disk path when the
// file was buffered from a reader.
func doExtract(ctx context.Context, path, displayName string) (*Metadata, error) {
	mime, isJPEG, err := detectMime(path)
	if err != nil {
		return nil, err
	}

	md := tryExtract(ctx, path, isJPEG)
	if md.Mime == "" {
		md.Mime = mime
	}
	if md.Orientation == 0 {
		md.Orientation = 1
	}
	if md.TakenAt.IsZero() {
		if t, ok := parseFilenameDate(displayName); ok {
			md.TakenAt = t
			md.TakenAtSource = "filename"
		}
	}
	if md.TakenAtSource == "" {
		md.TakenAtSource = "unknown"
	}
	return md, nil
}

// tryExtract attempts exiftool first; on any failure it falls back to the
// pure-Go JPEG parser if the file is a JPEG. A non-nil (possibly empty)
// Metadata is always returned so doExtract can still fill in mime and
// filename-date defaults.
func tryExtract(ctx context.Context, path string, isJPEG bool) *Metadata {
	if md, err := runExiftool(ctx, path); err == nil {
		return md
	}
	if isJPEG {
		if md, err := jpegFallback(path); err == nil {
			return md
		}
	}
	return &Metadata{}
}
