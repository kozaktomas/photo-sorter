package verify

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// TestFormatJSON_clean asserts a clean report serialises into the spec'd
// shape and contains every top-level section.
func TestFormatJSON_clean(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := &Report{
		Photos:   PhotoReport{PPCount: 1, SorterCount: 1},
		Albums:   AlbumReport{PPCount: 0, SorterCount: 0},
		Labels:   LabelReport{PPCount: 0, SorterCount: 0},
		Subjects: SubjectReport{PPCount: 0, SorterCount: 0},
		Markers:  MarkerReport{PPCount: 0, SorterCount: 0},
		Disk:     DiskReport{},
	}
	if err := FormatJSON(&buf, r); err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"photos", "albums", "labels", "subjects", "markers", "disk"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing top-level key %q in JSON output", key)
		}
	}
}

// TestFormatText_summary toggles the "OK" vs "FAIL" trailer based on
// HasDiffs.
func TestFormatText_summary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		report  *Report
		wantSub string
	}{
		{
			name:    "clean report says OK",
			report:  &Report{},
			wantSub: "OK",
		},
		{
			name: "dirty photos report says FAIL",
			report: &Report{
				Photos: PhotoReport{MissingInSorter: []string{"p001"}},
			},
			wantSub: "FAIL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			FormatText(&buf, tt.report, false)
			if !strings.Contains(buf.String(), tt.wantSub) {
				t.Errorf("output missing %q:\n%s", tt.wantSub, buf.String())
			}
		})
	}
}

