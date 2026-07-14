//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kozaktomas/photo-sorter/internal/auth"
	"github.com/kozaktomas/photo-sorter/internal/database"
)

// touchPhoto forces a photo's updated_at to an explicit instant. The normal
// write path stamps NOW(), which is useless for asserting an ordering, so the
// tests below drive the column directly.
func touchPhoto(t *testing.T, pool *Pool, uid string, updatedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE photos SET updated_at = $1 WHERE uid = $2`, updatedAt, uid); err != nil {
		t.Fatalf("touch photo %s: %v", uid, err)
	}
}

// seedExportPhotos inserts n photos and pins their updated_at so that photos
// i and i+1 share a timestamp in pairs — exercising the (updated_at, uid)
// tiebreak that a timestamp-only cursor would get wrong.
func seedExportPhotos(t *testing.T, pool *Pool, repo *PhotoRepository, n int) []string {
	t.Helper()
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	uids := make([]string, 0, n)
	for i := range n {
		p := makePhoto(fmt.Sprintf("hash-export-%02d", i), fmt.Sprintf("export%02d.jpg", i))
		p.UID = fmt.Sprintf("pexport%02d", i)
		if err := repo.CreatePhoto(context.Background(), p); err != nil {
			t.Fatalf("CreatePhoto %d: %v", i, err)
		}
		touchPhoto(t, pool, p.UID, base.Add(time.Duration(i/2)*time.Minute))
		uids = append(uids, p.UID)
	}
	return uids
}

// walkWithCursor pages through the whole result set using the keyset cursor
// and returns the UIDs in the order they came back.
func walkWithCursor(
	t *testing.T, repo *PhotoRepository, filter database.PhotoFilter, limit int,
) []string {
	t.Helper()
	ctx := context.Background()
	filter.SortBy = database.SortUpdated
	filter.Limit = limit

	var got []string
	for page := 0; page < 100; page++ {
		photos, _, err := repo.ListPhotos(ctx, filter)
		if err != nil {
			t.Fatalf("ListPhotos page %d: %v", page, err)
		}
		for _, p := range photos {
			got = append(got, p.UID)
		}
		if len(photos) < limit {
			return got
		}
		last := photos[len(photos)-1]
		filter.Cursor = &database.PhotoCursor{UpdatedAt: last.UpdatedAt, UID: last.UID}
	}
	t.Fatal("cursor walk did not terminate within 100 pages")
	return got
}

// TestListPhotos_KeysetCursor_NoGapsNoRepeats exercises the REAL SQL row-value
// predicate `(updated_at, uid) > ($1, $2)` against Postgres. The handler tests
// use a Go re-implementation of these semantics, so this is the test that
// proves the two agree.
func TestListPhotos_KeysetCursor_NoGapsNoRepeats(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	repo := NewPhotoRepository(pool)
	const total = 17
	seedExportPhotos(t, pool, repo, total)

	got := walkWithCursor(t, repo, database.PhotoFilter{}, 5)

	seen := map[string]int{}
	for _, uid := range got {
		seen[uid]++
	}
	if len(seen) != total {
		t.Errorf("walked %d distinct photos, want %d — the keyset has gaps", len(seen), total)
	}
	for uid, n := range seen {
		if n != 1 {
			t.Errorf("photo %s came back %d times, want 1", uid, n)
		}
	}

	// Ordering must be strictly ascending in (updated_at, uid). Since the
	// seeds pair up timestamps, this only holds if the uid tiebreak works.
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("order broke at %d: %q then %q — the uid tiebreak is not applied",
				i, got[i-1], got[i])
		}
	}
}

// TestListPhotos_KeysetCursor_StableUnderConcurrentWrite is the property the
// spec demands: a photo written mid-walk must not be lost.
func TestListPhotos_KeysetCursor_StableUnderConcurrentWrite(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)
	seedExportPhotos(t, pool, repo, 10)

	// Page 1.
	filter := database.PhotoFilter{SortBy: database.SortUpdated, Limit: 4}
	first, total, err := repo.ListPhotos(ctx, filter)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 10 {
		t.Errorf("total = %d, want 10 (the count must ignore the cursor)", total)
	}
	if len(first) != 4 {
		t.Fatalf("page 1 returned %d photos, want 4", len(first))
	}
	last := first[len(first)-1]
	filter.Cursor = &database.PhotoCursor{UpdatedAt: last.UpdatedAt, UID: last.UID}

	// Mid-walk, an ALREADY-SEEN photo is written. Its updated_at jumps ahead
	// of everything, so an ascending walk must serve it again rather than
	// drop it.
	touchPhoto(t, pool, "pexport00", time.Date(2024, 6, 1, 23, 0, 0, 0, time.UTC))

	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		photos, _, err := repo.ListPhotos(ctx, filter)
		if err != nil {
			t.Fatalf("resume page %d: %v", page, err)
		}
		for _, p := range photos {
			seen[p.UID] = true
		}
		if len(photos) < filter.Limit {
			break
		}
		lastPhoto := photos[len(photos)-1]
		filter.Cursor = &database.PhotoCursor{
			UpdatedAt: lastPhoto.UpdatedAt, UID: lastPhoto.UID,
		}
	}

	// Everything the client had not yet seen must arrive.
	for i := 4; i < 10; i++ {
		uid := fmt.Sprintf("pexport%02d", i)
		if !seen[uid] {
			t.Errorf("photo %s was never delivered — the export lost a row", uid)
		}
	}
	// And the row rewritten mid-walk must resurface.
	if !seen["pexport00"] {
		t.Error("pexport00 was rewritten mid-walk and never resurfaced; " +
			"an ascending updated_at walk must re-serve it")
	}
}

func TestListPhotos_UpdatedSince(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)
	seedExportPhotos(t, pool, repo, 6) // updated_at = base + (i/2) minutes

	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	since := base.Add(time.Minute) // photos 2..5 (i/2 >= 1)

	photos, total, err := repo.ListPhotos(ctx, database.PhotoFilter{
		SortBy:       database.SortUpdated,
		UpdatedSince: &since,
	})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if len(photos) != 4 {
		t.Fatalf("got %d photos, want 4", len(photos))
	}
	// Inclusive bound: the rows exactly AT `since` must be present.
	if photos[0].UID != "pexport02" {
		t.Errorf("first photo = %s, want pexport02 — the bound must be >=, not >", photos[0].UID)
	}
}

// TestListPhotos_CursorIgnoredWithoutUpdatedSort guards the repository-level
// belt to the handler's braces: a cursor under a non-updated sort must not
// silently filter rows out.
func TestListPhotos_CursorIgnoredWithoutUpdatedSort(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)
	seedExportPhotos(t, pool, repo, 5)

	photos, _, err := repo.ListPhotos(ctx, database.PhotoFilter{
		SortBy: "newest",
		Cursor: &database.PhotoCursor{
			UpdatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			UID:       "pzzzz",
		},
	})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	// A far-future cursor would exclude everything if it were applied.
	if len(photos) != 5 {
		t.Errorf("got %d photos, want 5 — the cursor must be ignored unless sort=updated",
			len(photos))
	}
}

// TestListPhotos_UpdatedSortIgnoresSearchRank pins the interaction the search
// ranking would otherwise break: under sort=updated the `q` filter still
// restricts rows, but ts_rank must NOT lead the ORDER BY, or the keyset would
// address a position in an ordering the cursor knows nothing about.
func TestListPhotos_UpdatedSortIgnoresSearchRank(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		p := makePhoto(fmt.Sprintf("hash-fts-%d", i), fmt.Sprintf("fts%d.jpg", i))
		p.UID = fmt.Sprintf("pfts%02d", i)
		p.Title = "svatba veselice"
		if err := repo.CreatePhoto(ctx, p); err != nil {
			t.Fatalf("CreatePhoto: %v", err)
		}
		// Reverse the updated_at order relative to the UID order, so a
		// correct (updated_at, uid) sort is visibly different from any
		// relevance-driven one.
		touchPhoto(t, pool, p.UID, base.Add(time.Duration(3-i)*time.Minute))
	}

	photos, _, err := repo.ListPhotos(ctx, database.PhotoFilter{
		SortBy: database.SortUpdated,
		Search: "svatba",
	})
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if len(photos) != 3 {
		t.Fatalf("got %d photos, want 3 — the search filter must still apply", len(photos))
	}
	// Ascending updated_at means the reverse of the UID order.
	want := []string{"pfts02", "pfts01", "pfts00"}
	for i, uid := range want {
		if photos[i].UID != uid {
			t.Errorf("photos[%d] = %s, want %s — sort=updated must order by "+
				"(updated_at, uid), not by ts_rank", i, photos[i].UID, uid)
		}
	}
}

// TestLoadPhotoRelations exercises the bulk relation loaders against real
// tables, including the nullable marker.subject_uid.
func TestLoadPhotoRelations(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)

	p1 := makePhoto("hash-rel-1", "rel1.jpg")
	p1.UID = "prel00000000001"
	p2 := makePhoto("hash-rel-2", "rel2.jpg")
	p2.UID = "prel00000000002"
	for _, p := range []*database.Photo{p1, p2} {
		if err := repo.CreatePhoto(ctx, p); err != nil {
			t.Fatalf("CreatePhoto %s: %v", p.UID, err)
		}
	}

	// A label with provenance, an album, two markers (one unassigned), and a
	// sidecar file — all hung off p1 only.
	mustExec(t, pool,
		`INSERT INTO labels (uid, slug, name) VALUES ('lrel1', 'svatba', 'svatba')`)
	mustExec(t, pool,
		`INSERT INTO photo_labels (photo_uid, label_uid, source, uncertainty)
		 VALUES ($1, 'lrel1', 'ai', 25)`, p1.UID)
	mustExec(t, pool,
		`INSERT INTO albums (uid, slug, title) VALUES ('arel1', 'veselice', 'Veselice 1998')`)
	mustExec(t, pool,
		`INSERT INTO album_photos (album_uid, photo_uid) VALUES ('arel1', $1)`, p1.UID)
	mustExec(t, pool,
		`INSERT INTO subjects (uid, slug, name) VALUES ('srel1', 'jana', 'Jana')`)
	mustExec(t, pool,
		`INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h, score, reviewed)
		 VALUES ('mrel1', $1, 'srel1', 'face', 0.1, 0.2, 0.3, 0.4, 90, true)`, p1.UID)
	mustExec(t, pool,
		`INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
		 VALUES ('mrel2', $1, NULL, 'face', 0.5, 0.6, 0.1, 0.1)`, p1.UID)
	mustExec(t, pool,
		`INSERT INTO photo_files (photo_uid, file_path, file_hash, file_size, file_mime, is_primary, role)
		 VALUES ($1, '2024/06/rel1.cr2', 'raw-hash', 999, 'image/x-canon-cr2', false, 'sidecar')`,
		p1.UID)

	all := database.RelationSet{Labels: true, Albums: true, Markers: true, Files: true}
	rels, err := repo.LoadPhotoRelations(ctx, []string{p1.UID, p2.UID}, all)
	if err != nil {
		t.Fatalf("LoadPhotoRelations: %v", err)
	}

	r1 := rels[p1.UID]
	if len(r1.Labels) != 1 || r1.Labels[0].Source != "ai" || r1.Labels[0].Uncertainty != 25 {
		t.Errorf("labels = %+v, want one ai/25 label", r1.Labels)
	}
	if len(r1.Albums) != 1 || r1.Albums[0].Title != "Veselice 1998" {
		t.Errorf("albums = %+v", r1.Albums)
	}
	if len(r1.Markers) != 2 {
		t.Fatalf("markers = %+v, want 2", r1.Markers)
	}
	var assigned, unassigned *database.PhotoMarkerRelation
	for i := range r1.Markers {
		if r1.Markers[i].UID == "mrel1" {
			assigned = &r1.Markers[i]
		} else {
			unassigned = &r1.Markers[i]
		}
	}
	if assigned == nil || assigned.SubjectUID != "srel1" || !assigned.Reviewed {
		t.Errorf("assigned marker = %+v, want subject srel1 and reviewed", assigned)
	}
	// A NULL subject_uid must land as "" rather than blowing up the scan.
	if unassigned == nil || unassigned.SubjectUID != "" {
		t.Errorf("unassigned marker = %+v, want an empty subject_uid", unassigned)
	}
	if len(r1.Files) != 1 || r1.Files[0].Role != "sidecar" {
		t.Errorf("files = %+v, want the sidecar", r1.Files)
	}

	// p2 has nothing, but every relation was requested — each must be an
	// empty non-nil slice, which is what tells an importer "none" rather than
	// "not fetched".
	r2 := rels[p2.UID]
	if r2 == nil {
		t.Fatal("photo with no relations is missing from the result map")
	}
	if r2.Labels == nil || r2.Albums == nil || r2.Markers == nil || r2.Files == nil {
		t.Errorf("requested-but-empty relations must be non-nil slices, got %+v", r2)
	}
	if len(r2.Labels) != 0 {
		t.Errorf("p2 labels = %+v, want empty", r2.Labels)
	}
}

// TestLoadPhotoRelations_UnrequestedStayNil is the other half of the contract.
func TestLoadPhotoRelations_UnrequestedStayNil(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewPhotoRepository(pool)
	p := makePhoto("hash-nil-1", "nil1.jpg")
	p.UID = "pnil000000000001"
	if err := repo.CreatePhoto(ctx, p); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	rels, err := repo.LoadPhotoRelations(ctx, []string{p.UID}, database.RelationSet{Labels: true})
	if err != nil {
		t.Fatalf("LoadPhotoRelations: %v", err)
	}
	r := rels[p.UID]
	if r.Labels == nil {
		t.Error("requested relation is nil, want an empty slice")
	}
	if r.Albums != nil || r.Markers != nil || r.Files != nil {
		t.Errorf("unrequested relations must stay nil, got %+v", r)
	}
}

// --- API tokens ---

func TestAPITokenRepository_ResolveLifecycle(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewAPITokenRepository(pool)

	raw, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	token := &database.APIToken{
		UID:       NewAPITokenUID(),
		Name:      "kukatko-migration",
		TokenHash: hash,
		Scope:     auth.APITokenScopeRead,
	}
	if err := repo.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	// A live token resolves.
	got, err := repo.ResolveAPIToken(ctx, raw)
	if err != nil {
		t.Fatalf("ResolveAPIToken: %v", err)
	}
	if got == nil {
		t.Fatal("a freshly minted token did not resolve")
	}
	if got.UID != token.UID || got.Scope != auth.APITokenScopeRead {
		t.Errorf("resolved %+v, want uid %s scope read", got, token.UID)
	}

	// A wrong token does not — and does so as (nil, nil), not an error.
	missing, err := repo.ResolveAPIToken(ctx, "psat_not-a-real-token")
	if err != nil {
		t.Errorf("ResolveAPIToken(unknown) error = %v, want nil", err)
	}
	if missing != nil {
		t.Error("an unknown token resolved")
	}

	// Touch advances last_used_at.
	if err := repo.TouchAPIToken(ctx, token.UID); err != nil {
		t.Fatalf("TouchAPIToken: %v", err)
	}
	touched, err := repo.GetAPITokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	if touched.LastUsedAt == nil {
		t.Error("last_used_at is still NULL after TouchAPIToken")
	}

	// Revoke takes effect immediately.
	if err := repo.RevokeAPIToken(ctx, token.UID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	revoked, err := repo.ResolveAPIToken(ctx, raw)
	if err != nil {
		t.Fatalf("ResolveAPIToken after revoke: %v", err)
	}
	if revoked != nil {
		t.Error("a revoked token still resolves — revocation is not enforced")
	}

	// Revoking twice is a no-op success; revoking a ghost is ErrNotFound.
	if err := repo.RevokeAPIToken(ctx, token.UID); err != nil {
		t.Errorf("second RevokeAPIToken = %v, want nil (idempotent)", err)
	}
	if err := repo.RevokeAPIToken(ctx, "tnonexistent"); err == nil {
		t.Error("revoking an unknown UID succeeded, want ErrNotFound")
	}
}

// TestAPITokenRepository_ExpiredDoesNotResolve pins that expiry is enforced in
// SQL against the database clock, not in Go.
func TestAPITokenRepository_ExpiredDoesNotResolve(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()

	ctx := context.Background()
	repo := NewAPITokenRepository(pool)

	raw, hash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := repo.CreateAPIToken(ctx, &database.APIToken{
		UID:       NewAPITokenUID(),
		Name:      "expired",
		TokenHash: hash,
		Scope:     auth.APITokenScopeRead,
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	got, err := repo.ResolveAPIToken(ctx, raw)
	if err != nil {
		t.Fatalf("ResolveAPIToken: %v", err)
	}
	if got != nil {
		t.Error("an expired token resolved")
	}
	// It is still visible to the management surface.
	if _, err := repo.GetAPITokenByHash(ctx, hash); err != nil {
		t.Errorf("GetAPITokenByHash on an expired token = %v, want it to be listable", err)
	}
}

// mustExec runs a statement or fails the test.
func mustExec(t *testing.T, pool *Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
