package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
)

// remapTargets is the list of (table, column) pairs the remap pass
// rewrites for every (old_uid, new_uid) in the photo map. Order matters:
// photo_phashes references photos(uid) via a FK with NO ACTION on UPDATE,
// so its child rows must move *before* photos.uid changes. Likewise
// album_photos / photo_labels reference photos(uid) and would block a
// photos.uid update — but the remap command never touches photos.uid; it
// only rewrites the soft references that point at PhotoPrism UIDs. The
// "section_photos" and "page_slots" tables have no FK to photos at all,
// so they are free to update in any order.
//
// Each remap target is a (table, column) pair. The remap pass executes a
// single UPDATE per pair inside the surrounding transaction.
var remapTargets = []remapTarget{
	{Table: "embeddings", Column: "photo_uid"},
	{Table: "faces", Column: "photo_uid"},
	{Table: "faces_processed", Column: "photo_uid"},
	{Table: "markers", Column: "photo_uid"},
	{Table: "album_photos", Column: "photo_uid"},
	{Table: "photo_labels", Column: "photo_uid"},
	{Table: "photo_phashes", Column: "photo_uid"},
	{Table: "section_photos", Column: "photo_uid"},
	{Table: "page_slots", Column: "photo_uid"},
}

// remapTarget identifies one (table, column) pair the remap pass
// rewrites. The columns are all soft references holding a PhotoPrism
// photo_uid; the actual photos(uid) primary key is not touched by the
// remap (it carries the PhotoPrism UID after the fixed migrator runs).
type remapTarget struct {
	Table  string
	Column string
}

// RemapOptions configures a remap-references run.
type RemapOptions struct {
	// Map is the parsed photo-map JSON. PhotoUIDMap drives the actual
	// remap; entries where old == new are skipped silently.
	Map *PhotoMapJSON
	// DryRun reports the row counts that *would* change without writing.
	// The full transaction still opens (so SELECT counts come from the
	// would-be-modified snapshot) and is rolled back at the end.
	DryRun bool
	// Writer receives human-readable progress output. nil falls back to
	// os.Stdout in the caller.
	Writer io.Writer
}

// RemapSummary aggregates how many rows changed in each (table, column)
// remap target plus any orphan-row counts the integrity audit found.
type RemapSummary struct {
	// Identity is true when every entry in PhotoUIDMap had old == new and
	// the command returned without touching the DB.
	Identity bool
	// Updated is the per-target row count actually changed (or that
	// would have been changed in dry-run mode).
	Updated map[string]int64
	// Orphans counts soft-FK rows whose photo_uid does not match any
	// photos.uid after the remap. Non-zero is informational (the
	// PhotoPrism originals may have been deleted independently); the
	// command does not fail.
	Orphans map[string]int64
}

// IdentityMap reports whether every key in PhotoUIDMap maps to itself.
// An identity map means the migration preserved every UID, so there is
// nothing for the remap pass to do.
func (p *PhotoMapJSON) IdentityMap() bool {
	if p == nil {
		return true
	}
	for k, v := range p.PhotoUIDMap {
		if k != v {
			return false
		}
	}
	return true
}

