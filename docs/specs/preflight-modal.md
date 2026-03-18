# Preflight Check Modal Before PDF Export

Show a validation report modal when the user clicks Export PDF, before starting the actual export.

## Requirements

- When user clicks the Export PDF button, first call `GET /api/v1/books/{id}/preflight`
- Display results in a modal dialog with three collapsible sections:
  1. **Errors** (red) - blocking issues (currently none defined, reserved for future)
  2. **Warnings** (amber) - non-blocking issues: empty slots, low DPI photos, empty sections
  3. **Info** (blue) - informational: unplaced photos, missing captions
- Each issue is a single line with an icon, description, and (where applicable) a "Go to page" link
- A summary bar at the top shows: "N pages, M photos, X/Y slots filled"
- Two buttons at the bottom:
  - "Export anyway" - proceeds with PDF export (always available)
  - "Cancel" - closes the modal
- If preflight returns `ok: true` (no warnings), skip the modal and export directly
- "Go to page" links close the modal and navigate to the Pages tab with that page selected

## UI Details

- Modal uses the standard dialog pattern (backdrop + centered card)
- Section headers are collapsible (click to expand/collapse), default expanded
- Each warning line: icon (AlertTriangle for warnings, Info for info) + text + optional link
- Low DPI warnings show the DPI value in parentheses: "Page 5, slot 1: photo abc — 185 DPI"
- Summary bar: `bg-slate-800 rounded-lg p-3` with the stats in a flex row
- "Export anyway" button: primary rose style; "Cancel": secondary slate style
- Loading state while preflight runs: spinner with "Checking..."

## Implementation Notes

- New file: `web/src/pages/BookEditor/PreflightModal.tsx`
- Add `preflightBook(bookId)` method to `web/src/api/client.ts`
- Modify the Export PDF button handler in `BookEditor/index.tsx` to call preflight first
- Type the preflight response in `web/src/types/index.ts`
- Add i18n keys under `books.editor.preflight.*` namespace
