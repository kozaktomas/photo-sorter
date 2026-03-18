# Preflight Check Backend Endpoint

A new API endpoint that validates a book before PDF export, returning warnings and errors without generating the PDF.

## Requirements

- `GET /api/v1/books/{id}/preflight` runs validation checks and returns a report
- Checks performed:
  1. **Empty slots**: pages with unfilled slot positions (warning per page)
  2. **Low DPI photos**: photos whose effective DPI < 200 at their assigned slot size (warning per slot)
  3. **Unplaced photos**: section photos not assigned to any page slot (info per section)
  4. **Empty sections**: sections with no pages (warning)
  5. **Missing captions**: photo slots without a description in section_photos (info, count only)
- DPI calculation: fetch photo dimensions from PhotoPrism, compute slot physical dimensions from format/split_position using the same logic as `internal/latex/formats.go`, then `effectiveDPI = min(photoW / slotWmm, photoH / slotHmm) * 25.4`
- Response format:
  ```json
  {
    "ok": false,
    "errors": [],
    "warnings": [
      { "type": "empty_slot", "page_number": 3, "section": "Summer", "slot_index": 2 },
      { "type": "low_dpi", "page_number": 5, "section": "Summer", "slot_index": 0, "photo_uid": "abc", "dpi": 185 }
    ],
    "info": [
      { "type": "unplaced_photos", "section": "Summer", "count": 4 },
      { "type": "missing_captions", "count": 12 }
    ],
    "summary": { "total_pages": 20, "total_photos": 45, "filled_slots": 38, "total_slots": 52 }
  }
  ```
- `ok` is true when there are no warnings and no errors

## Implementation Notes

- Add handler to `BooksHandler` in `internal/web/handlers/books.go`
- Reuse `FormatSlotsGrid`/`FormatSlotsGridWithSplit` from `internal/latex/formats.go` for slot dimensions
- Photo dimensions: batch-fetch via PhotoPrism `GetPhotos` (collect all photo UIDs from slots first)
- Register route in the books router
- Update `docs/API.md` with the new endpoint
