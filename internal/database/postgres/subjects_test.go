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

func TestNewSubjectUID(t *testing.T) {
	uid := NewSubjectUID()
	if !strings.HasPrefix(uid, "s") {
		t.Errorf("UID should start with 's', got %q", uid)
	}
	if len(uid) != 1+subjectUIDRandLen {
		t.Errorf("UID length = %d, want %d", len(uid), 1+subjectUIDRandLen)
	}
	if strings.ToLower(uid) != uid {
		t.Errorf("UID should be lowercase, got %q", uid)
	}
	seen := map[string]bool{uid: true}
	for i := 0; i < 64; i++ {
		next := NewSubjectUID()
		if seen[next] {
			t.Fatalf("collision after %d draws: %q", i, next)
		}
		seen[next] = true
	}
}

func TestSlugifySubjectName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Jan Novák", "jan-novak"},
		{"  Tomáš Křížek  ", "tomas-krizek"},
		{"  ---  ", "subject"},
		{"", "subject"},
		{"123_456!", "123-456"},
	}
	for _, c := range cases {
		got := slugifySubjectName(c.in)
		if got != c.want {
			t.Errorf("slugifySubjectName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSubjectRepository_EnsureSubject_Idempotent(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)

	first, err := repo.EnsureSubject(ctx, "Tomáš", "person")
	if err != nil {
		t.Fatalf("first EnsureSubject: %v", err)
	}
	if first.UID == "" || !strings.HasPrefix(first.UID, "s") {
		t.Errorf("UID not populated, got %q", first.UID)
	}
	if first.Slug != "tomas" {
		t.Errorf("Slug = %q, want tomas", first.Slug)
	}
	if first.Type != "person" {
		t.Errorf("Type = %q, want person", first.Type)
	}
	if first.CreatedAt.IsZero() {
		t.Error("CreatedAt not populated")
	}

	// Same name + same type — should return the same row.
	second, err := repo.EnsureSubject(ctx, "Tomáš", "person")
	if err != nil {
		t.Fatalf("second EnsureSubject: %v", err)
	}
	if second.UID != first.UID {
		t.Errorf("expected same UID, got %q vs %q", second.UID, first.UID)
	}

	// Accent-insensitive: "Tomas" should match "Tomáš".
	third, err := repo.EnsureSubject(ctx, "Tomas", "person")
	if err != nil {
		t.Fatalf("third EnsureSubject: %v", err)
	}
	if third.UID != first.UID {
		t.Errorf("accent variant should reuse row, got %q vs %q", third.UID, first.UID)
	}

	// Case-insensitive + surrounding whitespace.
	fourth, err := repo.EnsureSubject(ctx, "  TOMAS  ", "person")
	if err != nil {
		t.Fatalf("fourth EnsureSubject: %v", err)
	}
	if fourth.UID != first.UID {
		t.Errorf("case/whitespace variant should reuse row, got %q vs %q", fourth.UID, first.UID)
	}

	// Only one row should exist in the DB.
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM subjects`).Scan(&count); err != nil {
		t.Fatalf("count subjects: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 subjects row, got %d", count)
	}
}

func TestSubjectRepository_EnsureSubject_RaceSafe(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)

	const goroutines = 20
	var wg sync.WaitGroup
	uids := make([]string, goroutines)
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			s, err := repo.EnsureSubject(ctx, "Concurrent", "person")
			if err != nil {
				errs[i] = err
				return
			}
			uids[i] = s.UID
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
			t.Fatalf("expected all goroutines to converge on one UID, got %q and %q",
				first, uids[i])
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM subjects WHERE slug = 'concurrent'`).Scan(&count); err != nil {
		t.Fatalf("count concurrent rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 subjects row, got %d", count)
	}
}

func TestSubjectRepository_GetSubjectByName(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)

	created, err := repo.EnsureSubject(ctx, "Jiří Veselý", "person")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// Exact match.
	got, err := repo.GetSubjectByName(ctx, "Jiří Veselý")
	if err != nil {
		t.Fatalf("get by exact name: %v", err)
	}
	if got.UID != created.UID {
		t.Errorf("expected UID %q, got %q", created.UID, got.UID)
	}

	// Accent-stripped match.
	got, err = repo.GetSubjectByName(ctx, "Jiri Vesely")
	if err != nil {
		t.Fatalf("get by accent-stripped name: %v", err)
	}
	if got.UID != created.UID {
		t.Errorf("expected UID %q, got %q", created.UID, got.UID)
	}

	// Case variant.
	got, err = repo.GetSubjectByName(ctx, "JIŘÍ VESELÝ")
	if err != nil {
		t.Fatalf("get by upper-case name: %v", err)
	}
	if got.UID != created.UID {
		t.Errorf("expected UID %q, got %q", created.UID, got.UID)
	}

	// Missing.
	if _, err := repo.GetSubjectByName(ctx, "no such person"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("missing: got %v, want ErrNotFound", err)
	}
}

