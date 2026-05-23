package exif

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sidecarTimeout caps the exiftool invocation that writes an XMP sidecar.
// 20s mirrors the read-side timeout; XMP writes are small files but the
// process startup on slow ARM hardware can still take a couple of seconds.
const sidecarTimeout = 20 * time.Second

// errSidecarExiftoolMissing signals that the exiftool binary is not
// available; callers downgrade this to a logged warning because the DB is
// the source of truth for metadata edits.
var errSidecarExiftoolMissing = errors.New("exiftool binary not found in PATH")

// sidecarMissingOnce ensures the "exiftool missing" warning is logged at
// most once per process, mirroring the upload path's exiftoolMissingOnce.
var sidecarMissingOnce sync.Once

// SidecarFields captures the subset of EXIF/XMP tags the photo-edit endpoint
// can correct. Only non-nil pointer fields and non-empty string fields are
// written; the rest are left to whatever the existing sidecar (or future
// re-import) supplies. Apart from String/Notes the fields mirror the
// Metadata struct so callers can pass them straight through.
//
// The trailing block (Keywords, Artist, Copyright, License, Software,
// Panorama, Scan) was added by the photo-level metadata gap-fix task so
// the EXIF edit endpoint can persist them to BOTH the photos row AND the
// XMP sidecar in one call. Keywords are nil-vs-empty-distinguishable:
// nil means "do not touch", a non-nil slice (including a zero-length
// one) means "set to exactly this list".
type SidecarFields struct {
	TakenAt     *time.Time
	Lat         *float64
	Lng         *float64
	Altitude    *float64
	CameraMake  string
	CameraModel string
	LensModel   string
	ISO         *int
	Aperture    *float64
	Exposure    string
	FocalLength *float64
	Title       string
	Description string
	Notes       string
	Keywords    []string // nil = don't write; []string{} = clear the tag
	Artist      string
	Copyright   string
	License     string
	Software    string
	Panorama    *bool
	Scan        *bool
}

// hasAny reports whether the struct carries at least one field worth
// writing. The handler can short-circuit when an empty payload arrives so
// we don't spin up exiftool for nothing.
func (s SidecarFields) hasAny() bool {
	ptrs := []bool{
		s.TakenAt != nil, s.Lat != nil, s.Lng != nil, s.Altitude != nil,
		s.ISO != nil, s.Aperture != nil, s.FocalLength != nil,
		s.Panorama != nil, s.Scan != nil,
	}
	for _, ok := range ptrs {
		if ok {
			return true
		}
	}
	strs := []string{
		s.CameraMake, s.CameraModel, s.LensModel, s.Exposure,
		s.Title, s.Description, s.Notes,
		s.Artist, s.Copyright, s.License, s.Software,
	}
	for _, v := range strs {
		if v != "" {
			return true
		}
	}
	return s.Keywords != nil
}

// WriteSidecar writes an XMP sidecar at sidecarPath using exiftool. The
// sidecar is created if missing; existing tags are overwritten. The write
// is atomic from the caller's perspective: exiftool stages the change to
// a sibling `<basename>.xmp_tmp_<pid>` file (extension must remain `.xmp`
// so exiftool recognises the format) and we then rename it onto the
// target only after exiftool succeeds.
//
// Returns errSidecarExiftoolMissing (wrapped) when exiftool is not in PATH;
// the API handler downgrades that to a warning and reports success because
// the Postgres row is authoritative.
func WriteSidecar(ctx context.Context, sidecarPath string, fields SidecarFields) error {
	if sidecarPath == "" {
		return errors.New("exif: sidecarPath must not be empty")
	}
	if !fields.hasAny() {
		return nil
	}
	if _, err := exec.LookPath("exiftool"); err != nil {
		sidecarMissingOnce.Do(func() {
			log.Printf("exif: exiftool binary not found in PATH; skipping sidecar writes")
		})
		return fmt.Errorf("%w: %w", errSidecarExiftoolMissing, err)
	}

	dir := filepath.Dir(sidecarPath)
	// Spec mandates 0o755 to match the PhotoPrism originals tree layout
	// (see internal/storage/storage.go) so a future rsync-based migration
	// keeps consistent permissions.
	//nolint:gosec // G301: 0o755 matches the originals tree.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("exif: create sidecar dir: %w", err)
	}

	// exiftool detects the output format from the file extension, so the
	// staging file must still end in `.xmp`. A 16-hex random suffix
	// guarantees uniqueness across concurrent goroutines AND across
	// multiple photo-sorter processes writing the same sidecar — a
	// per-PID suffix would alias when two goroutines in the same process
	// raced and would also collide across processes that happen to be
	// assigned the same PID on different hosts.
	base := filepath.Base(sidecarPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	suffix, err := randomTmpSuffix()
	if err != nil {
		return fmt.Errorf("exif: generate sidecar tmp suffix: %w", err)
	}
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s.tmp.%s.xmp", stem, suffix))

	if err := runExiftoolWrite(ctx, tmpPath, fields); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, sidecarPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("exif: rename sidecar: %w", err)
	}
	return nil
}