// TestHasDiffs covers every section flagging path so a future copy-paste
// mistake (missed branch) is caught.
func TestHasDiffs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		r    Report
		want bool
	}{
		{"empty", Report{}, false},
		{"photo missing", Report{Photos: PhotoReport{MissingInSorter: []string{"x"}}}, true},
		{"photo orphan", Report{Photos: PhotoReport{OrphanInSorter: []string{"x"}}}, true},
		{"album slug mismatch", Report{Albums: AlbumReport{
			SlugTitleMismatch: []AlbumDiff{{Slug: "x"}},
		}}, true},
		{"album missing", Report{Albums: AlbumReport{MissingInSorter: []string{"x"}}}, true},
		{"album orphan", Report{Albums: AlbumReport{OrphanInSorter: []string{"x"}}}, true},
		{"album photo diff", Report{Albums: AlbumReport{
			PhotoDiffs: []AlbumPhoto{{Slug: "x", PPCount: 1}},
		}}, true},
		{"label slug mismatch", Report{Labels: LabelReport{
			SlugNameMismatch: []LabelDiff{{Slug: "x"}},
		}}, true},
		{"label missing", Report{Labels: LabelReport{MissingInSorter: []string{"x"}}}, true},
		{"label orphan", Report{Labels: LabelReport{OrphanInSorter: []string{"x"}}}, true},
		{"label pair diff", Report{Labels: LabelReport{
			PhotoPairDiffs: []LabelPairDiff{{Slug: "x"}},
		}}, true},
		{"subject missing", Report{Subjects: SubjectReport{MissingInSorter: []string{"x"}}}, true},
		{"subject orphan", Report{Subjects: SubjectReport{OrphanInSorter: []string{"x"}}}, true},
		{"marker count diff", Report{Markers: MarkerReport{
			CountDiffs: []MarkerCountDiff{{SubjectName: "x"}},
		}}, true},
		{"marker geometry diff", Report{Markers: MarkerReport{
			GeometryDiffs: []MarkerGeometryDiff{{PPMarkerUID: "x"}},
		}}, true},
		{"disk orphan", Report{Disk: DiskReport{OrphanFiles: []string{"x"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.r.HasDiffs(); got != tt.want {
				t.Errorf("HasDiffs() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTruncate caps the report list at MaxItemsPerCategory and leaves
// shorter inputs unchanged.
func TestTruncate(t *testing.T) {
	t.Parallel()
	short := []string{"a", "b"}
	if got := truncate(short); len(got) != 2 {
		t.Errorf("truncate(short) length = %d, want 2", len(got))
	}
	long := make([]string, MaxItemsPerCategory+10)
	for i := range long {
		long[i] = "x"
	}
	if got := truncate(long); len(got) != MaxItemsPerCategory {
		t.Errorf("truncate(long) length = %d, want %d", len(got), MaxItemsPerCategory)
	}
}

// TestGeometryWithinTolerance covers the three cases that matter: a
// pair within tolerance on every axis, a pair just outside tolerance,
// and a pair where only one axis drifts.
func TestGeometryWithinTolerance(t *testing.T) {
	t.Parallel()
	pp := ppMarkerRow{X: 0.25, Y: 0.30, W: 0.20, H: 0.25}
	tests := []struct {
		name string
		s    database.Marker
		want bool
	}{
		{
			name: "identical",
			s:    database.Marker{X: 0.25, Y: 0.30, W: 0.20, H: 0.25},
			want: true,
		},
		{
			name: "within tolerance",
			s:    database.Marker{X: 0.255, Y: 0.305, W: 0.205, H: 0.255},
			want: true,
		},
		{
			name: "x outside tolerance",
			s:    database.Marker{X: 0.27, Y: 0.30, W: 0.20, H: 0.25},
			want: false,
		},
		{
			name: "h outside tolerance",
			s:    database.Marker{X: 0.25, Y: 0.30, W: 0.20, H: 0.30},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := geometryWithinTolerance(pp, tt.s); got != tt.want {
				t.Errorf("geometryWithinTolerance(%+v, %+v) = %v, want %v",
					pp, tt.s, got, tt.want)
			}
		})
	}
}

// TestBBoxIoU covers identical, overlapping, and disjoint boxes.
func TestBBoxIoU(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b [4]float64
		want float64
	}{
		{
			name: "identical",
			a:    [4]float64{0, 0, 1, 1},
			b:    [4]float64{0, 0, 1, 1},
			want: 1.0,
		},
		{
			name: "disjoint",
			a:    [4]float64{0, 0, 1, 1},
			b:    [4]float64{5, 5, 1, 1},
			want: 0.0,
		},
		{
			name: "half overlap",
			a:    [4]float64{0, 0, 2, 2},
			b:    [4]float64{1, 0, 2, 2},
			want: 1.0 / 3.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := bboxIoU(tt.a[0], tt.a[1], tt.a[2], tt.a[3], tt.b[0], tt.b[1], tt.b[2], tt.b[3])
			if absf(got-tt.want) > 1e-6 {
				t.Errorf("bboxIoU = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCollectNativePhotosForMarkers asserts the union behaviour and
// deterministic sort.
func TestCollectNativePhotosForMarkers(t *testing.T) {
	t.Parallel()
	photoMap := map[string]string{
		"p1": "n2",
		"p2": "n1",
	}
	ppMarkers := map[string][]ppMarkerRow{
		"n1": {{MarkerUID: "m1"}},
		"n3": {{MarkerUID: "m2"}},
	}
	got := collectNativePhotosForMarkers(photoMap, ppMarkers)
	want := []string{"n1", "n2", "n3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, uid := range want {
		if got[i] != uid {
			t.Errorf("[%d] = %q, want %q", i, got[i], uid)
		}
	}
}

// absf is a tiny float64 |x| helper used only by tests.
func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestBuildMarkerCountDiffs covers the matching cases the verifier
// emits a diff for and the no-op case.
func TestBuildMarkerCountDiffs(t *testing.T) {
	t.Parallel()
	subjects := []ppSubjectRow{
		{UID: "s1", Name: "Alice"},
		{UID: "s2", Name: "Bob"},
	}
	subjectMap := map[string]string{
		"s1": "n1",
		"s2": "n2",
	}
	ppCount := map[string]int{
		"n1": 3,
		"n2": 1,
	}
	sorterCount := map[string]int{
		"n1": 3, // match
		"n2": 2, // diff
	}
	diffs := buildMarkerCountDiffs(subjects, subjectMap, ppCount, sorterCount)
	if len(diffs) != 1 {
		t.Fatalf("diffs = %d, want 1", len(diffs))
	}
	if diffs[0].SubjectName != "Bob" {
		t.Errorf("diff subject = %q, want Bob", diffs[0].SubjectName)
	}
	if diffs[0].PPCount != 1 || diffs[0].SorterCount != 2 {
		t.Errorf("diff counts = (%d, %d), want (1, 2)",
			diffs[0].PPCount, diffs[0].SorterCount)
	}
}
