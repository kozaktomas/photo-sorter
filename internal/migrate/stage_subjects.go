package migrate

import (
	"context"
	"fmt"

	"github.com/schollz/progressbar/v3"
)

// ppSubject is a single row of the PhotoPrism `subjects` table.
type ppSubject struct {
	UID      string
	Name     string
	Type     string
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
// stage so one bad row does not block the rest.
func (m *migrator) processOneSubject(
	ctx context.Context, s *ppSubject, summary *StageSummary,
) {
	if m.opts.DryRun {
		summary.Created++
		return
	}
	existing, _ := m.opts.Subjects.GetSubjectByName(ctx, s.Name)
	if existing != nil {
		summary.Skipped++
		return
	}
	subjectType := s.Type
	if subjectType == "" {
		subjectType = "person"
	}
	created, err := m.opts.Subjects.EnsureSubject(ctx, s.Name, subjectType)
	if err != nil {
		fmt.Fprintf(m.out, "\nsubject %q: %v\n", s.Name, err)
		summary.Failed++
		return
	}
	// Apply favorite/private flags if PhotoPrism had them set — the
	// EnsureSubject path only writes name/type.
	if s.Favorite || s.Private {
		created.Favorite = s.Favorite
		created.Private = s.Private
		if err := m.opts.Subjects.UpdateSubject(ctx, created); err != nil {
			fmt.Fprintf(m.out, "\nsubject %q flags: %v\n", s.Name, err)
			summary.Failed++
			return
		}
	}
	summary.Created++
}

// readPPSubjects loads every non-deleted PhotoPrism subject row that has a
// non-empty name. Anonymous/auto-clusters (PhotoPrism creates rows with an
// empty name for unidentified face clusters) are excluded.
func (m *migrator) readPPSubjects(ctx context.Context) ([]ppSubject, error) {
	rows, err := m.opts.MariaDB.QueryContext(ctx,
		`SELECT subj_uid, subj_name, subj_type, subj_favorite, subj_private
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
			s            ppSubject
			fav, private int
		)
		if err := rows.Scan(&s.UID, &s.Name, &s.Type, &fav, &private); err != nil {
			return nil, fmt.Errorf("scan subject: %w", err)
		}
		s.Favorite = fav != 0
		s.Private = private != 0
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
