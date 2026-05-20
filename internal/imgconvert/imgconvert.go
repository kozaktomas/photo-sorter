// Package imgconvert wraps the external decoders (heif-convert, dcraw) that
// turn HEIC/HEIF and RAW originals into an intermediate JPEG so the rest of
// the pipeline — image.Decode, the thumbnail generator, perceptual hashes —
// can handle them with just the JPEG/PNG/WebP decoders.
//
// The package is intentionally narrow: it inspects a file's extension and
// magic bytes, dispatches to the right converter when needed, and otherwise
// returns the input untouched. It does not modify the file, talk to a
// database, or know anything about uploads.
package imgconvert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrConverterMissing is returned (wrapped) when an external converter
// binary required for the input's format is not installed on PATH. Callers
// can errors.Is against this sentinel to decide whether to surface the
// error to the user as "install heif-convert/dcraw" rather than a generic
// failure.
var ErrConverterMissing = errors.New("imgconvert: external converter not installed")

// Format constants returned by DetectFormat. FormatUnknown is used both
// when the extension is unrecognised and when magic bytes fail to identify
// the file in any other category.
const (
	FormatJPEG    = "jpeg"
	FormatPNG     = "png"
	FormatWebP    = "webp"
	FormatHEIC    = "heic"
	FormatRAW     = "raw"
	FormatUnknown = "unknown"
)

// extFormats maps lowercased file extensions (including the leading dot)
// to the format constant returned by DetectFormat. The RAW entries cover
// the major vendors photo-sorter ingests; everything else is reported as
// FormatUnknown.
var extFormats = map[string]string{
	".jpg":  FormatJPEG,
	".jpeg": FormatJPEG,
	".png":  FormatPNG,
	".webp": FormatWebP,
	".heic": FormatHEIC,
	".heif": FormatHEIC,
	".cr2":  FormatRAW,
	".cr3":  FormatRAW,
	".nef":  FormatRAW,
	".arw":  FormatRAW,
	".dng":  FormatRAW,
	".raf":  FormatRAW,
	".orf":  FormatRAW,
	".rw2":  FormatRAW,
	".pef":  FormatRAW,
	".srw":  FormatRAW,
}

// IsSupportedFormat reports whether the upload pipeline can ingest a file
// with this extension. The extension may include or omit the leading dot
// and is case-insensitive.
func IsSupportedFormat(ext string) bool {
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	_, ok := extFormats[strings.ToLower(ext)]
	return ok
}

// DetectFormat returns one of "jpeg", "png", "webp", "heic", "raw", or
// "unknown" for the file at path. The extension is consulted first and
// then verified by magic bytes. When extension and magic disagree, the
// magic-byte result wins for JPEG/PNG/WebP/HEIC. RAW formats are accepted
// on extension alone — every vendor's RAW container has a different
// header, so there is no universal "this is a RAW" magic to match against.
func DetectFormat(path string) string {
	extFmt := formatByExt(path)
	magic := magicFormat(path)
	if magic == FormatUnknown {
		// Magic bytes told us nothing; trust the extension. The caller will
		// get an error from the converter or decoder later if the file is
		// genuinely invalid.
		return extFmt
	}
	if extFmt == FormatRAW {
		// RAW extensions override magic because most RAWs are TIFF-based
		// and magicFormat would never spot the vendor brand from the leading
		// bytes alone.
		return FormatRAW
	}
	if magic == extFmt {
		return extFmt
	}
	// Extension and magic disagree; the magic bytes are authoritative.
	return magic
}