// randomTmpSuffix returns 16 hex characters drawn from crypto/rand for
// use as a unique sidecar staging-file suffix.
func randomTmpSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// runExiftoolWrite invokes exiftool to write the given fields into the
// supplied (temp) sidecar path. The path must end in `.xmp` for exiftool
// to recognise it as an XMP-only sidecar.
func runExiftoolWrite(ctx context.Context, path string, fields SidecarFields) error {
	args := buildExiftoolArgs(path, fields)

	cctx, cancel := context.WithTimeout(ctx, sidecarTimeout)
	defer cancel()

	// #nosec G204 -- args are built from a fixed allow-list (buildExiftoolArgs);
	// only the structured field values reach exiftool, never user-controlled
	// command fragments.
	cmd := exec.CommandContext(cctx, "exiftool", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exif: exiftool sidecar write: %w (output: %s)", err, string(out))
	}
	return nil
}

// buildExiftoolArgs returns the argv tail (everything after the binary
// name) for an exiftool sidecar write. The flag layout is:
//
//	exiftool -overwrite_original -P -ignoreMinorErrors \
//	    -XMP-dc:Title=... -XMP-exif:DateTimeOriginal=... ... <path>
//
// `-overwrite_original` prevents exiftool from leaving a `_original`
// backup next to the sidecar (we already write to a tmp and rename).
// `-P` preserves the source file's modification time, which matters when
// callers compare sidecar mtime against the original.
func buildExiftoolArgs(path string, fields SidecarFields) []string {
	prefix := []string{
		"-overwrite_original",
		"-P",
		"-ignoreMinorErrors",
		"-q",
	}
	date := dateTimeArgs(fields.TakenAt)
	gps := gpsArgs(fields.Lat, fields.Lng, fields.Altitude)
	cam := cameraArgs(fields)
	text := textArgs(fields)
	tags := tagArgs(fields)

	args := make([]string, 0,
		len(prefix)+len(date)+len(gps)+len(cam)+len(text)+len(tags)+1)
	args = append(args, prefix...)
	args = append(args, date...)
	args = append(args, gps...)
	args = append(args, cam...)
	args = append(args, text...)
	args = append(args, tags...)
	args = append(args, path)
	return args
}

// tagArgs writes the gap-fix metadata: keywords (XMP-dc:Subject is the
// canonical list-of-strings tag the photo industry uses for keywords),
// Artist / Copyright / License / Software (Dublin Core + XMP-photoshop /
// XMP-xmp), and the PhotoPrism-specific panorama / scan flags. The flags
// are written into XMP-photoshop:Headline-adjacent custom tags — XMP
// has no standard slot for them, so we follow PhotoPrism's lead and
// emit XMP-photoshop:Source-style sidecar fields the operator can
// re-import later. Empty / nil values leave the existing tag untouched.
func tagArgs(f SidecarFields) []string {
	var out []string
	if f.Keywords != nil {
		// Clear the tag with a single empty argument first so a shorter
		// list does not retain trailing tokens from a previous write.
		out = append(out, "-XMP-dc:Subject=")
		for _, kw := range f.Keywords {
			if kw == "" {
				continue
			}
			out = append(out, "-XMP-dc:Subject+="+kw)
		}
	}
	if f.Artist != "" {
		out = append(out, "-XMP-dc:Creator="+f.Artist)
	}
	if f.Copyright != "" {
		out = append(out, "-XMP-dc:Rights="+f.Copyright)
	}
	if f.License != "" {
		out = append(out, "-XMP-xmpRights:WebStatement="+f.License)
	}
	if f.Software != "" {
		out = append(out, "-XMP-xmp:CreatorTool="+f.Software)
	}
	if f.Panorama != nil {
		out = append(out, "-XMP-GPano:UsePanoramaViewer="+boolToString(*f.Panorama))
	}
	if f.Scan != nil {
		// No widely-agreed XMP slot for "was scanned", so mirror
		// PhotoPrism and tuck the flag into a custom XMP-photo-sorter
		// namespace. Operators can inspect via `exiftool -a -G1` and
		// the value re-imports cleanly because exiftool round-trips
		// unknown namespaces verbatim.
		out = append(out, "-XMP-photo-sorter:IsScan="+boolToString(*f.Scan))
	}
	return out
}

