//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

func TestNewLabelUID(t *testing.T) {
	uid := NewLabelUID()
	if !strings.HasPrefix(uid, "l") {
		t.Errorf("UID should start with 'l', got %q", uid)
	}
	if len(uid) != 1+labelUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+labelUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewLabelUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

func TestSlugifyLabelName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Nature", "nature"},
		{"  Nature  ", "nature"},
		{"Příliš žluťoučký kůň", "prilis-zlutoucky-kun"},
		{"  ---  ", "label"},
		{"", "label"},
		{"123_456!", "123-456"},
	}
	for _, c := range cases {
		got := slugifyLabelName(c.in)
		if got != c.want {
			t.Errorf("slugifyLabelName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLabelRepository_CreateGetUpdateDelete(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)

	// EnsureLabel both creates and returns idempotently — covers the
	// "create" half of the round-trip.
	l, err := repo.EnsureLabel(ctx, "Nature")
	if err != nil {
		t.Fatalf("EnsureLabel: %v", err)
	}
	if l.UID == "" || !strings.HasPrefix(l.UID, "l") {
		t.Errorf("UID not populated, got %q", l.UID)
	}
	if l.Slug != "nature" {
		t.Errorf("Slug = %q, want nature", l.Slug)
	}
	if l.CreatedAt.IsZero() {
		t.Error("CreatedAt not populated")
	}

	got, err := repo.GetLabel(ctx, l.UID)
	if err != nil {
		t.Fatalf("GetLabel: %v", err)
	}
	if got.Name != "Nature" {
		t.Errorf("Name mismatch: %q", got.Name)
	}
	if got.PhotoCount != 0 {
		t.Errorf("PhotoCount = %d, want 0", got.PhotoCount)
	}

	bySlug, err := repo.GetLabelBySlug(ctx, "nature")
	if err != nil {
		t.Fatalf("GetLabelBySlug: %v", err)
	}
	if bySlug.UID != l.UID {
		t.Errorf("GetLabelBySlug UID mismatch: got %q want %q", bySlug.UID, l.UID)
	}

	// Update fields.
	l.Name = "Outdoor Nature"
	l.Favorite = true
	l.Priority = 10
	l.Slug = "" // Force re-slugging from the new name.
	if err := repo.UpdateLabel(ctx, l); err != nil {
		t.Fatalf("UpdateLabel: %v", err)
	}
	got, err = repo.GetLabel(ctx, l.UID)
	if err != nil {
		t.Fatalf("GetLabel after update: %v", err)
	}
	if got.Name != "Outdoor Nature" {
		t.Errorf("Name not updated: %q", got.Name)
	}
	if !got.Favorite {
		t.Error("Favorite not updated")
	}
	if got.Priority != 10 {
		t.Errorf("Priority = %d, want 10", got.Priority)
	}
	if got.Slug != "outdoor-nature" {
		t.Errorf("Slug = %q, want outdoor-nature", got.Slug)
	}

	// Delete cascades photo_labels via FK ON DELETE CASCADE.
	deleted, err := repo.DeleteLabels(ctx, []string{l.UID})
	if err != nil {
		t.Fatalf("DeleteLabels: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := repo.GetLabel(ctx, l.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("GetLabel after delete: got %v, want ErrNotFound", err)
	}

	// Updating a missing label returns ErrNotFound.
	l.UID = "no-such-label"
	if err := repo.UpdateLabel(ctx, l); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("UpdateLabel missing: got %v, want ErrNotFound", err)
	}
}

func TestLabelRepository_EnsureLabel_Idempotent(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)

	first, err := repo.EnsureLabel(ctx, "People")
	if err != nil {
		t.Fatalf("first EnsureLabel: %v", err)
	}
	second, err := repo.EnsureLabel(ctx, "People")
	if err != nil {
		t.Fatalf("second EnsureLabel: %v", err)
	}
	if first.UID != second.UID {
		t.Errorf("expected same UID across calls: %q vs %q", first.UID, second.UID)
	}

	// Different case / surrounding whitespace map to the same slug.
	third, err := repo.EnsureLabel(ctx, "  people  ")
	if err != nil {
		t.Fatalf("third EnsureLabel: %v", err)
	}
	if third.UID != first.UID {
		t.Errorf("case+whitespace variant should reuse row, got %q vs %q", third.UID, first.UID)
	}
}

func TestLabelRepository_EnsureLabel_RaceSafe(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)

	const goroutines = 20
	var wg sync.WaitGroup
	uids := make([]string, goroutines)
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			l, err := repo.EnsureLabel(ctx, "Concurrent")
			if err != nil {
				errs[i] = err
				return
			}
			uids[i] = l.UID
		}(i)
	}
	wg.Wait()

	first := ""
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if first == "" {
			first = uids[i]
			continue
		}
		if uids[i] != first {
			t.Fatalf("expected all goroutines to converge on one UID, got %q and %q", first, uids[i])
		}
	}

	// Only one row should exist in the DB after the race.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM labels WHERE slug = 'concurrent'`).Scan(&count); err != nil {
		t.Fatalf("count concurrent rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 labels row, got %d", count)
	}
}