// RemapReferences rewrites every (old_uid, new_uid) row in the supplied
// photo map's PhotoUIDMap across all the soft-FK tables in remapTargets.
// The entire pass runs inside one Postgres transaction; either every
// table is remapped or nothing is.
//
// Identity maps short-circuit before the transaction opens. Dry-run mode
// opens the transaction, runs the UPDATEs (so the row counts are
// accurate), then rolls back.
func RemapReferences(
	ctx context.Context, db *sql.DB, opts *RemapOptions,
) (*RemapSummary, error) {
	if opts == nil || opts.Map == nil {
		return nil, errors.New("remap: photo map is required")
	}
	summary := &RemapSummary{
		Updated: make(map[string]int64, len(remapTargets)),
		Orphans: make(map[string]int64, len(remapTargets)),
	}
	if opts.Map.IdentityMap() {
		summary.Identity = true
		return summary, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("remap: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := applyRemapUpdates(ctx, tx, opts.Map.PhotoUIDMap, summary); err != nil {
		return nil, err
	}
	if err := auditOrphans(ctx, tx, summary); err != nil {
		return nil, err
	}

	if opts.DryRun {
		// Roll back via the deferred call so the on-disk state is
		// unchanged. Returning the would-be counts is the whole point of
		// the dry-run.
		return summary, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("remap: commit: %w", err)
	}
	return summary, nil
}

// applyRemapUpdates issues one UPDATE per (table, column) target using a
// VALUES-driven join keyed on the old UID. Doing one round trip per
// table (instead of one per old/new pair) keeps the remap fast even when
// the map contains tens of thousands of rows.
func applyRemapUpdates(
	ctx context.Context, tx *sql.Tx, photoMap map[string]string, summary *RemapSummary,
) error {
	pairs := nonIdentityPairs(photoMap)
	if len(pairs) == 0 {
		return nil
	}
	values, args := buildValuesClause(pairs)
	for _, t := range remapTargets {
		key := t.Table + "." + t.Column
		// #nosec G201 -- t.Table/t.Column are not user input: they come
		// from the hard-coded remapTargets list above.
		stmt := fmt.Sprintf(
			`UPDATE %s SET %s = m.new_uid
			 FROM (VALUES %s) AS m(old_uid, new_uid)
			 WHERE %s = m.old_uid`,
			t.Table, t.Column, values, t.Column,
		)
		res, err := tx.ExecContext(ctx, stmt, args...)
		if err != nil {
			return fmt.Errorf("remap %s.%s: %w", t.Table, t.Column, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("remap %s.%s rows affected: %w", t.Table, t.Column, err)
		}
		summary.Updated[key] = n
	}
	return nil
}

// auditOrphans counts rows in each remap target whose photo_uid no
// longer references a photos.uid (the row will look detached in the
// UI). These are informational — the operator may have deleted the
// PhotoPrism originals before the migration, leaving downstream rows
// pointing at nothing. The remap command surfaces the count so the
// operator can decide whether to clean up.
func auditOrphans(ctx context.Context, tx *sql.Tx, summary *RemapSummary) error {
	for _, t := range remapTargets {
		key := t.Table + "." + t.Column
		// #nosec G201 -- t.Table/t.Column come from the hard-coded
		// remapTargets list, not from user input.
		stmt := fmt.Sprintf(
			`SELECT COUNT(*) FROM %s t
			 LEFT JOIN photos p ON p.uid = t.%s
			 WHERE t.%s IS NOT NULL AND p.uid IS NULL`,
			t.Table, t.Column, t.Column,
		)
		var n int64
		if err := tx.QueryRowContext(ctx, stmt).Scan(&n); err != nil {
			return fmt.Errorf("audit orphans %s.%s: %w", t.Table, t.Column, err)
		}
		summary.Orphans[key] = n
	}
	return nil
}

// nonIdentityPairs returns the entries in photoMap whose old != new.
// Identity entries would generate no-op UPDATEs and only slow the pass
// down.
func nonIdentityPairs(photoMap map[string]string) [][2]string {
	out := make([][2]string, 0, len(photoMap))
	for k, v := range photoMap {
		if k == "" || v == "" || k == v {
			continue
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

// buildValuesClause constructs a parameterised VALUES list of the form
// ($1::text, $2::text), ($3::text, $4::text), ... and the corresponding
// argument slice. The explicit ::text cast keeps Postgres from inferring
// "unknown" types when the VALUES are joined against a VARCHAR column.
func buildValuesClause(pairs [][2]string) (string, []any) {
	args := make([]any, 0, len(pairs)*2)
	var b strings.Builder
	for i, p := range pairs {
		base := i * 2
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "($%d::text, $%d::text)", base+1, base+2)
		args = append(args, p[0], p[1])
	}
	return b.String(), args
}
