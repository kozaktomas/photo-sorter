package exif

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // image.DecodeConfig needs the JPEG decoder registered
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// detectMime inspects the first 512 bytes of the file to classify it. The
// isJPEG flag is reported separately because only JPEG files are eligible
// for the pure-Go fallback (goexif does not understand HEIC/RAW headers).
func detectMime(path string) (mime string, isJPEG bool, err error) {
	// #nosec G304 -- callers (Extract / ExtractFromReader) provide either a
	// validated upload destination or a temp file owned by this package.
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("exif: open %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	var head [512]byte
	n, _ := f.Read(head[:])
	if n == 0 {
		return "application/octet-stream", false, nil
	}
	mime, isJPEG = classifyMagicBytes(head[:n])
	return mime, isJPEG, nil
}

// classifyMagicBytes identifies common image MIME types from the first
// few bytes of a file. Extracted from detectMime to keep the per-function
// complexity low; this is the format-specific decision table.
func classifyMagicBytes(head []byte) (mime string, isJPEG bool) {
	n := len(head)
	if n >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF {
		return "image/jpeg", true
	}
	if n >= 12 && string(head[4:8]) == "ftyp" {
		if m := heifMimeFromBrand(string(head[8:12])); m != "" {
			return m, false
		}
	}
	return http.DetectContentType(head), false
}

// heifMimeFromBrand maps an ISO Base Media major-brand to the MIME type
// the upload pipeline expects. An empty return means "not a HEIF family
// container we recognise" — caller falls back to http.DetectContentType.
func heifMimeFromBrand(brand string) string {
	switch brand {
	case "heic", "heix", "hevc", "hevx", "heim", "heis":
		return "image/heic"
	case "mif1", "msf1":
		return "image/heif"
	}
	return ""
}

// parseExiftoolJSON converts a single exiftool JSON object (as parsed
// from its -json output with -n) into a *Metadata. Tags that are missing
// or have unexpected types are silently ignored so the result is always
// usable, even on partial/sparse EXIF.
func parseExiftoolJSON(raw map[string]any) *Metadata {
	md := &Metadata{Raw: raw}

	if s, ok := raw["MIMEType"].(string); ok {
		md.Mime = s
	}

	if t, ok := firstExifDate(raw, "DateTimeOriginal", "CreateDate", "DateTimeDigitized", "ModifyDate"); ok {
		md.TakenAt = t
		md.TakenAtSource = "exif"
	}

	md.Width = firstInt(raw, "ImageWidth", "ExifImageWidth")
	md.Height = firstInt(raw, "ImageHeight", "ExifImageHeight")
	md.Orientation = firstInt(raw, "Orientation")

	md.Lat = parseLatLng(raw, "GPSLatitude", "GPSLatitudeRef", "S")
	md.Lng = parseLatLng(raw, "GPSLongitude", "GPSLongitudeRef", "W")
	if v, ok := toFloat(raw["GPSAltitude"]); ok {
		if r, _ := toFloat(raw["GPSAltitudeRef"]); r == 1 {
			v = -v
		}
		md.Altitude = &v
	}

	md.CameraMake = strings.TrimSpace(stringOr(raw, "Make"))
	md.CameraModel = strings.TrimSpace(stringOr(raw, "Model"))
	md.LensModel = strings.TrimSpace(firstString(raw, "LensModel", "Lens", "LensID"))

	if v, ok := toInt(raw["ISO"]); ok {
		md.ISO = &v
	}
	if v, ok := toFloat(raw["FNumber"]); ok {
		md.Aperture = &v
	}
	if v, ok := toFloat(raw["FocalLength"]); ok {
		md.FocalLength = &v
	}
	if v, ok := raw["ExposureTime"]; ok {
		md.Exposure = formatExposure(v)
	}

	return md
}

// parseLatLng pulls a signed decimal degree value out of exiftool JSON.
// With `-n`, exiftool already applies the sign for most builds; the
// negRef argument lets us defensively flip the sign when an older build
// returns the magnitude with a separate "S" or "W" reference field.
func parseLatLng(raw map[string]any, key, refKey, negRef string) *float64 {
	f, ok := toFloat(raw[key])
	if !ok {
		return nil
	}
	if f > 0 {
		if r, _ := raw[refKey].(string); r == negRef {
			f = -f
		}
	}
	return &f
}

// formatExposure normalises an EXIF ExposureTime to a printable string.
// With exiftool's -n flag the value is a numeric like 0.004 (= 1/250);
// without -n we may instead get the raw "1/250" string. Both inputs map
// to the same Exposure field.
func formatExposure(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	f, ok := toFloat(v)
	if !ok || f <= 0 {
		return ""
	}
	if f >= 1 {
		return strconv.FormatFloat(f, 'f', -1, 64) + "s"
	}
	denom := int(math.Round(1.0 / f))
	if denom <= 0 {
		return ""
	}
	return fmt.Sprintf("1/%d", denom)
}

// firstExifDate parses the first parseable EXIF-style date string found
// under any of the supplied keys, in order. EXIF dates are written as
// "YYYY:MM:DD HH:MM:SS"; we also accept a handful of common variants
// (sub-second precision, trailing zone offset).
func firstExifDate(raw map[string]any, keys ...string) (time.Time, bool) {
	for _, k := range keys {
		s, _ := raw[k].(string)
		if s == "" {
			continue
		}
		if t, ok := parseExifDate(s); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// exifDateLayouts enumerates the date formats exiftool emits across
// versions and source files. They are tried in order; the first one to
// parse wins. The bare "YYYY:MM:DD" form is treated as midnight local
// time so very old scans without HMS are still usable.
var exifDateLayouts = []string{
	"2006:01:02 15:04:05",
	"2006:01:02 15:04:05.000",
	"2006:01:02 15:04:05-07:00",
	"2006:01:02 15:04:05.000-07:00",
	"2006:01:02 15:04:05Z07:00",
	"2006:01:02",
}

// parseExifDate attempts each known exif date format in turn.
func parseExifDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range exifDateLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// filenamePatterns enumerates date-bearing filename conventions we know
// about. Each entry is matched against the bare base filename; the named
// capture groups are concatenated with a space and parsed with the
// matching layout in the user's local timezone.
var filenamePatterns = []struct {
	re     *regexp.Regexp
	layout string
}{
	// Generic camera dump (Android, many DSLRs):
	// IMG_20210521_124312.jpg, IMG_20210521_124312_001.jpg
	{regexp.MustCompile(`(?i)^IMG_(\d{8})_(\d{6})`), "20060102 150405"},
	// Pixel phones: PXL_20231005_173045123.MP.jpg.
	{regexp.MustCompile(`(?i)^PXL_(\d{8})_(\d{6})`), "20060102 150405"},
	// Mac/Cloud exports: "2024-05-21 14.32.10.jpg".
	{regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) (\d{2}\.\d{2}\.\d{2})`), "2006-01-02 15.04.05"},
	// macOS screenshots: "Screenshot 2024-05-21 at 14.32.10.png".
	{regexp.MustCompile(`(?i)^Screenshot (\d{4}-\d{2}-\d{2}) at (\d{2}\.\d{2}\.\d{2})`), "2006-01-02 15.04.05"},
}

// parseFilenameDate runs the filename heuristic for known camera/phone
// naming schemes. It returns the parsed time in local timezone and true
// on a hit, or the zero time and false otherwise.
func parseFilenameDate(name string) (time.Time, bool) {
	base := filepath.Base(name)
	for _, p := range filenamePatterns {
		m := p.re.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		// Submatches 1..n joined by a space matches the space in the layout.
		ts := strings.Join(m[1:], " ")
		if t, err := time.ParseInLocation(p.layout, ts, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// jpegFallback parses a JPEG file using the pure-Go goexif library. It
// is intentionally narrow: only the basic EXIF fields and the image
// dimensions are recovered. RAW/HEIC files never reach this path.
func jpegFallback(path string) (*Metadata, error) {
	// #nosec G304 -- see detectMime; callers always supply a path
	// validated by Extract / a temp file owned by ExtractFromReader.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("exif: open jpeg fallback: %w", err)
	}
	defer func() { _ = f.Close() }()

	md := &Metadata{Mime: "image/jpeg"}

	if cfg, _, err := image.DecodeConfig(f); err == nil {
		md.Width = cfg.Width
		md.Height = cfg.Height
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return md, nil
	}

	x, err := exif.Decode(f)
	if err != nil {
		// EXIF parse can fail on a valid JPEG (e.g. EXIF stripped); the
		// width/height we already populated above are still useful.
		return md, nil
	}
	populateFromGoexif(md, x)
	return md, nil
}

// populateFromGoexif copies all fields the pure-Go fallback can recover
// from a decoded *exif.Exif into md. Split out of jpegFallback to keep
// per-function cognitive complexity within the linter budget.
func populateFromGoexif(md *Metadata, x *exif.Exif) {
	if t, err := x.DateTime(); err == nil {
		md.TakenAt = t
		md.TakenAtSource = "exif"
	}
	if v, ok := exifIntTag(x, exif.Orientation); ok {
		md.Orientation = v
	}
	if lat, lng, err := x.LatLong(); err == nil {
		md.Lat, md.Lng = &lat, &lng
	}
	md.CameraMake = strings.TrimSpace(exifStringTag(x, exif.Make))
	md.CameraModel = strings.TrimSpace(exifStringTag(x, exif.Model))
	md.LensModel = strings.TrimSpace(exifStringTag(x, exif.LensModel))
	if v, ok := exifIntTag(x, exif.ISOSpeedRatings); ok {
		md.ISO = &v
	}
	if v, ok := exifRatTag(x, exif.FNumber); ok {
		md.Aperture = &v
	}
	if v, ok := exifRatTag(x, exif.FocalLength); ok {
		md.FocalLength = &v
	}
	md.Exposure = goexifExposure(x)
	if alt, ok := goexifAltitude(x); ok {
		md.Altitude = &alt
	}
}

// goexifExposure returns the ExposureTime tag formatted as a fraction
// string ("1/250") or an empty string if the tag is missing.
func goexifExposure(x *exif.Exif) string {
	num, den, ok := exifRatRaw(x, exif.ExposureTime)
	if !ok || den == 0 {
		return ""
	}
	if num == 1 {
		return fmt.Sprintf("1/%d", den)
	}
	return fmt.Sprintf("%d/%d", num, den)
}

// goexifAltitude applies the GPSAltitudeRef sign convention (0 = above
// sea level, 1 = below) to the raw GPSAltitude value.
func goexifAltitude(x *exif.Exif) (float64, bool) {
	v, ok := exifRatTag(x, exif.GPSAltitude)
	if !ok {
		return 0, false
	}
	if r, _ := exifIntTag(x, exif.GPSAltitudeRef); r == 1 {
		v = -v
	}
	return v, true
}

// exifStringTag returns the string value of tag, or "" if absent.
func exifStringTag(x *exif.Exif, name exif.FieldName) string {
	tag, err := x.Get(name)
	if err != nil {
		return ""
	}
	s, err := tag.StringVal()
	if err != nil {
		return ""
	}
	return s
}

// exifIntTag returns the first integer of tag, or false if absent.
func exifIntTag(x *exif.Exif, name exif.FieldName) (int, bool) {
	tag, err := x.Get(name)
	if err != nil {
		return 0, false
	}
	v, err := tag.Int(0)
	if err != nil {
		return 0, false
	}
	return v, true
}

// exifRatTag returns the first rational of tag as a float, or false if
// absent / undivisible.
func exifRatTag(x *exif.Exif, name exif.FieldName) (float64, bool) {
	num, den, ok := exifRatRaw(x, name)
	if !ok || den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}

// exifRatRaw returns the raw numerator/denominator pair so callers that
// care about the fraction representation (e.g. "1/250") can format
// without losing precision.
func exifRatRaw(x *exif.Exif, name exif.FieldName) (num, den int64, ok bool) {
	tag, err := x.Get(name)
	if err != nil {
		return 0, 0, false
	}
	n, d, err := tag.Rat2(0)
	if err != nil {
		return 0, 0, false
	}
	return n, d, true
}

// stringOr returns the first key's string value, or "".
func stringOr(raw map[string]any, key string) string {
	s, _ := raw[key].(string)
	return s
}

// firstString returns the first non-empty string value across the given
// keys, or "".
func firstString(raw map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := raw[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// firstInt returns the first numeric-coercible value across the given keys
// as an int. Missing keys and non-numeric values are skipped.
func firstInt(raw map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := toInt(raw[k]); ok {
			return v
		}
	}
	return 0
}

// toFloat coerces an arbitrary JSON value to float64. exiftool's -n mode
// emits numbers as JSON numbers (float64 in Go's encoding/json), but
// embedded strings still appear for date and reference fields; this
// helper accepts both so callers don't need a type switch each time.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// toInt coerces an arbitrary JSON value to int, truncating floats.
func toInt(v any) (int, bool) {
	if f, ok := toFloat(v); ok {
		return int(f), true
	}
	return 0, false
}
