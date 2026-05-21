package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ANSI colour escapes used by FormatText when colour is enabled. They
// are kept tiny on purpose — the output is meant to be readable in any
// monospace terminal, not pretty.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiCyan   = "\x1b[36m"
)

// FormatJSON writes the report as indented JSON to w. The shape matches
// the spec example so consumers can `jq .photos.missing_in_sorter[]`.
func FormatJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}

// FormatText writes a human-readable, section-by-section breakdown of
// the report to w. Counts always print first; the per-category lists
// follow, truncated to the same MaxItemsPerCategory the verifier uses.
// useColour toggles the ANSI escapes — pass false for log files /
// pipes / CI environments without TTY support.
func FormatText(w io.Writer, r *Report, useColour bool) {
	p := &textPrinter{w: w, useColour: useColour}
	p.photos(&r.Photos)
	p.albums(&r.Albums)
	p.labels(&r.Labels)
	p.subjects(&r.Subjects)
	p.markers(&r.Markers)
	p.disk(&r.Disk)
	p.summary(r)
}

// textPrinter encapsulates a colour-aware section writer so each
// per-section helper stays inside the linter's complexity budget.
type textPrinter struct {
	w         io.Writer
	useColour bool
}

// colour wraps s in ANSI escapes when useColour is true.
func (p *textPrinter) colour(code, s string) string {
	if !p.useColour {
		return s
	}
	return code + s + ansiReset
}

// heading prints a bolded section header.
func (p *textPrinter) heading(title string) {
	fmt.Fprintln(p.w)
	fmt.Fprintln(p.w, p.colour(ansiBold, "== "+title+" =="))
}

// counts prints the "X pp, Y sorter" line that opens every section.
func (p *textPrinter) counts(label string, pp, sorter int) {
	fmt.Fprintf(p.w, "  %s: pp=%d sorter=%d\n", label, pp, sorter)
}

// list prints a per-category list, truncated to MaxItemsPerCategory.
// header is e.g. "missing_in_sorter"; items is the truncated slice
// returned by the verifier.
func (p *textPrinter) list(header string, items []string) {
	if len(items) == 0 {
		return
	}
	style := ansiYellow
	if strings.HasPrefix(header, "orphan") {
		style = ansiRed
	}
	fmt.Fprintf(p.w, "  %s (%d shown):\n", p.colour(style, header), len(items))
	for _, item := range items {
		fmt.Fprintf(p.w, "    - %s\n", item)
	}
}

// photos prints the photos section.
func (p *textPrinter) photos(r *PhotoReport) {
	p.heading("photos")
	p.counts("counts", r.PPCount, r.SorterCount)
	p.entitySummary("Photos", len(r.MissingInSorter), len(r.OrphanInSorter), &r.FieldDiffs)
	p.list("missing_in_sorter", r.MissingInSorter)
	p.list("orphan_in_sorter", r.OrphanInSorter)
	p.fieldDiffs(&r.FieldDiffs)
}

// albums prints the albums section, including slug/title mismatches
// and per-album photo set diffs.
func (p *textPrinter) albums(r *AlbumReport) {
	p.heading("albums")
	p.counts("counts", r.PPCount, r.SorterCount)
	p.entitySummary("Albums", len(r.MissingInSorter), len(r.OrphanInSorter), &r.FieldDiffs)
	p.list("missing_in_sorter", r.MissingInSorter)
	p.list("orphan_in_sorter", r.OrphanInSorter)
	if len(r.SlugTitleMismatch) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "slug_title_mismatch"), len(r.SlugTitleMismatch))
		for _, d := range r.SlugTitleMismatch {
			fmt.Fprintf(p.w, "    - slug=%s pp=%q sorter=%q\n", d.Slug, d.PPTitle, d.NativeName)
		}
	}
	if len(r.PhotoDiffs) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "photo_diffs"), len(r.PhotoDiffs))
		for _, d := range r.PhotoDiffs {
			fmt.Fprintf(p.w, "    - %s: pp=%d sorter=%d missing=%d orphan=%d\n",
				d.Slug, d.PPCount, d.SorterCount, len(d.MissingInSorter), len(d.OrphanInSorter))
		}
	}
	p.membershipDiffs("membership_diffs", r.MembershipDiffs)
	p.fieldDiffs(&r.FieldDiffs)
}

