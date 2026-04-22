# Enable drag-and-drop of pages between sections

Allow moving a page from one section to another (or to a new section) in the Book editor via drag-and-drop in the Pages tab. Currently DnD works only within a single section.

## Requirements

- In the Pages tab of the Book editor, a page can be dragged and dropped onto any other section in the same book (including a section different from its current one).
- On drop, the page is reassigned to the target section while preserving:
  - Page format and layout (e.g. `4_landscape`, `2l_1p`, `1_fullbleed`, etc.)
  - All slot assignments (photo UIDs, text content, captions-slot / contents-slot flags)
  - Per-slot crop state (`crop_x`, `crop_y`, `crop_scale`)
  - `split_position`, `hide_page_number`, page description, style
  - Page order — the moved page is appended at the end of the target section (or inserted at drop position if the UI supports it)
- Photos used in any slot of the moved page are added to the target section's photo pool (via `AddPhotosToSection`-equivalent logic), preserving their section-level descriptions/notes where applicable.
- Photos are removed from the source section's pool only if no other page in the source section still uses them. Photos still referenced by remaining pages in the source stay in the source pool.
- Section-photo descriptions/notes for moved photos are carried over to the target section (best-effort copy; if a note already exists on the target, keep the existing one).
- Backend: either extend an existing page update endpoint to accept a `section_id` change or add a dedicated move endpoint. Perform all changes in a single DB transaction so the move is atomic.
- Frontend: extend the existing drag-and-drop wiring in the Pages tab so pages can be dropped on section headers / section containers in the sidebar, not just reordered within their current section.
- Undo/redo stack (if present in Book editor) should treat the cross-section move as one undoable step.
- Validate that a page cannot be moved to a section in a different book.

## Implementation Notes

- Relevant backend code: `internal/database/postgres/books.go`, `internal/web/handlers/books.go` (page update + reorder handlers), MCP `update_page` in `internal/mcp/pages.go`.
- Relevant frontend code: `web/src/pages/BookEditor/PagesTab.tsx`, `PageSidebar.tsx`, `PageMinimap.tsx`, and the `useBookData` / `useUndoRedo` hooks.
- Consider whether to expose the cross-section move through MCP `update_page` as well (accept new `section_id`) for API parity.
- Write tests for the "photo shared between pages in the same section" edge case to confirm it is not removed from the source pool.