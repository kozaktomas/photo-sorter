# Photo Book

A tool for organizing photos into a printed landscape photo book with PDF export via LaTeX.

## Workflow

1. **Create a book** — Give it a title
2. **Define sections** — Named groups (e.g., "Childhood", "Wedding", "Vacation")
3. **Prepick photos** — Browse the library and add photos to sections
4. **Write descriptions** — Add a description (caption for print) and optional note (internal) to each photo
5. **Create pages** — Choose a page format and assign to a section
6. **Add page descriptions** — Optional text displayed at the top of each page
7. **Assign photos to slots** — Drag photos from the unassigned pool into page slots
8. **Adjust crops** — Fine-tune crop position and zoom for each photo slot (drag to reposition, scroll to zoom)
9. **Adjust split position** — For mixed landscape/portrait formats, adjust the column split ratio
10. **Add text to slots** — Click "Add text" on empty slots to place text content instead of photos
11. **Preview** — Review the full book layout with page descriptions and photo captions
12. **Export PDF** — Generate a print-ready A4 landscape PDF via LaTeX

## Page Formats

| Format | Slots | Layout |
|--------|-------|--------|
| `4_landscape` | 4 | 2x2 grid of landscape photos |
| `2l_1p` | 3 | 2 landscape (stacked vertically, left) + 1 portrait (right, full height) |
| `1p_2l` | 3 | 1 portrait (left, full height) + 2 landscape (stacked vertically, right) |
| `2_portrait` | 2 | 2 portrait photos side by side |
| `1_fullscreen` | 1 | Single fullscreen photo |

### Layout Diagrams

```
4_landscape:          2l_1p:              1p_2l:              2_portrait:
+------+------+       +--------+----+     +----+--------+     +--------+--------+
|  0   |  1   |       |   0 L  |    |     |    |   1 L  |     |   0    |   1    |
+------+------+       +--------+ 2P |     | 0P +--------+     |   P    |   P    |
|  2   |  3   |       |   1 L  |    |     |    |   2 L  |     |        |        |
+------+------+       +--------+----+     +----+--------+     +--------+--------+

1_fullscreen:
+-------------+
|             |
|      0      |
|             |
+-------------+
```

The `2l_1p` and `1p_2l` formats use a `2fr:1fr` / `1fr:2fr` column ratio so the landscape side is wider than the portrait side.

## Database Schema

Migration: `internal/database/postgres/migrations/008_create_photo_books.sql`
Format constraint update: `internal/database/postgres/migrations/009_add_1p_2l_format.sql`
Fullscreen format: `internal/database/postgres/migrations/011_add_1_fullscreen_format.sql`
Page description + photo note: `internal/database/postgres/migrations/010_add_page_desc_and_photo_note.sql`
Slot text content: `internal/database/postgres/migrations/012_add_slot_text_content.sql`
Page style (modern/archival): `internal/database/postgres/migrations/013_add_page_style.sql`
Crop scale (zoom): `internal/database/postgres/migrations/015_add_crop_scale.sql`

### Tables

```
photo_books
├── id (PK)
├── title
├── description
├── created_at
└── updated_at

book_sections
├── id (PK)
├── book_id (FK → photo_books, CASCADE)
├── title
├── sort_order
├── created_at
└── updated_at

section_photos
├── id (PK, BIGSERIAL)
├── section_id (FK → book_sections, CASCADE)
├── photo_uid (references PhotoPrism)
├── description (caption for printed book)
├── note (internal creator note, not printed)
├── added_at
└── UNIQUE(section_id, photo_uid)

book_pages
├── id (PK)
├── book_id (FK → photo_books, CASCADE)
├── section_id (FK → book_sections, SET NULL)
├── format (CHECK: 4_landscape, 2l_1p, 1p_2l, 2_portrait, 1_fullscreen)
├── style (CHECK: modern, archival; DEFAULT 'modern')
├── split_position (REAL, default 0.5, range 0.2-0.8, for 2l_1p/1p_2l formats)
├── description (text displayed at top of page)
├── sort_order
├── created_at
└── updated_at

page_slots
├── id (PK, BIGSERIAL)
├── page_id (FK → book_pages, CASCADE)
├── slot_index
├── photo_uid (NULL for text slots)
├── text_content (TEXT, default '', non-empty for text slots)
├── crop_x (REAL, default 0.5, range 0.0-1.0, horizontal crop center)
├── crop_y (REAL, default 0.5, range 0.0-1.0, vertical crop center)
├── crop_scale (REAL, default 1.0, range 0.1-1.0, zoom level: 1.0 = fill, lower = zoom in)
├── CHECK(photo_uid IS NULL OR text_content = '')  -- mutual exclusivity
├── UNIQUE(page_id, slot_index)
└── UNIQUE(page_id, photo_uid)
```

