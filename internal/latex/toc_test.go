package latex

import (
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/database"
)

func TestBuildTOCData_GroupsByChapterAndComputesRanges(t *testing.T) {
	groups := []sectionGroup{
		{
			sectionID:    "s-intro",
			title:        "Úvod",
			chapterID:    "ch-intro",
			chapterTitle: "Úvod",
			pages:        []database.BookPage{{ID: "p1"}},
		},
		{
			sectionID:    "s-houses",
			title:        "Domy ve Veselici",
			chapterID:    "ch-rozvoj",
			chapterTitle: "Rozvoj obce",
			pages:        []database.BookPage{{ID: "p2"}, {ID: "p3"}, {ID: "p4"}},
		},
		{
			sectionID:    "s-water",
			title:        "Vodovod",
			chapterID:    "ch-rozvoj",
			chapterTitle: "Rozvoj obce",
			pages:        []database.BookPage{{ID: "p5"}},
		},
		{
			sectionID:    "s-orphan",
			title:        "Bez kapitoly",
			chapterID:    "",
			chapterTitle: "",
			pages:        []database.BookPage{{ID: "p6"}},
		},
	}
	ranges := map[string][2]int{
		"s-intro":  {1, 1},
		"s-houses": {2, 4},
		"s-water":  {5, 5},
		"s-orphan": {6, 6},
	}

	toc := buildTOCData(groups, ranges)

	if len(toc) != 3 {
		t.Fatalf("expected 3 chapter blocks, got %d: %+v", len(toc), toc)
	}
	if toc[0].Title != "Úvod" || len(toc[0].Sections) != 1 || toc[0].Sections[0].StartPage != 1 {
		t.Errorf("chapter 0 unexpected: %+v", toc[0])
	}
	if toc[1].Title != "Rozvoj obce" || len(toc[1].Sections) != 2 {
		t.Fatalf("chapter 1 unexpected: %+v", toc[1])
	}
	houses := toc[1].Sections[0]
	if houses.Title != "Domy ve Veselici" || houses.StartPage != 2 || houses.EndPage != 4 {
		t.Errorf("houses range = %d-%d, want 2-4 (title=%q)", houses.StartPage, houses.EndPage, houses.Title)
	}
	water := toc[1].Sections[1]
	if water.Title != "Vodovod" || water.StartPage != 5 || water.EndPage != 5 {
		t.Errorf("water range = %d-%d, want 5-5", water.StartPage, water.EndPage)
	}
	if toc[2].Title != "" || len(toc[2].Sections) != 1 {
		t.Errorf("orphan chapter block unexpected: %+v", toc[2])
	}
}

func TestBuildTOCData_SkipsHiddenChapters(t *testing.T) {
	groups := []sectionGroup{
		{
			sectionID: "s1", title: "Sec1",
			chapterID: "ch-a", chapterTitle: "A",
			pages: []database.BookPage{{ID: "p1"}},
		},
		{
			sectionID: "s2", title: "Sec2",
			chapterID:          "ch-hidden",
			chapterTitle:       "Hidden",
			chapterHideFromTOC: true,
			pages:              []database.BookPage{{ID: "p2"}, {ID: "p3"}},
		},
		{
			sectionID: "s3", title: "Sec3",
			chapterID: "ch-b", chapterTitle: "B",
			pages: []database.BookPage{{ID: "p4"}},
		},
	}
	ranges := map[string][2]int{
		"s1": {1, 1},
		"s2": {2, 3},
		"s3": {4, 4},
	}
	toc := buildTOCData(groups, ranges)
	if len(toc) != 2 {
		t.Fatalf("hidden chapter must be omitted; got %d entries: %+v", len(toc), toc)
	}
	titles := []string{toc[0].Title, toc[1].Title}
	if titles[0] != "A" || titles[1] != "B" {
		t.Errorf("unexpected chapter titles in TOC: %v", titles)
	}
}

func TestBuildTOCData_SkipsSectionsWithoutPageRange(t *testing.T) {
	groups := []sectionGroup{
		{
			sectionID:    "s-ghost",
			title:        "Prázdná",
			chapterID:    "ch-a",
			chapterTitle: "A",
			pages:        []database.BookPage{{ID: "p1"}},
		},
	}
	toc := buildTOCData(groups, map[string][2]int{})
	if len(toc) != 0 {
		t.Errorf("section without page range should produce empty TOC, got %+v", toc)
	}
}

func TestBalanceTOCColumns(t *testing.T) {
	tests := []struct {
		name         string
		chapters     []TOCChapter
		wantBreakIdx int // -1 means "no chapter should be flagged"
	}{
		{
			name:         "empty TOC",
			chapters:     nil,
			wantBreakIdx: -1,
		},
		{
			name: "single chapter needs no split",
			chapters: []TOCChapter{
				{Title: "Only", Sections: []TOCSection{{Title: "s1"}, {Title: "s2"}}},
			},
			wantBreakIdx: -1,
		},
		{
			name: "equal-size chapters split in the middle",
			chapters: []TOCChapter{
				{Title: "A", Sections: []TOCSection{{Title: "s1"}, {Title: "s2"}}}, // 3
				{Title: "B", Sections: []TOCSection{{Title: "s3"}, {Title: "s4"}}}, // 3
			},
			wantBreakIdx: 1,
		},
		{
			name: "picks closest-to-balanced split",
			chapters: []TOCChapter{
				{Title: "UVOD", Sections: make([]TOCSection, 2)},      // 3
				{Title: "ROZVOJ", Sections: make([]TOCSection, 12)},   // 13
				{Title: "LOKALITY", Sections: make([]TOCSection, 13)}, // 14
				{Title: "SPOLKY", Sections: make([]TOCSection, 5)},    // 6
				{Title: "TRADICE", Sections: make([]TOCSection, 18)},  // 19
			},
			// rows = [3, 13, 14, 6, 19], total 55
			// break at 3 → left = 3+13+14 = 30, right = 6+19 = 25, diff 5 ← best
			wantBreakIdx: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			balanceTOCColumns(tc.chapters)
			gotIdx := -1
			for i, ch := range tc.chapters {
				if ch.StartsRightColumn {
					if gotIdx != -1 {
						t.Fatalf("StartsRightColumn set on multiple chapters: %d and %d", gotIdx, i)
					}
					gotIdx = i
				}
			}
			if gotIdx != tc.wantBreakIdx {
				t.Errorf("break index = %d, want %d", gotIdx, tc.wantBreakIdx)
			}
		})
	}
}

func TestInjectContentsSlots_FillsOnlyContentsFlaggedSlots(t *testing.T) {
	sections := []TemplateSection{
		{
			Pages: []TemplatePage{
				{
					Slots: []TemplateSlot{
						{HasPhoto: true},
						{HasContents: true, ContentsHeader: "Obsah"},
						{HasText: true},
					},
				},
			},
		},
	}
	toc := []TOCChapter{{Title: "Kap", Sections: []TOCSection{{Title: "s", StartPage: 1, EndPage: 2}}}}

	injectContentsSlots(sections, toc)

	slots := sections[0].Pages[0].Slots
	if slots[0].ContentsEntries != nil {
		t.Errorf("photo slot must not receive TOC entries, got %+v", slots[0].ContentsEntries)
	}
	if len(slots[1].ContentsEntries) != 1 || slots[1].ContentsEntries[0].Title != "Kap" {
		t.Errorf("contents slot entries wrong: %+v", slots[1].ContentsEntries)
	}
	if slots[2].ContentsEntries != nil {
		t.Errorf("text slot must not receive TOC entries, got %+v", slots[2].ContentsEntries)
	}
}
