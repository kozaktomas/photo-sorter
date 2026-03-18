# Cross-Page Duplicate Photo Detection

Extend the existing Duplicates tab to also detect photos assigned to multiple page slots, not just photos in multiple sections.

## Requirements

- The Duplicates tab shows two separate groups:
  1. **Cross-section duplicates** (existing behavior): photos appearing in 2+ section photo pools
  2. **Cross-page duplicates** (new): photos assigned to 2+ page slots anywhere in the book
- Cross-page duplicates show:
  - Photo thumbnail
  - List of pages where the photo appears, with: section name, page number, slot index
  - A remove button per occurrence that clears that specific page slot
- A photo appearing on the same page in two different slots also counts as a duplicate
- Clearing a slot calls existing `clearSlot(pageId, slotIndex)` API, then refreshes book data
- If no duplicates exist in either group, show the existing "no duplicates" empty state
- Each group has a heading label to distinguish them

## UI Details

- Group headings: `text-sm font-medium text-slate-400 mb-3` (e.g. "In multiple sections" / "On multiple pages")
- Cross-page duplicate entries use the same card layout as cross-section ones (photo thumbnail + list)
- Page references show as: "Section Name > Page N, slot M"
- Remove button uses the same X icon style as existing cross-section remove

## Implementation Notes

- Modify `web/src/pages/BookEditor/DuplicatesTab.tsx`
- Build page-slot duplicate map by scanning `book.pages` and their `slots` arrays
- Clearing a page slot should push an undo entry (integrate with existing `useUndoRedo` if the `onClearSlot` callback is passed down)
- Add i18n keys: `books.editor.duplicatesSectionGroup`, `books.editor.duplicatesPageGroup`, `books.editor.duplicatesPageLocation`