Deleting a book cascades to all sections, pages, and slots.

## API Endpoints

### Books

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/books` | List all books |
| POST | `/api/v1/books` | Create a book (`{ title }`) |
| GET | `/api/v1/books/:id` | Get book with sections and pages |
| PUT | `/api/v1/books/:id` | Update book (`{ title, description }`) |
| DELETE | `/api/v1/books/:id` | Delete book (cascades) |

### Sections

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/books/:id/sections` | Create section (`{ title }`) |
| PUT | `/api/v1/books/:id/sections/reorder` | Reorder sections (`{ ids: [...] }`) |
| PUT | `/api/v1/sections/:id` | Update section (`{ title }`) |
| DELETE | `/api/v1/sections/:id` | Delete section |

### Section Photos

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/sections/:id/photos` | List photos in section |
| POST | `/api/v1/sections/:id/photos` | Add photos (`{ photo_uids: [...] }`) |
| DELETE | `/api/v1/sections/:id/photos` | Remove photos (`{ photo_uids: [...] }`) |
| PUT | `/api/v1/sections/:id/photos/:uid/description` | Update photo (`{ description, note }`) |

### Pages

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/books/:id/pages` | Create page (`{ format, section_id, style? }`) |
| PUT | `/api/v1/books/:id/pages/reorder` | Reorder pages (`{ ids: [...] }`) |
| PUT | `/api/v1/pages/:id` | Update page (`{ format, section_id, description, style }`) |
| DELETE | `/api/v1/pages/:id` | Delete page |

### Slots

| Method | Endpoint | Description |
|--------|----------|-------------|
| PUT | `/api/v1/pages/:id/slots/:index` | Assign photo or text to slot (`{ photo_uid }` or `{ text_content }`) |
| PUT | `/api/v1/pages/:id/slots/:index/crop` | Update crop for a slot (`{ crop_x, crop_y, crop_scale? }`) |
| POST | `/api/v1/pages/:id/slots/swap` | Swap two slots atomically (`{ slot_a, slot_b }`) |
| DELETE | `/api/v1/pages/:id/slots/:index` | Clear slot |

## PDF Export

The book can be exported to a print-ready A4 landscape PDF via the "Export PDF" button in the editor header.

### How It Works

1. Backend collects all pages (ordered by section, then sort_order)
2. Loads section photo descriptions to build a caption map
3. Downloads high-resolution thumbnails (`fit_3840`) for all photos in slots
4. Computes layout geometry with configurable margins (asymmetric for binding)
5. Generates a LaTeX document using TikZ for precise photo placement with object-cover cropping
6. Compiles with `lualatex` (Czech typography via `polyglossia` + Latin Modern Roman font)
7. Returns the PDF and an export report with DPI warnings

### Requirements

- `lualatex` must be installed on the server (packages: `texlive-luatex`, `texmf-dist-latexrecommended`, `texmf-dist-fontsrecommended`, `texmf-dist-langczechslovak`)
- Returns HTTP 503 if `lualatex` is not available

### Layout Configuration — 12-Column Grid System

