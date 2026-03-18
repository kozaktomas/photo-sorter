# Auto-Layout UI in Pages Tab

Add a button to automatically generate pages from unassigned photos in the current section.

## Requirements

- A "Auto-layout" button appears in the Pages tab when a section is selected and has unassigned photos
- Button is positioned in the section header area (near the "Add Page" controls)
- Clicking the button:
  1. Calls `POST /api/v1/books/{bookId}/sections/{sectionId}/auto-layout`
  2. Shows a loading spinner while the request is in progress
  3. On success, refreshes the book data to show the new pages
  4. Shows a brief success message: "Created N pages with M photos"
- The button is disabled when there are no unassigned photos in the selected section
- The button is disabled while a request is in progress

## UI Details

- Button uses `Wand2` icon from lucide-react with label text
- Style: secondary button (`bg-slate-700 hover:bg-slate-600 text-slate-200`)
- Positioned next to existing page creation controls in the section header
- Success toast/message appears for 3 seconds then fades
- Loading state shows `Loader2` spinner icon

## Implementation Notes

- Add `autoLayoutSection(bookId, sectionId)` method to `web/src/api/client.ts`
- Modify `web/src/pages/BookEditor/PagesTab.tsx` to add the button in the section header area
- The response type: `{ pages_created: number, photos_placed: number, pages: BookPage[] }`
- After success, call `onRefresh()` to reload book data
- Add i18n keys: `books.editor.autoLayout`, `books.editor.autoLayoutSuccess`, `books.editor.autoLayoutEmpty`
