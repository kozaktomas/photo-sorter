# Book Statistics Dashboard

A collapsible stats panel in the book editor showing key metrics about the book's current state.

## Requirements

- A toggle button in the editor header (next to the Export/Delete buttons) shows/hides a stats panel
- The panel displays in a compact grid layout with these metrics:
  - **Pages**: total page count
  - **Photos placed**: number of unique photos assigned to page slots
  - **Photos unassigned**: total section photos minus placed photos (across all sections)
  - **Slots filled**: filled slots / total slots (with percentage)
  - **Format distribution**: count of each page format used (e.g. "4L: 5, 2P: 3, FS: 2")
  - **Sections**: count of sections, with count of empty sections (no pages)
- All data is computed client-side from existing `book` and `sectionPhotos` state (no new API calls)
- Panel uses a dark background (`bg-slate-800/50 border border-slate-700 rounded-lg p-4`)
- Uses the existing `StatsGrid` component pattern (see `web/src/components/StatsGrid.tsx`)
- Toggle state persisted to localStorage (key: `book-stats-${bookId}`)

## UI Details

- Toggle button uses a `BarChart3` icon from lucide-react, placed in the editor header bar
- Stats panel appears below the tab bar, above the main content
- Grid uses 3 columns on desktop, 2 on mobile
- Each stat shows a label (text-xs text-slate-500) and a value (text-lg font-semibold)
- Fill rate percentage uses color coding: green >= 80%, amber >= 50%, red < 50%

## Implementation Notes

- New file: `web/src/pages/BookEditor/BookStatsPanel.tsx`
- Integrate into `BookEditor/index.tsx` with toggle state
- Add i18n keys under `books.editor.stats.*` namespace in both cs and en locales