The layout uses a 12-column grid with 3 fixed page zones. Configurable via `LayoutConfig` in `internal/latex/formats.go`:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `InsideMarginMM` | 20.0 | Binding side margin |
| `OutsideMarginMM` | 12.0 | Away from binding |
| `TopMarginMM` | 10.0 | Top margin |
| `BottomMarginMM` | 16.0 | Bottom margin |
| `GridColumns` | 12 | Number of grid columns |
| `ColumnGutterMM` | 4.0 | Gap between columns |
| `RowGapMM` | 4.0 | Gap between rows in multi-row layouts |
| `HeaderHeightMM` | 4.0 | Running header zone |
| `CanvasHeightMM` | 172.0 | Photo/text canvas zone |
| `FooterHeightMM` | 8.0 | Captions + folio zone |
| `ArchivalInsetMM` | 3.0 | Mat inset for archival photos |
| `GutterSafeMM` | 8.0 | Inset from binding edge for safe content placement |
| `BaselineUnitMM` | 4.0 | Vertical rhythm unit (all spacing is multiples of this) |

**Content width**: 297 - 20 - 12 = **265mm**. Column width: (265 - 11×4) / 12 = **18.42mm**.

**Margins are asymmetric** (mirrored for binding): recto pages have inside margin (20mm) on the left, verso pages have outside margin (12mm) on the left.

### Page Zones

Each content page has 3 fixed zones:

```
TOP MARGIN (10mm)
├── HEADER ZONE (4mm): Running section/page titles
├── CANVAS ZONE (172mm): Photo/text slots on 12-col grid
├── ──────────────────── ← 0.4pt separation rule (black!50)
├── FOOTER ZONE (8mm): Numbered captions + mirrored folio
BOTTOM MARGIN (16mm)
```

Zones sum exactly to page height: 10 + 4 + 172 + 8 + 16 = 210mm.

### Grid Column Mapping

| Format | Slot | Columns | Width | Height |
|--------|------|---------|-------|--------|
| `1_fullscreen` | 0 | 1-12 | 265mm | 172mm |
| `2_portrait` | 0,1 | 1-6, 7-12 | 130.5mm | 172mm |
| `4_landscape` | 0-3 | 1-6/7-12 × top/bottom | 130.5mm | 84mm |
| `2l_1p` | 0,1 (landscape) | 1-8 stacked | 175.3mm | 84mm |
| `2l_1p` | 2 (portrait) | 9-12 | 85.7mm | 172mm |
| `1p_2l` | 0 (portrait) | 1-4 | 85.7mm | 172mm |
| `1p_2l` | 1,2 (landscape) | 5-12 stacked | 175.3mm | 84mm |

Half-canvas height: (172 - 4) / 2 = 84mm.

**Note:** For `2l_1p` and `1p_2l` formats, the column split can be adjusted via the `split_position` field on `book_pages` (default 0.5 = 8:4 columns). This allows customizing the width ratio between landscape and portrait slots.

### Canvas Zone Clipping

The entire canvas zone is wrapped in a TikZ `\clip` scope that prevents any content (photos, text, markers) from bleeding into the header or footer zones. This is a safety net — all slots are already positioned within the canvas bounds, but the clip ensures integrity at scale.

### Running Headers

- **Verso (even) pages**: Section title flush left (outside edge), 7.5pt italic, `black!40`
- **Recto (odd) pages**: Page description flush right (outside edge), 7.5pt italic, `black!40`
- **Fallback**: If a recto page has no description, the section title is shown instead
- Header zone (4mm) is always reserved even when empty

### Photo Captions — Footer System

Captions are sourced from `section_photos.description` (section-scoped). Instead of per-slot inline captions, all captions are collected into the footer zone:

- **Single-photo pages**: Caption alone in footer, no marker number
- **Multi-photo pages**: Sequential marker numbers (1, 2, 3...) assigned to photos with captions
  - **Marker overlay**: 4×4mm bold number (6pt) on outside edge of photo (away from binding), white on semi-transparent dark background
  - **Footer text**: `**1** Caption text  **2** Caption text` in 8/10pt, `black!70`, 4mm below separation rule

### Page Styles

Each page has a `style` field: `"modern"` (default) or `"archival"`.

- **Modern**: Full-bleed photos clipped to slot boundary (existing behavior)
- **Archival**: 0.5pt gray border (`black!45`) at slot boundary, image inset 3mm inside (pasparta/mat effect). DPI computed against inset dimensions.

