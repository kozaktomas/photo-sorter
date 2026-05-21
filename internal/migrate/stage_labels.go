package migrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// ppLabel is a single row of PhotoPrism's `labels` table. Description /
// Categories are pulled in by readPPLabels so the migrator can populate
// the native columns added by migration 037 (label_description and
// label_categories are PhotoPrism's source columns; categories arrives as
// a comma-separated string and is unpacked into a slice).
type ppLabel struct {
	ID          int64
	Name        string
	Slug        string
	Description string
	Categories  []string
	Priority    int
	Favorite    bool
}

// ppPhotoLabel is one row of `photos_labels` joined back to the PhotoPrism
// photo UID so the migrator can resolve it through the photo UID map
// without re-querying.
type ppPhotoLabel struct {
	PhotoUID    string
	LabelID     int64
	Source      string
	Uncertainty int
}

// stageLabels imports labels and their photo links. EnsureLabel is
// idempotent on slug, so re-running adds nothing. photo_labels rows are
// inserted with source = "import" so the operator can distinguish
// migrated tags from native AI/manual entries.
func (m *migrator) stageLabels(ctx context.Context) error {
	labels, err := m.readPPLabels(ctx)
	if err != nil {
		return fmt.Errorf("read labels: %w", err)
	}
	links, err := m.readPPPhotoLabels(ctx)
	if err != nil {
		return fmt.Errorf("read photos_labels: %w", err)
	}
	idMap, err := m.upsertLabels(ctx, labels)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	return m.linkPhotoLabels(ctx, links, idMap)
}

// upsertLabels inserts (or finds) every PhotoPrism label in the native
// store and returns the (mariadb-label-id → native-label-uid) mapping.
func (m *migrator) upsertLabels(
	ctx context.Context, labels []ppLabel,
) (map[int64]string, error) {
	idMap := make(map[int64]string, len(labels))
	summary := StageSummary{Stage: StageLabels, Read: len(labels)}
	bar := newStageBar(len(labels), "labels")
	defer finishBar(bar)
	for i := range labels {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(summary)
			return idMap, fmt.Errorf("labels canceled: %w", err)
		}
		_ = bar.Add(1)
		m.processOneLabel(ctx, &labels[i], idMap, &summary)
	}
	m.report.AppendStage(summary)
	return idMap, nil
}

// processOneLabel upserts a single label and records its native UID.
// Existing rows are reported as Skipped so re-runs converge on zero
// new creations — but description/categories are backfilled when the
// destination value is still the column zero value (task 332a727c).
func (m *migrator) processOneLabel(
	ctx context.Context, l *ppLabel, idMap map[int64]string, summary *StageSummary,
) {
	if m.opts.DryRun {
		summary.Created++
		idMap[l.ID] = "dry-label-" + l.Slug
		return
	}
	existing, _ := m.opts.Labels.GetLabelBySlug(ctx, l.Slug)
	if existing != nil {
		idMap[l.ID] = existing.UID
		m.backfillLabelExtras(ctx, l, existing)
		summary.Skipped++
		return
	}
	native, err := m.opts.Labels.EnsureLabel(ctx, l.Name)
	if err != nil {
		fmt.Fprintf(m.out, "\nlabel %q: %v\n", l.Name, err)
		summary.Failed++
		return
	}
	// Persist priority/favorite/description/categories when PhotoPrism
	// had them set — EnsureLabel only writes name/slug.
	if labelNeedsUpdate(l, native) {
		native.Priority = l.Priority
		native.Favorite = l.Favorite
		if l.Description != "" {
			native.Description = l.Description
		}
		if len(l.Categories) > 0 {
			native.Categories = append([]string(nil), l.Categories...)
		}
		if err := m.opts.Labels.UpdateLabel(ctx, native); err != nil {
			fmt.Fprintf(m.out, "\nlabel %q flags: %v\n", l.Name, err)
			summary.Failed++
			return
		}
	}
	idMap[l.ID] = native.UID
	summary.Created++
}

// labelNeedsUpdate reports whether the PhotoPrism label has flag, text,
// or category values that the native row does not yet reflect.
func labelNeedsUpdate(pp *ppLabel, native *database.Label) bool {
	if labelFlagsDiffer(pp, native) {
		return true
	}
	if pp.Description != "" && native.Description == "" {
		return true
	}
	if len(pp.Categories) > 0 && len(native.Categories) == 0 {
		return true
	}
	return false
}

// labelFlagsDiffer reports whether the PhotoPrism label has flag values
// that the native row does not yet reflect (priority / favorite).
func labelFlagsDiffer(pp *ppLabel, native *database.Label) bool {
	if pp.Priority == 0 && !pp.Favorite {
		return false
	}
	return native.Priority != pp.Priority || native.Favorite != pp.Favorite
}

// backfillLabelExtras fills description / categories on an existing
// destination row when the destination value is still the column zero
// value and the PhotoPrism source has a non-default value. The native
// flags (priority/favorite) are left alone because the user may have
// edited them after the original migration; the spec only asks for the
// new TEXT/TEXT[] columns to be backfilled.
func (m *migrator) backfillLabelExtras(
	ctx context.Context, l *ppLabel, existing *database.Label,
) {
	changed := false
	if existing.Description == "" && l.Description != "" {
		existing.Description = l.Description
		changed = true
	}
	if len(existing.Categories) == 0 && len(l.Categories) > 0 {
		existing.Categories = append([]string(nil), l.Categories...)
		changed = true
	}
	if !changed {
		return
	}
	if err := m.opts.Labels.UpdateLabel(ctx, existing); err != nil {
		fmt.Fprintf(m.out, "\nlabel %q backfill: %v\n", l.Name, err)
	}
}