func TestSubjectRepository_UpdateSubject(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	s, err := repo.EnsureSubject(ctx, "Petr", "person")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	cover := createTestPhoto(t, photoRepo, "hash-subject-cover")

	s.Name = "Petr Novák"
	s.Slug = "" // Force re-slugging from the new name.
	s.Favorite = true
	s.Private = true
	s.Notes = "best friend"
	s.CoverPhotoUID = cover
	if err := repo.UpdateSubject(ctx, s); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetSubject(ctx, s.UID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Name != "Petr Novák" {
		t.Errorf("Name = %q, want Petr Novák", got.Name)
	}
	if got.Slug != "petr-novak" {
		t.Errorf("Slug = %q, want petr-novak", got.Slug)
	}
	if !got.Favorite {
		t.Error("Favorite not updated")
	}
	if !got.Private {
		t.Error("Private not updated")
	}
	if got.Notes != "best friend" {
		t.Errorf("Notes = %q, want %q", got.Notes, "best friend")
	}
	if got.CoverPhotoUID != cover {
		t.Errorf("CoverPhotoUID = %q, want %q", got.CoverPhotoUID, cover)
	}

	// Updating a missing subject returns ErrNotFound.
	missing := *s
	missing.UID = "no-such-subject"
	if err := repo.UpdateSubject(ctx, &missing); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("missing update: got %v, want ErrNotFound", err)
	}
}

func TestSubjectRepository_DeleteSubject_NotFound(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)

	if err := repo.DeleteSubject(ctx, "no-such-uid"); !errors.Is(err, database.ErrNotFound) {
		t.Errorf("delete missing: got %v, want ErrNotFound", err)
	}
}