### Page Numbers (Folio)

Pages are numbered continuously from 1. Folio renders in 8.5/10pt `black!35`, mirrored to outside-bottom corners:
- Recto (odd): bottom-right (`south east` anchor)
- Verso (even): bottom-left (`south west` anchor)

A 0.4pt separation rule (`black!50`) spans the full content width between canvas and footer zones.

### Gutter-Safe Zone

An 8mm gutter-safe zone is enforced from the binding (inside) edge. Caption markers are automatically placed on the **outside edge** (away from binding) of each photo to avoid the gutter zone:
- **Recto pages**: Marker in top-right corner
- **Verso pages**: Marker in top-left corner

The layout validation layer checks that markers don't fall within the gutter-safe zone and reports warnings if they do.

### Spacing Discipline

All spacing values follow a 4mm baseline rhythm:
- **Canvas text leading**: 11.34pt (= 4mm exactly) for T1, T2, T3 body text
- **Footer caption gap**: 4mm below separation rule (1 baseline unit)
- **Text slot padding**: Multiples of 4mm (T1/T3: 8mm/side, T2: 8mm accent bar offset)
- **Markdown vspace**: `\vspace{4mm}` for paragraph breaks and after headings
- **Parskip suppressed**: `\parskip=0pt\parindent=0pt` inside all text slot parboxes to prevent LaTeX default spacing from breaking baseline grid

### Section Dividers

Each section with a title generates a full-page divider with the title centered in 28pt EBGaramond SemiBold with letter-spacing (LetterSpace=5). Paired decorative rules (100mm wide, `black!20`) appear above and below the title for visual balance. No page number is displayed on divider pages.

**Recto alignment:** Section dividers always appear on recto (right-hand, odd) pages. If a divider would land on a verso (even) page, a blank page is automatically inserted before it.

### Text Slots

Text slots support Markdown formatting with auto-detected rendering types:

| Syntax | Result |
|--------|--------|
| `# Heading` | Large bold heading |
| `## Subheading` | Medium bold subheading |
| `**bold**` | **Bold text** |
| `*italic*` | *Italic text* |
| `- item` | Bulleted list |
| `1. item` | Numbered list |
| `> quote` | Blockquote (italic) |
| Blank line | Paragraph break |

**Text type auto-detection** (`DetectTextType`):
- **T1 (explanation)**: Default — 5% gray fill, 10/13pt body, headings via markdown
- **T2 (fact box)**: All lines are list items — 5% gray fill, left accent bar (2pt `black!20`), compact list
- **T3 (oral history)**: Contains `> ` blockquote lines — 5% gray fill, centered italic quote

All text slots render on a light gray background (`black!5`), replacing the previous white-on-dark style.

### Czech Typography

The template uses `polyglossia` with Czech as the default language and Latin Modern Roman as the main font. This provides proper Czech hyphenation, ligatures, and diacritics support.

### Export Report

The export generates a JSON report alongside the PDF containing:
- Book title, total page count, unique photo count
- Per-page details: page number, format, section title, divider flag
- Per-photo details: UID, slot index, effective DPI
- Warnings for photos with effective DPI < 200 (below print quality)
- **Layout validation warnings**: Zone integrity violations, slot overlaps, gutter-safe marker issues

Access the report via `?format=report` query parameter (returns JSON instead of PDF).

### Layout Validation

The export pipeline runs automatic layout validation (`ValidatePages`) after building the template data. Checks include:
1. **Zone integrity**: Every slot's clip rect is within canvas bounds
2. **Grid alignment**: Every slot's X offset matches a column left edge (±0.01mm)
3. **No overlaps**: No pair of slots has intersecting clip rects (0.01mm tolerance)
4. **Gutter-safe markers**: Caption markers don't fall within 8mm of the binding edge
5. **Footer bounds**: Caption block and folio are within the footer zone

Validation warnings appear in the export report alongside DPI warnings.

### Diagnostic Test PDF

