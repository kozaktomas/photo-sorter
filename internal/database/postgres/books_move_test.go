//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

// setupBookFixture creates a book with two sections and returns helper
// context for tests that exercise MovePageToSection. Sections are named
// "A" and "B"; both belong to the same book.
type moveFixture struct {
	repo      *BookRepository
	ctx       context.Context
	bookID    string
	sectionA  string
	sectionB  string
	otherBook string
	sectionC  string // belongs to the other book
}

func setupMoveFixture(t *testing.T) (*moveFixture, func()) {
	t.Helper()
	pool, cleanup := setupTestContainer(t)
	if pool == nil {
		return nil, func() {}
	}
	ctx := context.Background()
	repo := NewBookRepository(pool)

	book := &database.PhotoBook{Title: "Primary"}
	if err := repo.CreateBook(ctx, book); err != nil {
		cleanup()
		t.Fatalf("create primary book: %v", err)
	}
	other := &database.PhotoBook{Title: "Other"}
	if err := repo.CreateBook(ctx, other); err != nil {
		cleanup()
		t.Fatalf("create other book: %v", err)
	}
	sectionA := &database.BookSection{BookID: book.ID, Title: "A"}
	if err := repo.CreateSection(ctx, sectionA); err != nil {
		cleanup()
		t.Fatalf("create section A: %v", err)
	}
	sectionB := &database.BookSection{BookID: book.ID, Title: "B"}
	if err := repo.CreateSection(ctx, sectionB); err != nil {
		cleanup()
		t.Fatalf("create section B: %v", err)
	}
	sectionC := &database.BookSection{BookID: other.ID, Title: "C"}
	if err := repo.CreateSection(ctx, sectionC); err != nil {
		cleanup()
		t.Fatalf("create section C: %v", err)
	}
	return &moveFixture{
		repo: repo, ctx: ctx,
		bookID:    book.ID,
		sectionA:  sectionA.ID,
		sectionB:  sectionB.ID,
		otherBook: other.ID,
		sectionC:  sectionC.ID,
	}, cleanup
}

// makePage creates a page in the fixture's primary book with a single
// landscape format and populates the given photo UIDs into its slots
// (one per slot, up to 4).
func (f *moveFixture) makePage(t *testing.T, sectionID string, photoUIDs ...string) *database.BookPage {
	t.Helper()
	page := &database.BookPage{BookID: f.bookID, SectionID: sectionID, Format: "4_landscape"}
	if err := f.repo.CreatePage(f.ctx, page); err != nil {
		t.Fatalf("create page: %v", err)
	}
	for i, uid := range photoUIDs {
		if i >= 4 {
			break
		}
		if err := f.repo.AssignSlot(f.ctx, page.ID, i, uid); err != nil {
			t.Fatalf("assign slot %d: %v", i, err)
		}
		if err := f.repo.AddSectionPhotos(f.ctx, sectionID, []string{uid}); err != nil {
			t.Fatalf("add section photo: %v", err)
		}
	}
	return page
}

// photoUIDsIn returns the set of photo UIDs currently in a section's pool.
func (f *moveFixture) photoUIDsIn(t *testing.T, sectionID string) map[string]bool {
	t.Helper()
	photos, err := f.repo.GetSectionPhotos(f.ctx, sectionID)
	if err != nil {
		t.Fatalf("get section photos: %v", err)
	}
	out := make(map[string]bool, len(photos))
	for _, p := range photos {
		out[p.PhotoUID] = true
	}
	return out
}

func TestMovePageToSection_MovesPageAndPhotos(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	page := f.makePage(t, f.sectionA, "photoA1", "photoA2")

	if err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionB); err != nil {
		t.Fatalf("MovePageToSection: %v", err)
	}

	got, err := f.repo.GetPage(f.ctx, page.ID)
	if err != nil || got == nil {
		t.Fatalf("get page after move: %v", err)
	}
	if got.SectionID != f.sectionB {
		t.Errorf("page.SectionID: got %q want %q", got.SectionID, f.sectionB)
	}
	if len(got.Slots) != 2 {
		t.Errorf("expected 2 slots preserved, got %d", len(got.Slots))
	}
	bPool := f.photoUIDsIn(t, f.sectionB)
	if !bPool["photoA1"] || !bPool["photoA2"] {
		t.Errorf("target pool missing moved photos: %+v", bPool)
	}
	aPool := f.photoUIDsIn(t, f.sectionA)
	if aPool["photoA1"] || aPool["photoA2"] {
		t.Errorf("source pool still holds moved photos: %+v", aPool)
	}
}

func TestMovePageToSection_PhotoSharedWithAnotherPageStays(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	// Two pages in section A both reference photoShared.
	movedPage := f.makePage(t, f.sectionA, "photoShared", "photoUnique")
	_ = f.makePage(t, f.sectionA, "photoShared")

	if err := f.repo.MovePageToSection(f.ctx, movedPage.ID, f.sectionB); err != nil {
		t.Fatalf("MovePageToSection: %v", err)
	}

	aPool := f.photoUIDsIn(t, f.sectionA)
	if !aPool["photoShared"] {
		t.Error("photoShared must stay in source pool — still used by other page")
	}
	if aPool["photoUnique"] {
		t.Error("photoUnique must be removed from source — no other page uses it")
	}
	bPool := f.photoUIDsIn(t, f.sectionB)
	if !bPool["photoShared"] || !bPool["photoUnique"] {
		t.Errorf("target pool missing photos: %+v", bPool)
	}
}