// EnsureDecodable returns a path to a file that image.Decode (with the
// JPEG, PNG, and WebP decoders registered) can handle.
//
// If the input is already JPEG/PNG/WebP, EnsureDecodable returns the input
// path unchanged together with a no-op cleanup function and a nil error.
//
// If the input is HEIC/HEIF or one of the supported RAW formats, the
// matching external converter (heif-convert or dcraw) is invoked to
// produce a temporary JPEG in os.TempDir() with a random name and a
// ".jpg" suffix. The path to that temp file is returned together with a
// cleanup function the caller MUST defer; cleanup removes the temp file
// and is safe to call multiple times.
//
// On error EnsureDecodable returns a nil cleanup — there is no temp file
// to clean up. On any successful return (pass-through or converted) the
// returned cleanup is non-nil, so callers can unconditionally
// `defer cleanup()`.
func EnsureDecodable(ctx context.Context, srcPath string) (string, func(), error) {
	if srcPath == "" {
		return "", nil, errors.New("imgconvert: srcPath must not be empty")
	}
	if _, err := os.Stat(srcPath); err != nil {
		return "", nil, fmt.Errorf("imgconvert: stat %s: %w", filepath.Base(srcPath), err)
	}
	switch DetectFormat(srcPath) {
	case FormatJPEG, FormatPNG, FormatWebP:
		return srcPath, func() {}, nil
	case FormatHEIC:
		return convertHEIC(ctx, srcPath)
	case FormatRAW:
		return convertRAW(ctx, srcPath)
	default:
		return "", nil, fmt.Errorf("imgconvert: unsupported format for %s", filepath.Base(srcPath))
	}
}

// formatByExt looks up the format constant for a path's extension, or
// FormatUnknown if the extension is not in extFormats.
func formatByExt(path string) string {
	if f, ok := extFormats[strings.ToLower(filepath.Ext(path))]; ok {
		return f
	}
	return FormatUnknown
}

// magicFormat inspects the first bytes of the file and classifies it by
// magic bytes. Returns FormatUnknown for files that don't match a format
// we explicitly recognise, including all RAW variants — they have no
// universally-shared header so DetectFormat falls back to the extension.
func magicFormat(path string) string {
	// #nosec G304 -- caller-supplied path is the same file we'll convert; it
	// has already been Stat'ed by EnsureDecodable.
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown
	}
	defer func() { _ = f.Close() }()

	var head [16]byte
	n, _ := f.Read(head[:])
	if n < 4 {
		return FormatUnknown
	}
	return classifyMagic(head[:n])
}

// classifyMagic identifies common image formats from their leading bytes:
//   - JPEG: FF D8 FF (SOI marker followed by an APPn segment).
//   - PNG:  89 50 4E 47 0D 0A 1A 0A.
//   - WebP: "RIFF" .... "WEBP".
//   - HEIC: ISO Base Media file with "ftyp" at offset 4 and one of the
//     HEIC/HEIF major brands.
func classifyMagic(b []byte) string {
	if isJPEGMagic(b) {
		return FormatJPEG
	}
	if isPNGMagic(b) {
		return FormatPNG
	}
	if isWebPMagic(b) {
		return FormatWebP
	}
	if isHEICMagic(b) {
		return FormatHEIC
	}
	return FormatUnknown
}

// isJPEGMagic reports whether b begins with the JPEG SOI marker (FF D8)
// followed by the start of an APPn / DQT / etc. segment marker.
func isJPEGMagic(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

// isPNGMagic reports whether b begins with the 8-byte PNG signature.
func isPNGMagic(b []byte) bool {
	return len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n"
}

// isWebPMagic reports whether b begins with the RIFF/WEBP container head.
func isWebPMagic(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

// isHEICMagic reports whether b is an ISO Base Media file (`ftyp` at
// offset 4) carrying one of the HEIC/HEIF major brands.
func isHEICMagic(b []byte) bool {
	return len(b) >= 12 && string(b[4:8]) == "ftyp" && isHEIFBrand(string(b[8:12]))
}

// isHEIFBrand reports whether brand is one of the ISO Base Media major
// brands that designates a HEIC/HEIF container.
func isHEIFBrand(brand string) bool {
	switch brand {
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
		return true
	}
	return false
}