A diagnostic test PDF can be generated to verify the grid system and layout constraints:

```
GET /api/v1/books/:id/export-pdf?format=test
```

The test PDF contains 6 pages:
1. **Grid overlay**: Red column boundaries, blue canvas bounds, green header/footer zones
2. **Baseline overlay**: Gray 4mm horizontal lines within the canvas zone
3. **Format samples**: One page per format (`1_fullscreen`, `2_portrait`, `4_landscape`, `2l_1p`, `1p_2l`) with colored placeholder rectangles labeled with dimensions
4-5. **Additional format samples** (continued)
6. **Gutter-safe visualization**: Red overlay on the 8mm gutter-safe zone with sample marker placement

### Professional Print Features

**Bleed & Crop Marks:** The PDF includes 3mm bleed on all sides using the LaTeX `crop` package. The total paper size is 303×216mm (A4 landscape + 3mm per side). Crop marks (corner registration marks) are rendered in the bleed area for precise trimming. The TikZ content coordinate system remains anchored to the A4 trim area — no coordinate adjustments needed.

**PDF Compatibility:** `\pdfvariable minorversion 4` ensures PDF 1.4 output for broad printer compatibility.

**Blank Page Insertion:** Section dividers always appear on recto (odd) pages. Blank pages are automatically inserted to maintain this convention. These appear in the export report with format `"blank"`.

### Slot Aspect Ratios

With asymmetric margins (20mm inside, 12mm outside) and 4mm column gutters:

| Format | Slot | Size (mm) | Ratio | Closest Standard |
|--------|------|-----------|-------|-----------------|
| `4_landscape` | each | 130.5 × 84 | 1.55:1 | 3:2 landscape |
| `2_portrait` | each | 130.5 × 172 | 0.76:1 | 3:4 portrait |
| `1_fullscreen` | single | 265 × 172 | 1.54:1 | 3:2 landscape |
| `2l_1p` | landscape | 175.3 × 84 | 2.09:1 | panoramic |
| `2l_1p` | portrait | 85.7 × 172 | 0.50:1 | narrow portrait |
| `1p_2l` | portrait | 85.7 × 172 | 0.50:1 | narrow portrait |
| `1p_2l` | landscape | 175.3 × 84 | 2.09:1 | panoramic |

`4_landscape`, `2_portrait`, and `1_fullscreen` work well with standard photo ratios. The mixed formats (`2l_1p`, `1p_2l`) use 8:4 column splits for wider/narrower slots.

### Print Preparation Checklist

- **Recommended DPI**: 300+ for high-quality prints, 200+ minimum
- **Fonts**: All text is embedded via EBGaramond (OpenType)
- **Deterministic output**: PDF timestamps are suppressed for reproducible builds
- **Bleed**: 3mm bleed on all sides with crop marks — ready for professional trimming
- **Color**: Decorative elements use subtle grays (`black!20`–`black!50`) for clean print output
- **Review**: Check export report for DPI warnings before sending to printer

### Backend Files

| File | Description |
|------|-------------|
| `internal/latex/formats.go` | `LayoutConfig`, 12-column grid system, `FormatSlotsGrid`, page format slot positions |
| `internal/latex/latex.go` | PDF generation, caption lookup, DPI computation, export report |
| `internal/latex/markdown.go` | Markdown-to-LaTeX converter for text slots |
| `internal/latex/validate.go` | Layout validation (zone integrity, overlaps, gutter-safe markers) |
| `internal/latex/testpages.go` | Diagnostic test PDF generator |
| `internal/latex/templates/book.tex` | LaTeX template with TikZ, polyglossia, configurable layout |
| `internal/latex/templates/testpage.tex` | Diagnostic test page template |

### API

```
GET /api/v1/books/:id/export-pdf
```

Returns `application/pdf` with `Content-Disposition: attachment`.

**Query parameters:**
| Parameter | Value | Description |
|-----------|-------|-------------|
| `format` | `report` | Return JSON export report instead of PDF |
| `format` | `test` | Return diagnostic test PDF (grid overlay, baseline, format samples, gutter-safe) |
| `format` | `debug` | Return real book PDF with debug overlay (canvas bounds, column grid, zone edges) |