func TestMovePageToSection_CarriesDescriptionAndNote(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	page := f.makePage(t, f.sectionA, "photoCarry")
	if err := f.repo.UpdateSectionPhoto(f.ctx, f.sectionA, "photoCarry",
		"Original description", "Original note"); err != nil {
		t.Fatalf("seed description/note: %v", err)
	}

	if err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionB); err != nil {
		t.Fatalf("MovePageToSection: %v", err)
	}

	photos, err := f.repo.GetSectionPhotos(f.ctx, f.sectionB)
	if err != nil {
		t.Fatalf("get target photos: %v", err)
	}
	var carried *database.SectionPhoto
	for i := range photos {
		if photos[i].PhotoUID == "photoCarry" {
			carried = &photos[i]
			break
		}
	}
	if carried == nil {
		t.Fatal("photoCarry not found in target pool")
	}
	if carried.Description != "Original description" {
		t.Errorf("description: got %q want %q", carried.Description, "Original description")
	}
	if carried.Note != "Original note" {
		t.Errorf("note: got %q want %q", carried.Note, "Original note")
	}
}

func TestMovePageToSection_KeepsExistingTargetDescription(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	// Both sections already have photoX, each with their own description.
	_ = f.makePage(t, f.sectionB, "photoX")
	if err := f.repo.UpdateSectionPhoto(f.ctx, f.sectionB, "photoX",
		"Target desc", "Target note"); err != nil {
		t.Fatalf("seed target desc: %v", err)
	}
	page := f.makePage(t, f.sectionA, "photoX")
	if err := f.repo.UpdateSectionPhoto(f.ctx, f.sectionA, "photoX",
		"Source desc", "Source note"); err != nil {
		t.Fatalf("seed source desc: %v", err)
	}

	if err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionB); err != nil {
		t.Fatalf("MovePageToSection: %v", err)
	}

	photos, err := f.repo.GetSectionPhotos(f.ctx, f.sectionB)
	if err != nil {
		t.Fatalf("get target photos: %v", err)
	}
	var row *database.SectionPhoto
	for i := range photos {
		if photos[i].PhotoUID == "photoX" {
			row = &photos[i]
			break
		}
	}
	if row == nil {
		t.Fatal("photoX missing in target pool")
	}
	if row.Description != "Target desc" || row.Note != "Target note" {
		t.Errorf("target description/note overwritten: %+v", row)
	}
}

func TestMovePageToSection_AppendsAtEndOfTargetSection(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	// Target section has two existing pages.
	_ = f.makePage(t, f.sectionB)
	_ = f.makePage(t, f.sectionB)
	page := f.makePage(t, f.sectionA, "photo")

	if err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionB); err != nil {
		t.Fatalf("MovePageToSection: %v", err)
	}

	pages, err := f.repo.GetPages(f.ctx, f.bookID)
	if err != nil {
		t.Fatalf("get pages: %v", err)
	}
	var sectionBPages []database.BookPage
	for _, p := range pages {
		if p.SectionID == f.sectionB {
			sectionBPages = append(sectionBPages, p)
		}
	}
	if len(sectionBPages) != 3 {
		t.Fatalf("expected 3 pages in section B, got %d", len(sectionBPages))
	}
	if sectionBPages[2].ID != page.ID {
		t.Errorf("moved page not at end: got %q, want %q",
			sectionBPages[2].ID, page.ID)
	}
}

func TestMovePageToSection_SameSectionNoOp(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	page := f.makePage(t, f.sectionA, "photoNoop")
	originalOrder := page.SortOrder

	if err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionA); err != nil {
		t.Fatalf("MovePageToSection same section: %v", err)
	}
	got, err := f.repo.GetPage(f.ctx, page.ID)
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	if got.SectionID != f.sectionA {
		t.Errorf("section changed unexpectedly: %q", got.SectionID)
	}
	if got.SortOrder != originalOrder {
		t.Errorf("sort order changed unexpectedly: %d -> %d", originalOrder, got.SortOrder)
	}
}

func TestMovePageToSection_DifferentBookRejected(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	page := f.makePage(t, f.sectionA, "photoMismatch")
	err := f.repo.MovePageToSection(f.ctx, page.ID, f.sectionC)
	if !errors.Is(err, database.ErrSectionBookMismatch) {
		t.Fatalf("expected ErrSectionBookMismatch, got %v", err)
	}
	// Page stays in source.
	got, _ := f.repo.GetPage(f.ctx, page.ID)
	if got.SectionID != f.sectionA {
		t.Errorf("page moved despite error: %q", got.SectionID)
	}
}

func TestMovePageToSection_UnknownPageAndSection(t *testing.T) {
	f, cleanup := setupMoveFixture(t)
	if f == nil {
		return
	}
	defer cleanup()

	err := f.repo.MovePageToSection(f.ctx, "does-not-exist", f.sectionB)
	if !errors.Is(err, database.ErrPageNotFound) {
		t.Errorf("expected ErrPageNotFound, got %v", err)
	}
	page := f.makePage(t, f.sectionA, "p")
	err = f.repo.MovePageToSection(f.ctx, page.ID, "section-does-not-exist")
	if !errors.Is(err, database.ErrSectionNotFound) {
		t.Errorf("expected ErrSectionNotFound, got %v", err)
	}
}