// linkPhotoLabels attaches every (photo, label) pair to its native rows
// using AddPhotoLabel with source = "import".
func (m *migrator) linkPhotoLabels(
	ctx context.Context, links []ppPhotoLabel, idMap map[int64]string,
) error {
	linkSummary := StageSummary{Stage: "photo_labels", Read: len(links)}
	linkBar := newStageBar(len(links), "links")
	defer finishBar(linkBar)
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(linkSummary)
			return fmt.Errorf("photo_labels canceled: %w", err)
		}
		_ = linkBar.Add(1)
		m.processOneLabelLink(ctx, link, idMap, &linkSummary)
	}
	m.report.AppendStage(linkSummary)
	return nil
}

// processOneLabelLink attaches one (photo, label) pair. The destination
// is queried first so re-runs report the right count (the underlying
// AddPhotoLabel uses ON CONFLICT DO NOTHING; without the pre-check we
// would always credit a "create").
func (m *migrator) processOneLabelLink(
	ctx context.Context, link ppPhotoLabel, idMap map[int64]string, summary *StageSummary,
) {
	nativePhoto, ok := m.photoMap[link.PhotoUID]
	if !ok {
		summary.Skipped++
		return
	}
	nativeLabel, ok := idMap[link.LabelID]
	if !ok {
		summary.Skipped++
		return
	}
	if m.opts.DryRun {
		summary.Created++
		return
	}
	if m.photoLabelExists(ctx, nativePhoto, nativeLabel) {
		summary.Skipped++
		return
	}
	if err := m.opts.Labels.AddPhotoLabel(
		ctx, nativePhoto, nativeLabel, "import", link.Uncertainty,
	); err != nil {
		fmt.Fprintf(m.out, "\nlink %s↔%s: %v\n", link.PhotoUID, nativeLabel, err)
		summary.Failed++
		return
	}
	summary.Created++
}

// photoLabelExists reports whether the destination already has the
// (photo, label) pair. Errors are swallowed — a transient query failure
// falls through to the insert path, which is itself idempotent.
func (m *migrator) photoLabelExists(ctx context.Context, photoUID, labelUID string) bool {
	labels, err := m.opts.Labels.ListLabelsForPhoto(ctx, photoUID)
	if err != nil {
		return false
	}
	for _, lbl := range labels {
		if lbl.UID == labelUID {
			return true
		}
	}
	return false
}

// readPPLabels loads non-deleted labels with non-negative priority
// (PhotoPrism uses negative priorities for internal/hidden labels which
// we deliberately exclude). label_description and label_categories are
// pulled into the projection so the migrator can populate the native
// columns added by migration 037 (task 332a727c).
func (m *migrator) readPPLabels(ctx context.Context) ([]ppLabel, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx,
		`SELECT id, label_name, label_slug,
		        COALESCE(label_priority, 0), COALESCE(label_favorite, 0),
		        COALESCE(label_description, ''),
		        COALESCE(label_categories, '')
		 FROM labels
		 WHERE deleted_at IS NULL AND COALESCE(label_priority, 0) >= 0
		   AND label_name <> ''
		 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}
	defer rows.Close()
	var out []ppLabel
	for rows.Next() {
		var (
			l                          ppLabel
			fav                        int
			descriptionRaw, categories []byte
		)
		if err := rows.Scan(
			&l.ID, &l.Name, &l.Slug, &l.Priority, &fav,
			&descriptionRaw, &categories,
		); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		l.Favorite = fav != 0
		l.Description = string(descriptionRaw)
		l.Categories = splitLabelCategories(string(categories))
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels: %w", err)
	}
	return out, nil
}

// splitLabelCategories unpacks PhotoPrism's comma-separated
// label_categories column into a deduplicated slice. Whitespace-padded
// tokens are trimmed; empty tokens are dropped; the first-seen casing
// wins on duplicate-after-trim entries. The result preserves source
// order so the migrated value is stable across runs.
func splitLabelCategories(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

// readPPPhotoLabels joins `photos_labels` to `photos` so we get a
// (photo_uid, label_id) pair directly. PhotoPrism's photos_labels uses
// numeric photo_id which we don't keep around, hence the JOIN.
func (m *migrator) readPPPhotoLabels(ctx context.Context) ([]ppPhotoLabel, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx, `
		SELECT p.photo_uid, pl.label_id,
		       COALESCE(pl.label_src, ''),
		       COALESCE(pl.uncertainty, 0)
		FROM photos_labels pl
		JOIN photos p ON p.id = pl.photo_id
		WHERE p.deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("query photos_labels: %w", err)
	}
	defer rows.Close()
	var out []ppPhotoLabel
	for rows.Next() {
		var (
			pl  ppPhotoLabel
			src []byte
		)
		if err := rows.Scan(&pl.PhotoUID, &pl.LabelID, &src, &pl.Uncertainty); err != nil {
			return nil, fmt.Errorf("scan photo_label: %w", err)
		}
		pl.Source = string(src)
		out = append(out, pl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos_labels: %w", err)
	}
	return out, nil
}
