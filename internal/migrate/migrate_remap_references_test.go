//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestRemapReferences_Identity short-circuits the remap when every old
// UID equals its new UID — the happy path after a fixed migrator run.
// No transaction should open and no row should be touched.
func TestRemapReferences_Identity(t *testing.T) {
	fx := setupFixture(t)
	if fx == nil {
		return
	}
	defer fx.cleanup()

	ctx := context.Background()
	summary, err := RemapReferences(ctx, fx.pgPool.DB(), &RemapOptions{
		Map: &PhotoMapJSON{
			PhotoUIDMap: map[string]string{"p001": "p001", "p002": "p002"},
		},
	})
	if err != nil {
		t.Fatalf("remap identity: %v", err)
	}
	if !summary.Identity {
		t.Errorf("Identity = false, want true")
	}
	for k, v := range summary.Updated {
		if v != 0 {
			t.Errorf("identity map updated %s: %d rows", k, v)
		}
	}
}

// TestRemapReferences_NonIdentity rewrites soft-FK rows across every
// remap target. We seed each table with rows referencing an "old" UID
// and confirm they end up referencing the "new" UID after the remap.
// The "new" UID matches a real photos.uid (post-migration) so the
// orphan audit stays at zero.
func TestRemapReferences_NonIdentity(t *testing.T) {
	fx := setupFixture(t)
	if fx == nil {
		return
	}
	defer fx.cleanup()

	ctx := context.Background()
	if _, err := Run(ctx, buildOptions(fx)); err != nil {
		t.Fatalf("baseline migration: %v", err)
	}

	const oldUID = "p001-legacy"
	const newUID = "p001"

	if err := seedRemapFixtures(ctx, fx, oldUID); err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}

	summary, err := RemapReferences(ctx, fx.pgPool.DB(), &RemapOptions{
		Map: &PhotoMapJSON{
			PhotoUIDMap: map[string]string{oldUID: newUID},
		},
	})
	if err != nil {
		t.Fatalf("remap non-identity: %v", err)
	}
	if summary.Identity {
		t.Errorf("Identity = true, want false")
	}
	// Every target we seeded should report exactly one row updated.
	wantTouched := []string{
		"embeddings.photo_uid",
		"faces.photo_uid",
		"faces_processed.photo_uid",
		"section_photos.photo_uid",
	}
	for _, key := range wantTouched {
		if got := summary.Updated[key]; got != 1 {
			t.Errorf("Updated[%s] = %d, want 1", key, got)
		}
	}
	// Targets we did not seed should report zero updates but still be
	// present in the map (every target is always issued an UPDATE).
	for _, key := range []string{
		"markers.photo_uid", "album_photos.photo_uid", "photo_labels.photo_uid",
		"photo_phashes.photo_uid", "page_slots.photo_uid",
	} {
		if got, ok := summary.Updated[key]; !ok || got != 0 {
			t.Errorf("Updated[%s] = %d (ok=%v), want 0", key, got, ok)
		}
	}
	// After the remap, no soft-FK row should reference oldUID anymore.
	var lingering int
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE photo_uid = $1`, oldUID,
	).Scan(&lingering); err != nil {
		t.Fatalf("count lingering embeddings: %v", err)
	}
	if lingering != 0 {
		t.Errorf("lingering embeddings under oldUID: %d, want 0", lingering)
	}
	// And every orphan count must be zero because the rows now point at
	// real photos.
	for k, n := range summary.Orphans {
		if n != 0 {
			t.Errorf("orphan count %s = %d, want 0", k, n)
		}
	}
}

// TestRemapReferences_DryRunDoesNotPersist confirms the dry-run path
// rolls back its transaction: post-call the seeded "old" rows are
// untouched.
func TestRemapReferences_DryRunDoesNotPersist(t *testing.T) {
	fx := setupFixture(t)
	if fx == nil {
		return
	}
	defer fx.cleanup()

	ctx := context.Background()
	if _, err := Run(ctx, buildOptions(fx)); err != nil {
		t.Fatalf("baseline migration: %v", err)
	}
	const oldUID = "p001-legacy"
	const newUID = "p001"
	if err := seedRemapFixtures(ctx, fx, oldUID); err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}

	summary, err := RemapReferences(ctx, fx.pgPool.DB(), &RemapOptions{
		Map: &PhotoMapJSON{
			PhotoUIDMap: map[string]string{oldUID: newUID},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("remap dry-run: %v", err)
	}
	if got := summary.Updated["embeddings.photo_uid"]; got != 1 {
		t.Errorf("dry-run row count: Updated[embeddings] = %d, want 1", got)
	}
	// The actual rows must still reference oldUID — dry-run rolls back.
	var stillOld int
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE photo_uid = $1`, oldUID,
	).Scan(&stillOld); err != nil {
		t.Fatalf("count still-old embeddings: %v", err)
	}
	if stillOld != 1 {
		t.Errorf("dry-run mutated DB: %d rows still under oldUID, want 1", stillOld)
	}
}