func TestLabelRepository_ListLabels_FiltersAndSort(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	alpha, err := repo.EnsureLabel(ctx, "Alpha keyword")
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	bravo, err := repo.EnsureLabel(ctx, "Bravo")
	if err != nil {
		t.Fatalf("create bravo: %v", err)
	}
	if _, err := repo.EnsureLabel(ctx, "Charlie"); err != nil {
		t.Fatalf("create charlie: %v", err)
	}

	p1 := createTestPhoto(t, photoRepo, "hash-l1")
	p2 := createTestPhoto(t, photoRepo, "hash-l2")
	if err := repo.AddPhotoLabel(ctx, p1, bravo.UID, "manual", 0); err != nil {
		t.Fatalf("attach p1->bravo: %v", err)
	}
	if err := repo.AddPhotoLabel(ctx, p2, bravo.UID, "manual", 0); err != nil {
		t.Fatalf("attach p2->bravo: %v", err)
	}
	if err := repo.AddPhotoLabel(ctx, p1, alpha.UID, "manual", 0); err != nil {
		t.Fatalf("attach p1->alpha: %v", err)
	}

	// min_photos = 2 keeps only bravo.
	listed, err := repo.ListLabels(ctx, database.LabelQuery{MinPhotos: 2})
	if err != nil {
		t.Fatalf("list min_photos=2: %v", err)
	}
	if len(listed) != 1 || listed[0].UID != bravo.UID {
		t.Errorf("min_photos=2 wrong: %+v", listed)
	}

	// search keyword.
	listed, err = repo.ListLabels(ctx, database.LabelQuery{Search: "keyword"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if len(listed) != 1 || listed[0].UID != alpha.UID {
		t.Errorf("search filter wrong: %+v", listed)
	}

	// sort = name (default, ASC).
	listed, err = repo.ListLabels(ctx, database.LabelQuery{SortBy: "name"})
	if err != nil {
		t.Fatalf("list name: %v", err)
	}
	if len(listed) != 3 || listed[0].Name != "Alpha keyword" || listed[2].Name != "Charlie" {
		t.Errorf("name ASC sort wrong: %+v", listed)
	}

	// sort = -name (DESC).
	listed, err = repo.ListLabels(ctx, database.LabelQuery{SortBy: "-name"})
	if err != nil {
		t.Fatalf("list -name: %v", err)
	}
	if len(listed) != 3 || listed[0].Name != "Charlie" || listed[2].Name != "Alpha keyword" {
		t.Errorf("name DESC sort wrong: %+v", listed)
	}

	// sort = count (ASC: empty first, then heavy).
	listed, err = repo.ListLabels(ctx, database.LabelQuery{SortBy: "count"})
	if err != nil {
		t.Fatalf("list count: %v", err)
	}
	if len(listed) != 3 || listed[0].PhotoCount != 0 || listed[2].PhotoCount != 2 {
		t.Errorf("count ASC sort wrong: %+v", listed)
	}

	// sort = -count (DESC: heavy first, then empty).
	listed, err = repo.ListLabels(ctx, database.LabelQuery{SortBy: "-count"})
	if err != nil {
		t.Fatalf("list -count: %v", err)
	}
	if len(listed) != 3 || listed[0].UID != bravo.UID || listed[0].PhotoCount != 2 {
		t.Errorf("-count sort wrong: %+v", listed)
	}
}

func TestLabelRepository_AddPhotoLabel_Idempotent(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-add-idem")
	l, err := repo.EnsureLabel(ctx, "Idempotent")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := repo.AddPhotoLabel(ctx, p, l.UID, "manual", 0); err != nil {
			t.Fatalf("AddPhotoLabel iteration %d: %v", i, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM photo_labels WHERE photo_uid = $1 AND label_uid = $2`,
		p, l.UID).Scan(&count); err != nil {
		t.Fatalf("count photo_labels: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 photo_labels row after repeated adds, got %d", count)
	}

	// Removing a non-existent pair is a silent no-op.
	if err := repo.RemovePhotoLabel(ctx, "no-such-photo", l.UID); err != nil {
		t.Errorf("remove missing should be no-op, got %v", err)
	}

	// Removing the real pair drops the row.
	if err := repo.RemovePhotoLabel(ctx, p, l.UID); err != nil {
		t.Fatalf("RemovePhotoLabel: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM photo_labels WHERE photo_uid = $1 AND label_uid = $2`,
		p, l.UID).Scan(&count); err != nil {
		t.Fatalf("count photo_labels after remove: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 photo_labels rows after remove, got %d", count)
	}
}

func TestLabelRepository_DeleteLabels_MixedValid(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)

	a, err := repo.EnsureLabel(ctx, "DeleteOne")
	if err != nil {
		t.Fatalf("ensure a: %v", err)
	}
	b, err := repo.EnsureLabel(ctx, "DeleteTwo")
	if err != nil {
		t.Fatalf("ensure b: %v", err)
	}

	// Mix two real UIDs with two bogus ones — only two rows should drop.
	deleted, err := repo.DeleteLabels(ctx, []string{a.UID, "bogus-1", b.UID, "bogus-2"})
	if err != nil {
		t.Fatalf("DeleteLabels: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (only the two real UIDs existed)", deleted)
	}

	// Both real rows are gone.
	if _, err := repo.GetLabel(ctx, a.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("a still exists: %v", err)
	}
	if _, err := repo.GetLabel(ctx, b.UID); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("b still exists: %v", err)
	}

	// Empty UIDs slice is a no-op (no row count, no error).
	deleted, err = repo.DeleteLabels(ctx, nil)
	if err != nil {
		t.Errorf("DeleteLabels nil: %v", err)
	}
	if deleted != 0 {
		t.Errorf("DeleteLabels nil deleted = %d, want 0", deleted)
	}
}

func TestLabelRepository_ListLabelsForPhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	p := createTestPhoto(t, photoRepo, "hash-membership")
	a, err := repo.EnsureLabel(ctx, "One")
	if err != nil {
		t.Fatalf("ensure a: %v", err)
	}
	b, err := repo.EnsureLabel(ctx, "Two")
	if err != nil {
		t.Fatalf("ensure b: %v", err)
	}
	if _, err := repo.EnsureLabel(ctx, "Three"); err != nil {
		t.Fatalf("ensure c: %v", err)
	}
	if err := repo.AddPhotoLabel(ctx, p, a.UID, "manual", 0); err != nil {
		t.Fatalf("attach a: %v", err)
	}
	if err := repo.AddPhotoLabel(ctx, p, b.UID, "ai", 5); err != nil {
		t.Fatalf("attach b: %v", err)
	}

	memberships, err := repo.ListLabelsForPhoto(ctx, p)
	if err != nil {
		t.Fatalf("ListLabelsForPhoto: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(memberships))
	}
	got := map[string]bool{}
	for _, l := range memberships {
		got[l.UID] = true
		if l.PhotoCount != 1 {
			t.Errorf("PhotoCount = %d for %q, want 1", l.PhotoCount, l.Name)
		}
	}
	if !got[a.UID] || !got[b.UID] {
		t.Errorf("missing expected memberships: %v", memberships)
	}
}

func TestLabelRepository_UpdateLabel_SlugCollision(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewLabelRepository(pool)

	first, err := repo.EnsureLabel(ctx, "Holiday")
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	second, err := repo.EnsureLabel(ctx, "Trip")
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}

	// Renaming second to collide with first should produce a dedup suffix.
	second.Name = "Holiday"
	second.Slug = "" // Force re-slugging.
	if err := repo.UpdateLabel(ctx, second); err != nil {
		t.Fatalf("UpdateLabel collision: %v", err)
	}
	if second.Slug == first.Slug {
		t.Errorf("expected slug suffix, got identical %q", second.Slug)
	}
	if !strings.HasPrefix(second.Slug, "holiday-") {
		t.Errorf("expected suffix slug, got %q", second.Slug)
	}

	// Self-update should keep the existing slug.
	first.Name = "Holiday"
	first.Slug = ""
	if err := repo.UpdateLabel(ctx, first); err != nil {
		t.Fatalf("UpdateLabel self: %v", err)
	}
	if first.Slug != "holiday" {
		t.Errorf("self update slug = %q, want holiday", first.Slug)
	}
}