// labels prints the labels section.
func (p *textPrinter) labels(r *LabelReport) {
	p.heading("labels")
	p.counts("counts", r.PPCount, r.SorterCount)
	p.counts("photo_pairs", r.PPPhotoPairs, r.SorterPhotoPairs)
	p.entitySummary("Labels", len(r.MissingInSorter), len(r.OrphanInSorter), &r.FieldDiffs)
	p.list("missing_in_sorter", r.MissingInSorter)
	p.list("orphan_in_sorter", r.OrphanInSorter)
	if len(r.SlugNameMismatch) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "slug_name_mismatch"), len(r.SlugNameMismatch))
		for _, d := range r.SlugNameMismatch {
			fmt.Fprintf(p.w, "    - slug=%s pp=%q sorter=%q\n", d.Slug, d.PPName, d.NativeName)
		}
	}
	if len(r.PhotoPairDiffs) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "photo_pair_diffs"), len(r.PhotoPairDiffs))
		for _, d := range r.PhotoPairDiffs {
			fmt.Fprintf(p.w, "    - %s: pp=%d sorter=%d\n", d.Slug, d.PPCount, d.SorterCount)
		}
	}
	p.membershipDiffs("membership_diffs", r.MembershipDiffs)
	p.fieldDiffs(&r.FieldDiffs)
}

// subjects prints the subjects section.
func (p *textPrinter) subjects(r *SubjectReport) {
	p.heading("subjects")
	p.counts("counts", r.PPCount, r.SorterCount)
	p.entitySummary("Subjects", len(r.MissingInSorter), len(r.OrphanInSorter), &r.FieldDiffs)
	p.list("missing_in_sorter", r.MissingInSorter)
	p.list("orphan_in_sorter", r.OrphanInSorter)
	p.fieldDiffs(&r.FieldDiffs)
}

// markers prints the markers section.
func (p *textPrinter) markers(r *MarkerReport) {
	p.heading("markers")
	p.counts("counts", r.PPCount, r.SorterCount)
	p.entitySummary("Markers", 0, 0, &r.FieldDiffs)
	if len(r.CountDiffs) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "count_diffs"), len(r.CountDiffs))
		for _, d := range r.CountDiffs {
			fmt.Fprintf(p.w, "    - %s: pp=%d sorter=%d\n", d.SubjectName, d.PPCount, d.SorterCount)
		}
	}
	if len(r.GeometryDiffs) > 0 {
		fmt.Fprintf(p.w, "  %s (%d):\n", p.colour(ansiYellow, "geometry_diffs"), len(r.GeometryDiffs))
		for _, d := range r.GeometryDiffs {
			fmt.Fprintf(p.w, "    - photo=%s pp_marker=%s pp=(%.3f,%.3f,%.3f,%.3f) sorter=(%.3f,%.3f,%.3f,%.3f)\n",
				d.PhotoSorterUID, d.PPMarkerUID,
				d.PPX, d.PPY, d.PPW, d.PPH,
				d.SorterX, d.SorterY, d.SorterW, d.SorterH)
		}
	}
	p.fieldDiffs(&r.FieldDiffs)
}

// disk prints the disk section.
func (p *textPrinter) disk(r *DiskReport) {
	p.heading("disk")
	p.list("orphan_files", r.OrphanFiles)
	if len(r.OrphanFiles) == 0 {
		fmt.Fprintln(p.w, "  (no orphan files)")
	}
}

// entitySummary prints a one-line "<Entity>: N missing | M field diffs
// (field: count, ...)" header. missingN/orphanN are passed in by the
// caller because each entity reports them slightly differently
// (markers use count/geometry rather than missing/orphan). When all
// counts are zero the line is suppressed entirely so a clean section
// stays tidy.
func (p *textPrinter) entitySummary(entity string, missingN, orphanN int, fields *FieldDiffBucket) {
	fieldTotal := 0
	if fields != nil {
		fieldTotal = fields.totalDiffs()
	}
	if missingN == 0 && orphanN == 0 && fieldTotal == 0 {
		return
	}
	parts := []string{fmt.Sprintf("%s: %d missing", entity, missingN)}
	if orphanN > 0 {
		parts = append(parts, fmt.Sprintf("%d orphan", orphanN))
	}
	if fieldTotal > 0 {
		parts = append(parts, fmt.Sprintf("%d field diffs (%s)", fieldTotal, formatFieldCounts(fields.FieldCounts)))
	}
	fmt.Fprintf(p.w, "  %s\n", strings.Join(parts, " | "))
}