**Response headers (PDF mode):**
| Header | Description |
|--------|-------------|
| `X-Export-Warnings` | Number of DPI warnings (only present when > 0) |

**Error responses:**
- 404: Book not found
- 400: Book has no pages
- 503: `lualatex` not installed
- 500: LaTeX compilation or other error

## Backend Architecture

### Go Files

| File | Description |
|------|-------------|
| `internal/database/types.go` | `PhotoBook`, `BookSection`, `SectionPhoto`, `BookPage`, `PageSlot` structs; `PageFormatSlotCount()` |
| `internal/database/repository.go` | `BookReader` and `BookWriter` interfaces |
| `internal/database/provider.go` | `RegisterBookWriter()`, `GetBookWriter()`, `GetBookReader()` |
| `internal/database/postgres/books.go` | `BookRepository` implementing `BookWriter` |
| `internal/web/handlers/books.go` | `BooksHandler` with all REST endpoints |
| `internal/web/routes.go` | Route registration (18 routes) |
| `cmd/serve.go` | Repository creation and registration |

### Key Implementation Details

- UUIDs generated in Go via `google/uuid`
- `ReorderSections` / `ReorderPages`: accept ordered ID slices, update `sort_order` in a transaction
- `AssignSlot`: UPSERT via `INSERT ... ON CONFLICT(page_id, slot_index) DO UPDATE` — clears `text_content` when assigning a photo
- `AssignTextSlot`: UPSERT with `photo_uid = NULL, text_content = $3` — clears photo when assigning text
- `ClearSlot`: `DELETE FROM page_slots WHERE page_id = $1 AND slot_index = $2`
- `SwapSlots`: Atomic swap in a transaction — reads both slots (photo_uid + text_content), deletes both, re-inserts with swapped values. Required because `UNIQUE(page_id, photo_uid)` prevents concurrent updates
- `CreateSection` / `CreatePage`: auto-assign `sort_order` as `MAX(sort_order) + 1`
- **Format change**: When a page's format is changed to one with fewer slots, excess slots are automatically cleared (photos returned to the unassigned pool). Slots within the new format's capacity are preserved.

## Frontend Architecture

### Files

| File | Description |
|------|-------------|
| `web/src/pages/Books/index.tsx` | Books list page — card grid, create, delete |
| `web/src/pages/BookEditor/index.tsx` | Editor shell — tabs (Sections, Pages, Preview), title editing |
| `web/src/pages/BookEditor/hooks/useBookData.ts` | Book data fetching and section photo loading |
| `web/src/pages/BookEditor/SectionsTab.tsx` | Sections tab — sidebar + photo pool layout |
| `web/src/pages/BookEditor/SectionSidebar.tsx` | Sortable section list (drag to reorder) |
| `web/src/pages/BookEditor/SectionPhotoPool.tsx` | Photo grid with selection, inline description + note editing |
| `web/src/pages/BookEditor/PhotoBrowserModal.tsx` | Full-screen modal to browse library and add photos |
| `web/src/pages/BookEditor/PagesTab.tsx` | Pages tab — DndContext for drag-to-slot |
| `web/src/pages/BookEditor/PageSidebar.tsx` | Pages grouped by section with collapsible headers, sortable within section |
| `web/src/pages/BookEditor/PageTemplate.tsx` | CSS grid page layout with droppable slots |
| `web/src/pages/BookEditor/PageSlot.tsx` | Individual slot component (both draggable and droppable) |
| `web/src/pages/BookEditor/UnassignedPool.tsx` | Draggable photos with L/P orientation badges, description/note icons |
| `web/src/pages/BookEditor/PhotoDescriptionDialog.tsx` | Modal dialog for editing photo description + creator note |
| `web/src/pages/BookEditor/PhotoActionOverlay.tsx` | Hover overlay with View Detail, Find Similar, Copy ID actions |
| `web/src/pages/BookEditor/PreviewTab.tsx` | Read-only scrollable book preview with page descriptions |
| `web/src/pages/PhotoDetail/AddToBookDropdown.tsx` | Two-step picker (book → section) for adding photo to a book |
| `web/src/pages/PhotoDetail/BookMembership.tsx` | Sidebar panel showing which books/sections a photo belongs to |

