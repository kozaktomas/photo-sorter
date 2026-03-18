# Spread View (Facing Pages Preview)

Add a two-page spread layout mode to the Preview tab showing facing pages side by side, simulating how the printed book looks when opened.

## Requirements

- A toggle button in the Preview tab switches between "single page" (current) and "spread" view
- In spread view, pages are displayed in pairs: left page (verso/even) + right page (recto/odd)
- The first page (page 1) is displayed alone on the right side (recto), with the left side showing a blank placeholder
- Subsequent pages pair as: [2,3], [4,5], [6,7], etc.
- The last page, if odd-numbered, is displayed alone on the left with a blank right side
- Each spread is wrapped in a container maintaining the double-page aspect ratio (2 * 297 / 210 for A4 landscape)
- A thin vertical line down the center represents the binding/spine
- Section dividers still appear between spreads when the section changes
- Margins are mirrored: inside margins (toward binding) are 20mm, outside margins are 12mm — visualized by slightly shifting pages inward

## UI Details

- Toggle button uses `BookOpen` icon from lucide-react, positioned in the Preview tab header
- Spread container: `bg-slate-950 border border-slate-700 rounded-lg` with `gap-1` between the two pages
- Binding line: `border-l border-slate-600` on the right page
- Blank page placeholder: `bg-slate-900` with subtle text "blank"
- Toggle state persisted to localStorage (key: `book-spread-${bookId}`)
- Default view remains single-page (existing behavior unchanged)

## Implementation Notes

- Modify `web/src/pages/BookEditor/PreviewTab.tsx`
- Add a `spreadMode` boolean state with localStorage persistence
- Create a `SpreadView` sub-component that takes the ordered pages array and renders pairs
- Reuse the existing `PreviewPage` component for each page in the spread
- Add i18n keys: `books.editor.spreadView`, `books.editor.singlePageView`, `books.editor.blankPage`
