# Page Minimap / Overview

A bird's-eye view panel showing all pages as small thumbnails in a grid, for quick navigation and visual overview of the book layout.

## Requirements

- A toggle button in the Pages tab header area opens/closes the minimap panel
- The minimap shows all pages as miniature cards in a responsive grid
- Each miniature card (~80x56px, matching page aspect ratio) shows:
  - The page format layout with tiny colored rectangles for each slot
  - Filled slots show a micro-thumbnail of the assigned photo
  - Empty slots show a light dashed placeholder
  - Text slots show a subtle text pattern/icon
- Clicking a miniature card selects that page (calls `onSelect(pageId)`)
- The currently selected page is highlighted with a rose border
- Pages are visually grouped by section with section names as small labels above each group
- Incomplete pages (not all slots filled) have a subtle indicator (e.g. amber dot)

## UI Details

- The minimap panel appears between the tab bar and the main content area
- Panel has a dark background (bg-slate-900) with subtle border, max height ~200px with scroll if needed
- Toggle button uses a `Grid3x3` or `LayoutGrid` icon from lucide-react
- Grid uses `gap-2` spacing, wrapping naturally
- Section labels are `text-xs text-slate-500 uppercase`
- Miniature cards use the same completion color scheme as the sidebar (emerald border = complete)
- Use `getThumbnailUrl(uid, 'tile_50')` for the micro-thumbnails

## Implementation Notes

- New file: `web/src/pages/BookEditor/PageMinimap.tsx`
- Integrate into `PagesTab.tsx` with a toggle state
- Minimap visibility state can be persisted to localStorage (key: `book-minimap-${bookId}`)
- Add i18n keys for toggle button tooltip
