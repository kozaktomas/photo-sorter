package migrate

import (
	"context"
	"fmt"

	"github.com/kozaktomas/photo-sorter/internal/database"
	"github.com/schollz/progressbar/v3"
)

// ppSubject is a single row of the PhotoPrism `subjects` table. Bio/About/
// Alias are preserved verbatim (subj_alias may contain comma-separated
// values; the native schema stores it as a single TEXT field so splitting
// would lose intent).
type ppSubject struct {
	UID      string
	Name     string
	Type     string
	Bio      string
	About    string
	Alias    string
	Favorite bool
	Private  bool
}

// stageSubjects imports every PhotoPrism subject as a native subject.
// Lookup is idempotent on accent-insensitive lowercased name (handled by
// SubjectWriter.EnsureSubject), so re-running the stage produces zero new
// rows. Subject type is preserved (PhotoPrism uses "person" / "pet" / "");
// empty maps to the native "person" default.
func (m *migrator) stageSubjects(ctx context.Context) error {
	subjects, err := m.readPPSubjects(ctx)
	if err != nil {
		return fmt.Errorf("read subjects: %w", err)
	}

	summary := StageSummary{Stage: StageSubjects, Read: len(subjects)}
	bar := newStageBar(len(subjects), "subjects")
	defer finishBar(bar)

	for i := range subjects {
		if err := ctx.Err(); err != nil {
			m.report.AppendStage(summary)
			return fmt.Errorf("subjects canceled: %w", err)
		}
		_ = bar.Add(1)
		m.processOneSubject(ctx, &subjects[i], &summary)
	}
	m.report.AppendStage(summary)
	return nil
}

// processOneSubject handles a single PhotoPrism subject; counters on the
// summary are updated in place. Errors are logged and do not abort the
// stage so one bad row does not block the rest. Existing rows are
// re-visited so columns added after the first migration (bio/about/alias,
// see task 332a727c) can be backfilled — but only when the destination
// value is still the column zero value.
func (m *migrator) processOneSubject(
	ctx context.Context, s *ppSubject, summary *StageSummary,
) {
	if m.opts.DryRun {
		summary.Created++
		return
	}
	existing, _ := m.opts.Subjects.GetSubjectByName(ctx, s.Name)
	if existing != nil {
		m.backfillSubjectExtras(ctx, s, existing)
		summary.Skipped++
		return
	}
	subjectType := s.Type
	if subjectType == "" {
		subjectType = "person"
	}
	// Preserve the PhotoPrism subj_uid as the native subjects.uid so
	// cached PhotoPrism references in faces.subject_uid keep pointing at
	// the right row. EnsureSubjectWithUID only uses the supplied UID on
	// insert; an existing row matched by name keeps its native UID.
	created, err := m.opts.Subjects.EnsureSubjectWithUID(ctx, s.UID, s.Name, subjectType)
	if err != nil {
		fmt.Fprintf(m.out, "\nsubject %q: %v\n", s.Name, err)
		summary.Failed++
		return
	}
	// Apply favorite/private flags + bio/about/alias if PhotoPrism had
	// them set — the EnsureSubject path only writes name/type.
	if subjectFlagsOrExtrasSet(s) {
		created.Favorite = s.Favorite
		created.Private = s.Private
		applySubjectExtras(s, created)
		if err := m.opts.Subjects.UpdateSubject(ctx, created); err != nil {
			fmt.Fprintf(m.out, "\nsubject %q flags: %v\n", s.Name, err)
			summary.Failed++
			return
		}
	}
	summary.Created++
}

// subjectFlagsOrExtrasSet returns true when at least one source-side
// optional column has a non-zero value worth syncing on first insert.
func subjectFlagsOrExtrasSet(s *ppSubject) bool {
	return s.Favorite || s.Private ||
		s.Bio != "" || s.About != "" || s.Alias != ""
}

// applySubjectExtras copies bio/about/alias into the native subject row.
// Empty source values are not written so a manual edit in the native UI
// survives a re-run.
func applySubjectExtras(s *ppSubject, dst *database.Subject) {
	if s.Bio != "" {
		dst.Bio = s.Bio
	}
	if s.About != "" {
		dst.About = s.About
	}
	if s.Alias != "" {
		dst.Alias = s.Alias
	}
}

// backfillSubjectExtras fills bio/about/alias on an existing destination
// row when the destination value is still the column zero value and the
// PhotoPrism source has a non-default value. Flags (favorite/private) are
// left alone because the user may have edited them in the native UI; the
// spec only asks for the new TEXT columns to be backfilled.
func (m *migrator) backfillSubjectExtras(
	ctx context.Context, s *ppSubject, existing *database.Subject,
) {
	changed := false
	if existing.Bio == "" && s.Bio != "" {
		existing.Bio = s.Bio
		changed = true
	}
	if existing.About == "" && s.About != "" {
		existing.About = s.About
		changed = true
	}
	if existing.Alias == "" && s.Alias != "" {
		existing.Alias = s.Alias
		changed = true
	}
	if !changed {
		return
	}
	if err := m.opts.Subjects.UpdateSubject(ctx, existing); err != nil {
		fmt.Fprintf(m.out, "\nsubject %q backfill: %v\n", s.Name, err)
	}
}

// readPPSubjects loads every non-deleted PhotoPrism subject row that has a
// non-empty name. Anonymous/auto-clusters (PhotoPrism creates rows with an
// empty name for unidentified face clusters) are excluded. subj_bio /
// subj_about / subj_alias are pulled into the projection so the migrator
// can populate the matching native columns (added by migration 037).
func (m *migrator) readPPSubjects(ctx context.Context) ([]ppSubject, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx,
		`SELECT subj_uid, subj_name, subj_type, subj_favorite, subj_private,
		        COALESCE(subj_bio, ''),
		        COALESCE(subj_about, ''),
		        COALESCE(subj_alias, '')
		 FROM subjects
		 WHERE deleted_at IS NULL AND subj_name <> ''
		 ORDER BY subj_uid`)
	if err != nil {
		return nil, fmt.Errorf("query subjects: %w", err)
	}
	defer rows.Close()
	var out []ppSubject
	for rows.Next() {
		var (
			s                       ppSubject
			fav, private            int
			bioRaw, aboutRaw, alias []byte
		)
		if err := rows.Scan(
			&s.UID, &s.Name, &s.Type, &fav, &private,
			&bioRaw, &aboutRaw, &alias,
		); err != nil {
			return nil, fmt.Errorf("scan subject: %w", err)
		}
		s.Favorite = fav != 0
		s.Private = private != 0
		s.Bio = string(bioRaw)
		s.About = string(aboutRaw)
		s.Alias = string(alias)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subjects: %w", err)
	}
	return out, nil
}

// newStageBar returns a progressbar wired with the conventions used by the
// other photo-sorter CLIs (the upload progress bar is the reference shape).
func newStageBar(total int, items string) *progressbar.ProgressBar {
	return progressbar.NewOptions(total,
		progressbar.OptionSetDescription("Migrating "+items),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString(items),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

// finishBar prints a trailing newline so subsequent log lines line up.
func finishBar(bar *progressbar.ProgressBar) {
	_ = bar.Finish()
	fmt.Println()
}
