package verify

import (
	"context"
	"fmt"
	"sort"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppLabelRow is the minimal projection of a PhotoPrism label row.
type ppLabelRow struct {
	ID   int64
	Name string
	Slug string
}

// runLabels compares slug/name pairs and photo-label pair counts. Each
// PhotoPrism label is resolved by slug; name mismatches and missing rows
// are reported. The reverse pass flags sorter labels with no PhotoPrism
// counterpart. Pair counts are reported per-label, and totals are
// reported on the section header.
func (v *verifier) runLabels(ctx context.Context) error {
	ppLabels, err := v.readPPLabels(ctx)
	if err != nil {
		return fmt.Errorf("read pp labels: %w", err)
	}
	v.report.Labels.PPCount = len(ppLabels)

	ppPairCounts, ppTotalPairs, err := v.readPPLabelPairCounts(ctx)
	if err != nil {
		return fmt.Errorf("read pp photos_labels: %w", err)
	}
	v.report.Labels.PPPhotoPairs = ppTotalPairs

	if err := v.compareLabelsBySlug(ctx, ppLabels, ppPairCounts); err != nil {
		return err
	}
	return v.findOrphanLabels(ctx, ppLabels)
}

// compareLabelsBySlug iterates PhotoPrism labels, resolves the matching
// sorter label by slug, and records slug/name mismatches and per-label
// pair-count diffs.
func (v *verifier) compareLabelsBySlug(
	ctx context.Context, ppLabels []ppLabelRow, ppPairs map[int64]int,
) error {
	var (
		nameMiss []LabelDiff
		missing  []string
		pairMiss []LabelPairDiff
	)
	for _, l := range ppLabels {
		if l.Slug == "" {
			continue
		}
		native, err := v.opts.Labels.GetLabelBySlug(ctx, l.Slug)
		if err != nil || native == nil {
			missing = append(missing, l.Slug)
			continue
		}
		if native.Name != l.Name {
			nameMiss = append(nameMiss, LabelDiff{
				Slug:       l.Slug,
				PPName:     l.Name,
				NativeUID:  native.UID,
				NativeName: native.Name,
			})
		}
		ppCount := ppPairs[l.ID]
		if ppCount != native.PhotoCount {
			pairMiss = append(pairMiss, LabelPairDiff{
				Slug:        l.Slug,
				PPCount:     ppCount,
				SorterCount: native.PhotoCount,
			})
		}
	}
	sort.Strings(missing)
	v.report.Labels.SlugNameMismatch = nameMiss
	v.report.Labels.MissingInSorter = truncate(missing)
	v.report.Labels.PhotoPairDiffs = pairMiss
	return nil
}

// findOrphanLabels walks the sorter and flags slugs that are not in
// PhotoPrism. Also tallies the sorter's total label count and total
// photo-label pair count for the section header.
func (v *verifier) findOrphanLabels(ctx context.Context, ppLabels []ppLabelRow) error {
	ppSlugs := make(map[string]struct{}, len(ppLabels))
	for _, l := range ppLabels {
		if l.Slug != "" {
			ppSlugs[l.Slug] = struct{}{}
		}
	}

	var orphans []string
	total := 0
	totalPairs := 0
	offset := 0
	const pageSize = 500
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("orphan label walk canceled: %w", err)
		}
		page, err := v.opts.Labels.ListLabels(ctx, database.LabelQuery{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list sorter labels: %w", err)
		}
		if len(page) == 0 {
			break
		}
		for _, l := range page {
			total++
			totalPairs += l.PhotoCount
			if _, ok := ppSlugs[l.Slug]; !ok {
				orphans = append(orphans, l.Slug)
			}
		}
		offset += len(page)
	}
	v.report.Labels.SorterCount = total
	v.report.Labels.SorterPhotoPairs = totalPairs
	sort.Strings(orphans)
	v.report.Labels.OrphanInSorter = truncate(orphans)
	return nil
}

// readPPLabels loads non-deleted PhotoPrism labels with non-negative
// priority. Empty-name rows are skipped to match the migrator.
func (v *verifier) readPPLabels(ctx context.Context) ([]ppLabelRow, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT id, label_name, label_slug
		FROM labels
		WHERE deleted_at IS NULL
		  AND COALESCE(label_priority, 0) >= 0
		  AND label_name <> ''
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query pp labels: %w", err)
	}
	defer rows.Close()
	var out []ppLabelRow
	for rows.Next() {
		var (
			l       ppLabelRow
			slugRaw []byte
		)
		if err := rows.Scan(&l.ID, &l.Name, &slugRaw); err != nil {
			return nil, fmt.Errorf("scan pp label: %w", err)
		}
		l.Slug = string(slugRaw)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pp labels: %w", err)
	}
	return out, nil
}

// readPPLabelPairCounts returns the photo-count keyed by label_id for the
// non-deleted PhotoPrism photos_labels rows. The second return value is
// the total pair count across every label, used for the section header.
func (v *verifier) readPPLabelPairCounts(ctx context.Context) (map[int64]int, int, error) {
	rows, err := v.opts.MariaDB.QueryContext(ctx, `
		SELECT pl.label_id, COUNT(*)
		FROM photos_labels pl
		JOIN photos p ON p.id = pl.photo_id
		WHERE p.deleted_at IS NULL
		GROUP BY pl.label_id`)
	if err != nil {
		return nil, 0, fmt.Errorf("query pp photos_labels: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]int)
	total := 0
	for rows.Next() {
		var (
			labelID int64
			n       int
		)
		if err := rows.Scan(&labelID, &n); err != nil {
			return nil, 0, fmt.Errorf("scan pp photos_labels: %w", err)
		}
		out[labelID] = n
		total += n
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate pp photos_labels: %w", err)
	}
	return out, total, nil
}