// formatFieldCounts renders the per-field tally as "keywords: 2,
// panorama: 1" in deterministic order so the line is reproducible
// across runs.
func formatFieldCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

// fieldDiffs prints the first MaxFieldDiffsTextSample diffs from the
// bucket, colour-coded by side. Truncation footer is printed when the
// per-field tally exceeds the printed sample.
func (p *textPrinter) fieldDiffs(b *FieldDiffBucket) {
	if !b.HasDiffs() {
		return
	}
	fmt.Fprintf(p.w, "  %s (%d shown):\n", p.colour(ansiYellow, "field_diffs"), p.fieldDiffSampleSize(b))
	for i, d := range b.Diffs {
		if i >= MaxFieldDiffsTextSample {
			break
		}
		p.printOneFieldDiff(d)
	}
	if total := b.totalDiffs(); total > MaxFieldDiffsTextSample {
		fmt.Fprintf(p.w, "  %s\n", p.colour(ansiCyan,
			fmt.Sprintf("...and %d more truncated (use --json for the full report)", total-MaxFieldDiffsTextSample)))
	}
}

// fieldDiffSampleSize returns the lower of the bucket size and the
// text-sample cap, used to render the "(N shown)" prefix accurately.
func (p *textPrinter) fieldDiffSampleSize(b *FieldDiffBucket) int {
	if len(b.Diffs) < MaxFieldDiffsTextSample {
		return len(b.Diffs)
	}
	return MaxFieldDiffsTextSample
}

// printOneFieldDiff renders one FieldDiff entry. Red highlights the
// source side (which had a value the destination lost); yellow
// highlights the destination side (which has a value the source did
// not). When both sides are non-empty both are highlighted.
func (p *textPrinter) printOneFieldDiff(d FieldDiff) {
	srcOut := p.colour(ansiRed, "PhotoPrism="+d.Source)
	if d.Source == "" {
		srcOut = p.colour(ansiYellow, "PhotoPrism=<empty>")
	}
	dstOut := p.colour(ansiYellow, "native="+d.Destination)
	if d.Destination == "" {
		dstOut = p.colour(ansiRed, "native=<empty>")
	}
	fmt.Fprintf(p.w, "    - %s: %s  %s  %s\n", d.Key, d.Field, srcOut, dstOut)
}

// membershipDiffs prints the per-pair (photo, container) diffs that
// widen "1 fewer pair" into identity-level reporting. Truncation
// follows MaxFieldDiffsTextSample so the section stays consistent
// with the field-diff sampling rules.
func (p *textPrinter) membershipDiffs(header string, diffs []MembershipPairDiff) {
	if len(diffs) == 0 {
		return
	}
	shown := min(len(diffs), MaxFieldDiffsTextSample)
	fmt.Fprintf(p.w, "  %s (%d shown of %d):\n", p.colour(ansiYellow, header), shown, len(diffs))
	for i, d := range diffs {
		if i >= MaxFieldDiffsTextSample {
			break
		}
		style := ansiRed
		if d.Side == "sorter_only" {
			style = ansiYellow
		}
		fmt.Fprintf(p.w, "    - %s  %s/%s\n", p.colour(style, d.Side), d.ContainerSlug, d.PhotoFileHash)
	}
}

// summary prints a one-line outcome at the end of the report.
func (p *textPrinter) summary(r *Report) {
	fmt.Fprintln(p.w)
	if r.HasDiffs() {
		fmt.Fprintln(p.w, p.colour(ansiRed+ansiBold, "FAIL: differences found between PhotoPrism and sorter."))
		fmt.Fprintln(p.w, p.colour(ansiCyan, "Use --json for the full machine-readable report."))
		return
	}
	fmt.Fprintln(p.w, p.colour(ansiGreen+ansiBold, "OK: PhotoPrism and sorter match."))
}