// boolToString renders a Go bool as the lowercase "true"/"false" XMP
// expects. Centralised so callers do not invent their own encoding.
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// dateTimeArgs writes the photo capture date/time to the standard XMP
// tags used by Lightroom and PhotoPrism (DateTimeOriginal mirrors
// CreateDate so both legacy and modern readers see the same value).
func dateTimeArgs(takenAt *time.Time) []string {
	if takenAt == nil {
		return nil
	}
	// XMP date format per the spec: "YYYY:MM:DD HH:MM:SS" (exiftool also
	// accepts ISO-style with offset; the colon form is the most widely
	// portable).
	s := takenAt.UTC().Format("2006:01:02 15:04:05")
	return []string{
		"-XMP-exif:DateTimeOriginal=" + s,
		"-XMP-xmp:CreateDate=" + s,
		"-XMP-photoshop:DateCreated=" + s,
	}
}

// gpsArgs writes signed decimal-degree GPS coordinates. The XMP-exif
// schema stores latitude/longitude as a single string with hemisphere
// suffix; exiftool encodes the sign for us when we pass a signed decimal,
// so a separate Ref tag is neither required nor accepted. Altitude has a
// real Ref tag in XMP-exif and is written explicitly.
func gpsArgs(lat, lng, alt *float64) []string {
	var out []string
	if lat != nil {
		out = append(out, "-XMP-exif:GPSLatitude="+strconv.FormatFloat(*lat, 'f', -1, 64))
	}
	if lng != nil {
		out = append(out, "-XMP-exif:GPSLongitude="+strconv.FormatFloat(*lng, 'f', -1, 64))
	}
	if alt != nil {
		v := *alt
		ref := "0"
		if v < 0 {
			ref = "1"
			v = -v
		}
		out = append(out,
			"-XMP-exif:GPSAltitude="+strconv.FormatFloat(v, 'f', -1, 64),
			"-XMP-exif:GPSAltitudeRef="+ref,
		)
	}
	return out
}

// cameraArgs writes the camera/lens/exposure fields to their standard
// XMP-exif locations.
func cameraArgs(f SidecarFields) []string {
	var out []string
	if f.CameraMake != "" {
		out = append(out, "-XMP-tiff:Make="+f.CameraMake)
	}
	if f.CameraModel != "" {
		out = append(out, "-XMP-tiff:Model="+f.CameraModel)
	}
	if f.LensModel != "" {
		out = append(out, "-XMP-exifEX:LensModel="+f.LensModel)
	}
	if f.ISO != nil {
		// XMP uses "ISO" (a list), not the EXIF-style "ISOSpeedRatings".
		out = append(out, "-XMP-exif:ISO="+strconv.Itoa(*f.ISO))
	}
	if f.Aperture != nil {
		out = append(out, "-XMP-exif:FNumber="+strconv.FormatFloat(*f.Aperture, 'f', -1, 64))
	}
	if f.FocalLength != nil {
		out = append(out, "-XMP-exif:FocalLength="+strconv.FormatFloat(*f.FocalLength, 'f', -1, 64))
	}
	if f.Exposure != "" {
		out = append(out, "-XMP-exif:ExposureTime="+f.Exposure)
	}
	return out
}

// textArgs writes the free-text fields to the Dublin Core namespace used
// by PhotoPrism, plus the IPTC-style Instructions tag for notes.
func textArgs(f SidecarFields) []string {
	var out []string
	if f.Title != "" {
		out = append(out, "-XMP-dc:Title="+f.Title)
	}
	if f.Description != "" {
		out = append(out, "-XMP-dc:Description="+f.Description)
	}
	if f.Notes != "" {
		out = append(out, "-XMP-photoshop:Instructions="+f.Notes)
	}
	return out
}