func TestSubjectRepository_ListSubjects_FiltersAndSort(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)
	markerRepo := NewMarkerRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	alpha, err := repo.EnsureSubject(ctx, "Alpha Keyword", "person")
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	bravo, err := repo.EnsureSubject(ctx, "Bravo", "person")
	if err != nil {
		t.Fatalf("create bravo: %v", err)
	}
	charlie, err := repo.EnsureSubject(ctx, "Charlie", "pet")
	if err != nil {
		t.Fatalf("create charlie: %v", err)
	}

	// Toggle alpha to favorite for the favorite filter.
	alpha.Favorite = true
	if err := repo.UpdateSubject(ctx, alpha); err != nil {
		t.Fatalf("favorite alpha: %v", err)
	}

	// Attach markers so PhotoCount/FaceCount become observable.
	p1 := createTestPhoto(t, photoRepo, "hash-s1")
	p2 := createTestPhoto(t, photoRepo, "hash-s2")
	mk := func(photoUID, subjectUID string) {
		m := &database.Marker{
			PhotoUID:   photoUID,
			SubjectUID: subjectUID,
			Type:       "face",
			X:          0.1, Y: 0.1, W: 0.2, H: 0.2,
			Score: 90,
		}
		if err := markerRepo.CreateMarker(ctx, m); err != nil {
			t.Fatalf("create marker: %v", err)
		}
	}
	mk(p1, bravo.UID)
	mk(p2, bravo.UID)
	mk(p1, bravo.UID) // second face on p1 — face count rises but photo count stays
	mk(p1, alpha.UID)

	// Filter by type = "pet" keeps only charlie.
	listed, err := repo.ListSubjects(ctx, database.SubjectQuery{Type: "pet"})
	if err != nil {
		t.Fatalf("list type=pet: %v", err)
	}
	if len(listed) != 1 || listed[0].UID != charlie.UID {
		t.Errorf("type=pet filter wrong: %+v", listed)
	}

	// Filter by favorite = true keeps only alpha.
	truePtr := true
	listed, err = repo.ListSubjects(ctx, database.SubjectQuery{Favorite: &truePtr})
	if err != nil {
		t.Fatalf("list favorite=true: %v", err)
	}
	if len(listed) != 1 || listed[0].UID != alpha.UID {
		t.Errorf("favorite=true filter wrong: %+v", listed)
	}

	// Search keyword (accent-insensitive).
	listed, err = repo.ListSubjects(ctx, database.SubjectQuery{Search: "keyword"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if len(listed) != 1 || listed[0].UID != alpha.UID {
		t.Errorf("search filter wrong: %+v", listed)
	}

	// Sort = name (default, ASC).
	listed, err = repo.ListSubjects(ctx, database.SubjectQuery{SortBy: "name"})
	if err != nil {
		t.Fatalf("list name: %v", err)
	}
	if len(listed) != 3 || listed[0].Name != "Alpha Keyword" || listed[2].Name != "Charlie" {
		t.Errorf("name ASC sort wrong: %+v", listed)
	}

	// Sort = photos: bravo (2) > alpha (1) > charlie (0).
	listed, err = repo.ListSubjects(ctx, database.SubjectQuery{SortBy: "photos"})
	if err != nil {
		t.Fatalf("list photos: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 subjects, got %d", len(listed))
	}
	if listed[0].UID != bravo.UID || listed[0].PhotoCount != 2 {
		t.Errorf("photos sort top: got UID=%q PhotoCount=%d, want bravo (2)",
			listed[0].UID, listed[0].PhotoCount)
	}
	if listed[1].UID != alpha.UID || listed[1].PhotoCount != 1 {
		t.Errorf("photos sort middle: got UID=%q PhotoCount=%d, want alpha (1)",
			listed[1].UID, listed[1].PhotoCount)
	}
	if listed[2].UID != charlie.UID || listed[2].PhotoCount != 0 {
		t.Errorf("photos sort tail: got UID=%q PhotoCount=%d, want charlie (0)",
			listed[2].UID, listed[2].PhotoCount)
	}

	// FaceCount: bravo should have 3 (p1×2 + p2×1), alpha 1.
	for _, s := range listed {
		switch s.UID {
		case bravo.UID:
			if s.FaceCount != 3 {
				t.Errorf("bravo FaceCount = %d, want 3", s.FaceCount)
			}
		case alpha.UID:
			if s.FaceCount != 1 {
				t.Errorf("alpha FaceCount = %d, want 1", s.FaceCount)
			}
		case charlie.UID:
			if s.FaceCount != 0 {
				t.Errorf("charlie FaceCount = %d, want 0", s.FaceCount)
			}
		}
	}

	// Sort = newest: charlie (last created) first.
	listed, err = repo.ListSubjects(ctx, database.SubjectQuery{SortBy: "newest"})
	if err != nil {
		t.Fatalf("list newest: %v", err)
	}
	if len(listed) != 3 || listed[0].UID != charlie.UID {
		t.Errorf("newest sort wrong: %+v", listed)
	}
}

func TestSubjectRepository_ListSubjectsForPhoto(t *testing.T) {
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return
	}
	defer cleanup()
	ctx := context.Background()
	repo := NewSubjectRepository(pool)
	markerRepo := NewMarkerRepository(pool)
	photoRepo := NewPhotoRepository(pool)

	a, err := repo.EnsureSubject(ctx, "Anna", "person")
	if err != nil {
		t.Fatalf("ensure a: %v", err)
	}
	b, err := repo.EnsureSubject(ctx, "Bára", "person")
	if err != nil {
		t.Fatalf("ensure b: %v", err)
	}
	if _, err := repo.EnsureSubject(ctx, "Cyril", "person"); err != nil {
		t.Fatalf("ensure c: %v", err)
	}

	p := createTestPhoto(t, photoRepo, "hash-subj-photo")
	mkAttach := func(subjectUID string, invalid bool) {
		m := &database.Marker{
			PhotoUID:   p,
			SubjectUID: subjectUID,
			Type:       "face",
			X:          0.1, Y: 0.1, W: 0.2, H: 0.2,
			Score: 80, Invalid: invalid,
		}
		if err := markerRepo.CreateMarker(ctx, m); err != nil {
			t.Fatalf("create marker: %v", err)
		}
	}
	mkAttach(a.UID, false)
	mkAttach(b.UID, true) // invalid — should be excluded from the membership list

	memberships, err := repo.ListSubjectsForPhoto(ctx, p)
	if err != nil {
		t.Fatalf("ListSubjectsForPhoto: %v", err)
	}
	if len(memberships) != 1 || memberships[0].UID != a.UID {
		t.Fatalf("expected only %q, got %+v", a.UID, memberships)
	}
}
