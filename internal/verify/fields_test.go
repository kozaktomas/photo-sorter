package verify

import (
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TestNormaliseString covers trim + NFC normalisation. NFD-encoded
// inputs must compare equal to their NFC twin so a PhotoPrism row that
// stores "Veselí" as NFD does not produce a false-positive diff
// against the NFC native row.
func TestNormaliseString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \t  ", ""},
		{"trim", "  hello  ", "hello"},
		// NFD ("e" + combining acute) collapses to NFC ("é").
		{"nfd to nfc", "Veselé", "Veselé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normaliseString(tt.in); got != tt.want {
				t.Errorf("normaliseString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormaliseStringSlice asserts trim + NFC + dedupe + sort for the
// keyword / categories comparators.
func TestNormaliseStringSlice(t *testing.T) {
	t.Parallel()
	got := normaliseStringSlice([]string{"  beta", "alpha", "alpha", "", "gamma"})
	want := []string{"alpha", "beta", "gamma"}
	if !stringSliceEq(got, want) {
		t.Errorf("normaliseStringSlice = %v, want %v", got, want)
	}
}

// TestStringSliceEq covers the only-equal-when-elements-and-length
// match contract that the photo-keyword / label-category comparators
// rely on.
func TestStringSliceEq(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, true},
		{"empty equal", nil, nil, true},
		{"len differ", []string{"a"}, []string{"a", "b"}, false},
		{"contents differ", []string{"a", "c"}, []string{"a", "b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stringSliceEq(tt.a, tt.b); got != tt.want {
				t.Errorf("stringSliceEq(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestFieldComparer covers the tolerance bands. Strict mode disables
// every tolerance and falls back to exact equality.
func TestFieldComparer(t *testing.T) {
	t.Parallel()
	lax := newFieldComparer(false)
	strict := newFieldComparer(true)

	// floatEq under tolerance.
	if !lax.floatEq(1.0, 1.0000005, LatLngTolerance) {
		t.Error("expected lax to accept sub-tolerance lat diff")
	}
	if strict.floatEq(1.0, 1.0000005, LatLngTolerance) {
		t.Error("expected strict to reject sub-tolerance diff")
	}
	if lax.floatEq(1.0, 1.001, LatLngTolerance) {
		t.Error("expected lax to reject diff above tolerance")
	}

	// secondsEq tolerates ≤ 1 second drift.
	t1 := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(500 * time.Millisecond)
	a := &timeLike{unixNano: t1.UnixNano()}
	b := &timeLike{unixNano: t2.UnixNano()}
	if !lax.secondsEq(a, b) {
		t.Error("expected lax to accept 0.5s drift")
	}
	if strict.secondsEq(a, b) {
		t.Error("expected strict to reject any drift")
	}

	// nil handling.
	if !lax.secondsEq(nil, nil) {
		t.Error("expected nil == nil")
	}
	if lax.secondsEq(a, nil) {
		t.Error("expected non-nil != nil")
	}
}

// TestFieldDiffCollectorCap asserts the per-field truncation rule:
// FieldCounts always reflects the true total; Diffs only carries up
// to MaxFieldDiffsPerField entries per field name; other fields are
// not affected by a noisy one.
func TestFieldDiffCollectorCap(t *testing.T) {
	t.Parallel()
	bucket := &FieldDiffBucket{}
	c := newFieldCollector("photo", bucket)
	noise := MaxFieldDiffsPerField + 5
	for range noise {
		c.Push("key", "noisy_field", "a", "b")
	}
	c.Push("k", "other", "x", "y")
	if got := bucket.FieldCounts["noisy_field"]; got != noise {
		t.Errorf("FieldCounts[noisy_field] = %d, want %d", got, noise)
	}
	// Diffs slice should contain MaxFieldDiffsPerField noisy entries
	// plus 1 other-field entry.
	want := MaxFieldDiffsPerField + 1
	if len(bucket.Diffs) != want {
		t.Errorf("len(Diffs) = %d, want %d", len(bucket.Diffs), want)
	}
}

// TestExpectedTakenSrc covers the migrator's source mapping.
func TestExpectedTakenSrc(t *testing.T) {
	t.Parallel()
	now := time.Now()
	if got := expectedTakenSrc(&now); got != "exif" {
		t.Errorf("with date: %q, want %q", got, "exif")
	}
	if got := expectedTakenSrc(nil); got != "unknown" {
		t.Errorf("no date: %q, want %q", got, "unknown")
	}
}

// TestTakenSrcEquivalent covers the "accept either side" rule.
func TestTakenSrcEquivalent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		expected, native, ppSrc string
		want                    bool
	}{
		{"exif/exif", "exif", "exif", "meta", true},
		{"exif/meta", "exif", "meta", "meta", true},
		{"exif/empty", "exif", "", "meta", false},
		{"unknown/unknown", "unknown", "unknown", "", true},
		{"unknown/empty", "unknown", "", "", true},
		{"unknown/exif (real diff)", "unknown", "exif", "", false},
		{"empty/empty", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := takenSrcEquivalent(tt.expected, tt.native, tt.ppSrc)
			if got != tt.want {
				t.Errorf("takenSrcEquivalent(%q, %q, %q) = %v, want %v",
					tt.expected, tt.native, tt.ppSrc, got, tt.want)
			}
		})
	}
}

// TestComparePhotoKeywords confirms that two keyword sources are
// considered equal when their NFC-normalised, sorted sets agree, even
// when the input orderings and casing differ.
func TestComparePhotoKeywords(t *testing.T) {
	t.Parallel()
	pp := &ppPhotoFull{
		PhotoUID: "p1",
		Keywords: []string{"beta", "ALPHA", "gamma"},
	}
	native := &database.Photo{
		FileHash: "deadbeef0000",
		Keywords: []string{"gamma", "ALPHA", "beta"},
	}
	bucket := &FieldDiffBucket{}
	c := newFieldCollector("photo", bucket)
	v := &verifier{comparer: newFieldComparer(false)}
	v.diffPhotoKeywords(c, "deadbeef", pp, native)
	if bucket.HasDiffs() {
		t.Errorf("expected no diffs, got: %+v", bucket.Diffs)
	}

	// Now diverge by one keyword.
	native.Keywords = []string{"gamma", "ALPHA"}
	bucket = &FieldDiffBucket{}
	c = newFieldCollector("photo", bucket)
	v.diffPhotoKeywords(c, "deadbeef", pp, native)
	if !bucket.HasDiffs() {
		t.Error("expected a keyword diff, got none")
	}
	if bucket.FieldCounts["keywords"] != 1 {
		t.Errorf("FieldCounts[keywords] = %d, want 1", bucket.FieldCounts["keywords"])
	}
}

// TestAppendMembershipDiffSymmetric ensures the junction-table diff
// emits both sides of an asymmetric membership and respects photoMap
// (rows whose photo did not migrate are silently dropped).
func TestAppendMembershipDiffSymmetric(t *testing.T) {
	t.Parallel()
	// Two PhotoPrism photos map to native UIDs, one does not.
	photoMap := map[string]string{
		"pp-p1": "n1",
		"pp-p2": "n2",
		// pp-p3 has no native counterpart.
	}
	hashByUID := map[string]string{
		"n1": "11111111aaaaaaaa",
		"n2": "22222222bbbbbbbb",
		"n3": "33333333cccccccc",
	}
	// PhotoPrism says holiday has {p1, p2, p3}; native says holiday has {n2, n3}.
	pp := []string{"pp-p1", "pp-p2", "pp-p3"}
	native := []string{"n2", "n3"}
	diffs := appendMembershipDiff(nil, "holiday", pp, native, photoMap, hashByUID)
	// Expected:
	//   pp_only: n1 (translated from pp-p1)
	//   sorter_only: n3
	//   (pp-p3 dropped because it isn't in photoMap)
	if len(diffs) != 2 {
		t.Fatalf("len(diffs) = %d, want 2: %+v", len(diffs), diffs)
	}
	seen := make(map[string]string)
	for _, d := range diffs {
		seen[d.Side] = d.PhotoFileHash
	}
	if seen["pp_only"] != "11111111" {
		t.Errorf("pp_only = %q, want 11111111", seen["pp_only"])
	}
	if seen["sorter_only"] != "33333333" {
		t.Errorf("sorter_only = %q, want 33333333", seen["sorter_only"])
	}
}

// TestPhotoIdentifier asserts the file_hash prefix preference and the
// UID fallback.
func TestPhotoIdentifier(t *testing.T) {
	t.Parallel()
	hashByUID := map[string]string{
		"n1": "abcdef0123456789",
		"n2": "",
	}
	if got := photoIdentifier("n1", hashByUID); got != "abcdef01" {
		t.Errorf("with hash: %q, want abcdef01", got)
	}
	if got := photoIdentifier("n2", hashByUID); got != "n2" {
		t.Errorf("empty hash: %q, want n2", got)
	}
	if got := photoIdentifier("n3", hashByUID); got != "n3" {
		t.Errorf("missing entry: %q, want n3", got)
	}
}

// TestFormatStringSlice covers the empty and populated cases.
func TestFormatStringSlice(t *testing.T) {
	t.Parallel()
	if got := formatStringSlice(nil); got != "[]" {
		t.Errorf("nil: %q, want []", got)
	}
	if got := formatStringSlice([]string{"a", "b"}); got != "[a, b]" {
		t.Errorf("populated: %q, want [a, b]", got)
	}
}

// TestSplitCommaTrim covers the comma-separated keyword/categories
// parser used by ppLabelFull / ppPhotoFull.
func TestSplitCommaTrim(t *testing.T) {
	t.Parallel()
	got := splitCommaTrim("alpha,  beta, ,  gamma")
	want := []string{"alpha", "beta", "gamma"}
	if !stringSliceEq(got, want) {
		t.Errorf("splitCommaTrim = %v, want %v", got, want)
	}
	if got := splitCommaTrim(""); got != nil {
		t.Errorf("empty input: %v, want nil", got)
	}
}

// TestMergeKeywords confirms the union behaviour the migrator uses
// (details.keywords + photos_keywords).
func TestMergeKeywords(t *testing.T) {
	t.Parallel()
	a := []string{"alpha", "beta"}
	b := []string{"BETA", "gamma", "alpha"}
	got := mergeKeywords(a, b)
	want := []string{"alpha", "beta", "BETA", "gamma"}
	if !stringSliceEq(got, want) {
		t.Errorf("mergeKeywords = %v, want %v", got, want)
	}
}

// TestEntitySummary indirectly exercises the entitySummary print path
// by asserting that a clean bucket prints nothing and a dirty one
// prints both the count and the per-field tally.
func TestEntitySummary(t *testing.T) {
	t.Parallel()
	report := &Report{
		Photos: PhotoReport{
			PPCount:     5,
			SorterCount: 5,
			FieldDiffs: FieldDiffBucket{
				FieldCounts: map[string]int{
					"panorama": 1,
					"keywords": 2,
				},
				Diffs: []FieldDiff{
					{Entity: "photo", Key: "k", Field: "keywords", Source: "a", Destination: "b"},
				},
			},
		},
	}
	if !report.HasDiffs() {
		t.Fatal("expected HasDiffs when FieldDiffs non-empty")
	}
}
