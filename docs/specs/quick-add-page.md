# Quick Add Page Button

Add a `+` button next to each section name in the Pages tab sidebar for rapid page creation without scrolling to the bottom form.

## Requirements

- A small `+` icon button appears next to each section header in `PageSidebar` (between the section title and the page count)
- Clicking the `+` opens a compact dropdown/popover with the 5 page format options: 4 Landscape, 2L+1P, 1P+2L, 2 Portrait, 1 Fullscreen
- Selecting a format immediately creates a new page in that section and refreshes the book data
- The newly created page is auto-selected after creation
- Only one section's popover can be open at a time
- Clicking outside the popover or pressing Escape closes it
- The `+` button click must not trigger section collapse/expand (use `e.stopPropagation()`)

## UI Details

- The `+` button is small (h-4 w-4) with slate-500 color, hover to rose-400
- The popover appears below the section header, positioned absolutely
- Each format option shows the localized format name (reuse existing i18n keys like `books.editor.format4Landscape`)
- Format options have hover highlight and are clickable
- Popover has dark background (bg-slate-800) with border, matching the existing UI style