// TestRemapReferences_TransactionRollback proves the all-or-nothing
// contract: an UPDATE that fails partway through must leave every
// previously-updated table untouched. We simulate the failure by
// injecting a remap target that doesn't exist; the very first UPDATE
// throws and the surrounding deferred Rollback discards every prior
// change.
func TestRemapReferences_TransactionRollback(t *testing.T) {
	fx := setupFixture(t)
	if fx == nil {
		return
	}
	defer fx.cleanup()

	ctx := context.Background()
	if _, err := Run(ctx, buildOptions(fx)); err != nil {
		t.Fatalf("baseline migration: %v", err)
	}
	const oldUID = "p001-legacy"
	const newUID = "p001"
	if err := seedRemapFixtures(ctx, fx, oldUID); err != nil {
		t.Fatalf("seed fixtures: %v", err)
	}

	// Patch remapTargets to include a non-existent table at the end so
	// the first few UPDATEs succeed inside the transaction and then a
	// later one fails. Restore the original list when the test exits so
	// other tests are not affected.
	original := append([]remapTarget(nil), remapTargets...)
	t.Cleanup(func() { remapTargets = original })
	remapTargets = append(remapTargets, remapTarget{
		Table: "does_not_exist", Column: "photo_uid",
	})

	_, err := RemapReferences(ctx, fx.pgPool.DB(), &RemapOptions{
		Map: &PhotoMapJSON{PhotoUIDMap: map[string]string{oldUID: newUID}},
	})
	if err == nil {
		t.Fatalf("expected remap to fail on bogus target, got nil error")
	}

	// Despite earlier UPDATEs succeeding inside the transaction, the
	// rollback must restore the seeded rows to point at oldUID.
	var stillOld int
	if err := fx.pgPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE photo_uid = $1`, oldUID,
	).Scan(&stillOld); err != nil {
		t.Fatalf("count post-rollback embeddings: %v", err)
	}
	if stillOld != 1 {
		t.Errorf("transaction rolled back partially: %d rows, want 1", stillOld)
	}
}

// seedRemapFixtures inserts one row into each soft-FK table that the
// remap pass will touch, all under the supplied "old" photo UID. The
// rows reference no real photo (oldUID is not in photos.uid) so they
// will register as orphans before the remap and reconnect afterwards.
func seedRemapFixtures(
	ctx context.Context, fx *testFixture, oldUID string,
) error {
	embedding := embeddingZeroLiteral(768)
	face := embeddingZeroLiteral(512)
	bbox := "{0.1,0.1,0.4,0.4}"

	stmts := []struct {
		name string
		sql  string
		args []any
	}{
		{
			"embeddings",
			`INSERT INTO embeddings (photo_uid, embedding, model, pretrained)
			 VALUES ($1, $2::vector, 'test', 'test')`,
			[]any{oldUID, embedding},
		},
		{
			"faces",
			`INSERT INTO faces (photo_uid, face_index, embedding, bbox, det_score)
			 VALUES ($1, 0, $2::vector, $3::double precision[], 0.95)`,
			[]any{oldUID, face, bbox},
		},
		{
			"faces_processed",
			`INSERT INTO faces_processed (photo_uid, face_count) VALUES ($1, 1)`,
			[]any{oldUID},
		},
		{
			"section_photos",
			`WITH b AS (
			   INSERT INTO photo_books (id, title) VALUES ('remap-book', 'Remap')
			   ON CONFLICT (id) DO NOTHING
			   RETURNING id
			 ), b2 AS (
			   SELECT id FROM photo_books WHERE id = 'remap-book'
			 ), s AS (
			   INSERT INTO book_sections (id, book_id, title)
			   VALUES ('remap-section', 'remap-book', 'Section')
			   ON CONFLICT (id) DO NOTHING
			   RETURNING id
			 )
			 INSERT INTO section_photos (section_id, photo_uid)
			 VALUES ('remap-section', $1)`,
			[]any{oldUID},
		},
	}

	for _, s := range stmts {
		if _, err := fx.pgPool.Exec(ctx, s.sql, s.args...); err != nil {
			return errors.Join(
				fmt.Errorf("seed %s: %w", s.name, err),
			)
		}
	}
	return nil
}
