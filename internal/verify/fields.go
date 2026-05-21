package verify

import (
	"math"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// MaxFieldDiffsPerField caps the number of FieldDiff entries we record for
// any single (entity, field) bucket. The spec asks for 1000 per field type
// in the JSON output to keep the report sane on large libraries; the
// text formatter further trims to first-50 for human-readable output.
const MaxFieldDiffsPerField = 1000

// MaxFieldDiffsTextSample is the per-entity text rendering cap. The full
// list still lives in the JSON report (up to MaxFieldDiffsPerField per
// field type); this is just the slice we show to a human.
const MaxFieldDiffsTextSample = 50

// Field-level tolerance bands. The verifier accepts up to these
// differences as "equivalent" unless --strict is on.
const (
	// TakenAtToleranceSeconds: PhotoPrism's MySQL DATETIME has 1-second
	// precision; native is microsecond. A 1-second band swallows that.
	TakenAtToleranceSeconds = 1.0

	// LatLngTolerance: PhotoPrism stores doubles, native stores doubles,
	// but PhotoPrism's MariaDB driver occasionally rounds at the 7th
	// decimal place. 1e-6 ≈ 11 cm — safely below "user-visible drift".
	LatLngTolerance = 1e-6

	// AltitudeTolerance: PhotoPrism altitude is INT meters; native is
	// double. Allow 1 meter drift for any rounding.
	AltitudeTolerance = 1.0

	// MarkerScoreTolerance: PhotoPrism stores marker score as SMALLINT
	// (0..100); native mirrors that. Spec asks for ≤ 0.01 drift.
	MarkerScoreTolerance = 0.01
)

// FieldDiff is a single field-level mismatch between PhotoPrism and the
// native store. Key identifies the row on the source side (file_hash for
// photos, slug for albums/labels, normalised name for subjects, or a
// composite for markers). Source/Destination are the rendered string
// representations of the two values.
//
// Both Source and Destination are intentionally strings rather than
// `any`: the report has to be JSON-serializable, and PhotoPrism values
// come back as a mix of []byte, sql.NullX, and primitives. Rendering to
// a string at the point of diff detection keeps the report shape
// boring.
type FieldDiff struct {
	Entity      string `json:"entity"`
	Key         string `json:"key"`
	Field       string `json:"field"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// FieldDiffBucket aggregates field diffs per field name for one entity.
// FieldCounts is the per-field tally before truncation, so the human-
// readable header line ("Photos: 12 missing | 3 field diffs (keywords:
// 2, panorama: 1)") can report accurate totals even when Diffs is
// capped.
type FieldDiffBucket struct {
	// Diffs is the slice of individual mismatches. Capped at
	// MaxFieldDiffsPerField *per field name*, so a single noisy field
	// cannot crowd out diffs from other fields in the JSON report.
	Diffs []FieldDiff `json:"diffs"`
	// FieldCounts maps field name -> total mismatches detected (before
	// truncation). Used for the human-readable header.
	FieldCounts map[string]int `json:"field_counts,omitempty"`
}

// HasDiffs reports whether the bucket recorded any field-level diff.
func (b *FieldDiffBucket) HasDiffs() bool {
	return len(b.Diffs) > 0 || len(b.FieldCounts) > 0
}

// totalDiffs returns the sum of every field's count, useful for the
// header line.
func (b *FieldDiffBucket) totalDiffs() int {
	total := 0
	for _, c := range b.FieldCounts {
		total += c
	}
	return total
}

// fieldDiffCollector accumulates FieldDiff entries with per-field
// truncation. Push() always increments FieldCounts but only appends to
// Diffs while the per-field cap is below MaxFieldDiffsPerField. The
// collector is not safe for concurrent use; each entity owns its own.
type fieldDiffCollector struct {
	entity string
	out    *FieldDiffBucket
	// perField holds the running tally of *appended* (not just counted)
	// diffs per field name, so we can stop appending without losing the
	// total in FieldCounts.
	perField map[string]int
}

// newFieldCollector allocates a collector backed by the supplied bucket.
// The bucket's FieldCounts map is lazily initialised the first time
// Push is called.
func newFieldCollector(entity string, out *FieldDiffBucket) *fieldDiffCollector {
	return &fieldDiffCollector{
		entity:   entity,
		out:      out,
		perField: make(map[string]int),
	}
}

// Push records one field-level mismatch. The total count for the field
// is always bumped; the entry is only appended to Diffs while we are
// still below MaxFieldDiffsPerField for that field name.
func (c *fieldDiffCollector) Push(key, field, src, dst string) {
	if c.out.FieldCounts == nil {
		c.out.FieldCounts = make(map[string]int)
	}
	c.out.FieldCounts[field]++
	if c.perField[field] >= MaxFieldDiffsPerField {
		return
	}
	c.perField[field]++
	c.out.Diffs = append(c.out.Diffs, FieldDiff{
		Entity:      c.entity,
		Key:         key,
		Field:       field,
		Source:      src,
		Destination: dst,
	})
}

// fieldComparer bundles the per-field comparators with the strict-mode
// flag so each call site can be a one-liner. Construct via
// newFieldComparer.
type fieldComparer struct {
	strict bool
}

// newFieldComparer returns a comparer with the given strict-mode
// setting. Strict mode disables tolerance bands (1-second drift on
// dates, 1e-6 drift on coords, 0.01 on marker score) so any deviation
// becomes a diff.
func newFieldComparer(strict bool) *fieldComparer {
	return &fieldComparer{strict: strict}
}

// floatEq reports whether two floats are equal under the given
// tolerance. In strict mode tolerance collapses to 0.
func (c *fieldComparer) floatEq(a, b, tol float64) bool {
	if c.strict {
		return a == b
	}
	return math.Abs(a-b) <= tol
}

// floatPtrEq compares two *float64 with tolerance. nil == nil; one nil
// and one non-nil is unequal.
func (c *fieldComparer) floatPtrEq(a, b *float64, tol float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return c.floatEq(*a, *b, tol)
}

// secondsEq compares two times under the date tolerance band (seconds).
// nil times are considered equal to each other and unequal to anything
// else.
func (c *fieldComparer) secondsEq(a, b *timeLike) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if c.strict {
		return a.unixNano == b.unixNano
	}
	delta := float64(a.unixNano-b.unixNano) / 1e9
	if delta < 0 {
		delta = -delta
	}
	return delta <= TakenAtToleranceSeconds
}

// timeLike wraps a Unix-nanosecond timestamp so we can compare PhotoPrism
// (UTC DATETIME) against native (TIMESTAMP WITH TIME ZONE) without
// passing time.Time around the verifier. nil means "absent".
type timeLike struct {
	unixNano int64
}

// normaliseString trims whitespace and applies Unicode NFC. PhotoPrism's
// MariaDB instance occasionally returns NFD-encoded keywords; the
// native side is NFC. Normalising both before comparison removes the
// false positive without losing real differences.
func normaliseString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return norm.NFC.String(s)
}

// normaliseStringSlice trims, NFC-normalises, dedupes and sorts the
// input. Used by keyword / categories comparisons where order on either
// side is non-canonical.
func normaliseStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		v := normaliseString(s)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// stringSliceEq reports whether two normalised string slices contain
// the same values. Callers must pass slices already routed through
// normaliseStringSlice.
func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// formatStringSlice renders a string slice as "[a, b, c]" for the diff
// report. An empty slice renders as "[]" so the operator can distinguish
// "no keywords" from "missing".
func formatStringSlice(in []string) string {
	if len(in) == 0 {
		return "[]"
	}
	return "[" + strings.Join(in, ", ") + "]"
}