### Dependencies

- `@dnd-kit/core` — drag-and-drop primitives (draggable, droppable, DragOverlay)
- `@dnd-kit/sortable` — sortable lists for sections and pages
- `@dnd-kit/utilities` — CSS transform helpers

### Drag-and-Drop Behavior

**Sections Tab**: Sections are reorderable via `@dnd-kit/sortable`. On drag-end, calls `reorderSections()`.

**Pages Tab**: Pages are grouped by section in the sidebar with collapsible headers showing section title and page count. Pages are reorderable via `@dnd-kit/sortable` within the same section; cross-section drag is blocked. Global page numbering (Page 1, 2, 3...) is maintained across all sections. Creating a new page auto-expands the target section if collapsed. Photo assignment uses `@dnd-kit/core`:
- Drag from unassigned pool → drop on slot: assigns photo
- Drag assigned photo → drop on empty slot: moves photo (clears old slot first)
- Drag assigned photo → drop on another assigned photo: swaps both photos atomically
- Click X on a slot: clears the assignment
- Click "Add text" on empty slot: opens text editing dialog
- Click pencil on text slot: opens text editing dialog to modify content
- Text slots are draggable and can be swapped with photo slots

Slot photos are both draggable (via `useDraggable`) and droppable (via `useDroppable`) using a combined ref on the same DOM element. The `DragOverlay` uses a `snapCenterToCursor` modifier to keep the small drag thumbnail centered on the cursor regardless of the source element's size. Collision detection uses `pointerWithin` for accurate drop target resolution.

### Unassigned Pool Orientation Badges

Each photo in the unassigned pool displays a small badge indicating orientation:
- **L** (blue) — landscape (width >= height)
- **P** (amber) — portrait (height > width)

Orientation is detected from the thumbnail's `naturalWidth` / `naturalHeight` after loading.

### Page Template CSS Grid

The page templates use Tailwind CSS grid classes:

| Format | Grid Classes | Slot Positioning |
|--------|-------------|-----------------|
| `4_landscape` | `grid-cols-2 grid-rows-2` | All slots auto-placed |
| `2l_1p` | `grid-cols-[2fr_1fr] grid-rows-2` | Slot 2: `col-start-2 row-start-1 row-span-2` |
| `1p_2l` | `grid-cols-[1fr_2fr] grid-rows-2` | Slot 0: `row-span-2` |
| `2_portrait` | `grid-cols-2 grid-rows-1` | All slots auto-placed |
| `1_fullscreen` | `grid-cols-1 grid-rows-1` | Single slot fills page |

### Internationalization

All UI strings are translated (English + Czech). Key translation groups in `pages.json`:

- `books.title`, `books.subtitle`, `books.createBook`, etc.
- `books.editor.*` — editor UI strings
- `books.editor.format*` — page format labels for dropdowns
- `books.editor.formatShort*` — short format labels for sidebar/preview

Czech translations use proper orientation terms: "na šířku" (landscape) and "na výšku" (portrait).

### Routes

- `/books` — Books list page
- `/books/:id` — Book editor page

Registered in `App.tsx` and accessible via the "Photo Book" item in the Tools navigation dropdown.

### Photo Detail Integration

The photo detail page (`/photos/:uid`) integrates with the book feature:

- **Add to Book dropdown** — Header button opens a cascading picker: select a book, then a section. The photo is added to the chosen section with success/error feedback.
- **Book Membership panel** — If the photo belongs to any book sections, a sidebar panel lists each "Book / Section" as a clickable link to the book editor.
- **API:** `GET /api/v1/photos/:uid/books` returns `PhotoBookMembership[]` with `book_id`, `book_title`, `section_id`, `section_title`.

### Page Config

The photo book pages use `rose` as their accent color, configured in `constants/pageConfig.ts` as `books` and `bookEditor` entries.
