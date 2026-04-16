# Photo Book

A tool for organizing photos into a printed landscape photo book with PDF export via LaTeX.

## Hierarchy

Book > Chapters (optional) > Sections > Pages > Slots

Chapters provide an optional grouping level between books and sections. Sections can exist without a chapter.

## Workflow

1. **Create a book** — Give it a title
2. **Define chapters** (optional) — Top-level groupings (e.g., "Part One", "Part Two")
3. **Define sections** — Named groups (e.g., "Childhood", "Wedding", "Vacation"), optionally assigned to a chapter
4. **Prepick photos** — Browse the library and add photos to sections
5. **Write descriptions** — Add a description (caption for print) and optional note (internal) to each photo
6. **Create pages** — Choose a page format and assign to a section (or use auto-layout to generate pages automatically from unassigned photos)
7. **Add page descriptions** — Optional text displayed at the top of each page
8. **Assign photos to slots** — Drag photos from the unassigned pool into page slots
9. **Adjust crops** — Fine-tune crop position and zoom for each photo slot (drag to reposition, scroll to zoom, drag bottom-right corner handle to resize). Displays live pixel dimensions of the cropped region.
10. **Adjust split position** — For mixed landscape/portrait formats, adjust the column split ratio
11. **Add text to slots** — Click "Add text" on empty slots to place text content instead of photos
12. **Preview** — Review the full book layout with page descriptions and photo captions
13. **Preflight check** — Validate the book for empty slots, low-DPI photos, unplaced photos, and missing captions
14. **Export PDF** — Generate a print-ready A4 landscape PDF via LaTeX

## Page Formats

| Format | Slots | Layout |
|--------|-------|--------|
| `4_landscape` | 4 | 2x2 grid of landscape photos |
| `2l_1p` | 3 | 2 landscape (stacked vertically, left) + 1 portrait (right, full height) |
| `1p_2l` | 3 | 1 portrait (left, full height) + 2 landscape (stacked vertically, right) |
| `2_portrait` | 2 | 2 portrait photos side by side |
| `1_fullscreen` | 1 | Single photo filling the safe canvas (margins for folio + captions) |
| `1_fullbleed` | 1 | Single photo covering the **entire page including 3 mm print bleed** — folio and footer captions are automatically suppressed for the page. Manual selection only (not produced by auto-layout). |

### Layout Diagrams

```
4_landscape:          2l_1p:              1p_2l:              2_portrait:
+------+------+       +--------+----+     +----+--------+     +--------+--------+
|  0   |  1   |       |   0 L  |    |     |    |   1 L  |     |   0    |   1    |
+------+------+       +--------+ 2P |     | 0P +--------+     |   P    |   P    |
|  2   |  3   |       |   1 L  |    |     |    |   2 L  |     |        |        |
+------+------+       +--------+----+     +----+--------+     +--------+--------+

1_fullscreen:         1_fullbleed:
+-------------+       +=============+
|             |       |             |
|      0      |       |      0      |  ← extends past trim line by 3 mm
|             |       |             |    on every side; no folio, no captions
+-------------+       +=============+
```

The `2l_1p` and `1p_2l` formats use a `2fr:1fr` / `1fr:2fr` column ratio so the landscape side is wider than the portrait side.

## Database Schema

Migration: `internal/database/postgres/migrations/008_create_photo_books.sql`
Format constraint update: `internal/database/postgres/migrations/009_add_1p_2l_format.sql`
Fullscreen format: `internal/database/postgres/migrations/011_add_1_fullscreen_format.sql`
Page description + photo note: `internal/database/postgres/migrations/010_add_page_desc_and_photo_note.sql`
Slot text content: `internal/database/postgres/migrations/012_add_slot_text_content.sql`
Page style (modern/archival): `internal/database/postgres/migrations/013_add_page_style.sql`
Crop position + split: `internal/database/postgres/migrations/014_add_crop_and_split.sql`
Crop scale (zoom): `internal/database/postgres/migrations/015_add_crop_scale.sql`
Chapters: `internal/database/postgres/migrations/016_create_book_chapters.sql`
Chapter color: `internal/database/postgres/migrations/020_add_chapter_color.sql`
Book typography (fonts, sizes, caption opacity): `internal/database/postgres/migrations/021_add_book_typography.sql`
Caption font size (standalone): `internal/database/postgres/migrations/022_add_caption_font_size.sql`
Heading color bleed: `internal/database/postgres/migrations/023_add_heading_color_bleed.sql`
Caption badge size: `internal/database/postgres/migrations/024_add_caption_badge_size.sql`
Full-bleed format: `internal/database/postgres/migrations/027_add_1_fullbleed_format.sql`
Body text padding next to photo: `internal/database/postgres/migrations/029_add_body_text_pad_mm.sql`

### Tables

```
photo_books
├── id (PK)
├── title
├── description
├── body_font (VARCHAR(50), default 'pt-serif')
├── heading_font (VARCHAR(50), default 'source-sans-3')
├── body_font_size (REAL, default 11.0 pt)
├── body_line_height (REAL, default 15.0 pt)
├── h1_font_size (REAL, default 18.0 pt)
├── h2_font_size (REAL, default 13.0 pt)
├── caption_opacity (REAL, default 0.85, range 0.0-1.0)
├── caption_font_size (REAL, default 9.0 pt)
├── heading_color_bleed (REAL, default 4.0 mm, range 0–20 mm)
├── caption_badge_size (REAL, default 4.0 mm, range 2–12 mm)
├── body_text_pad_mm (REAL, default 4.0 mm, range 0–10 mm)
├── created_at
└── updated_at

book_chapters
├── id (PK)
├── book_id (FK → photo_books, CASCADE)
├── title
├── color (TEXT, optional hex color e.g. '#8B0000' for chapter theme)
├── sort_order
├── created_at
└── updated_at

book_sections
├── id (PK)
├── book_id (FK → photo_books, CASCADE)
├── chapter_id (FK → book_chapters, SET NULL, nullable)
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
├── format (CHECK: 4_landscape, 2l_1p, 1p_2l, 2_portrait, 1_fullscreen, 1_fullbleed)
├── style (CHECK: modern, archival; DEFAULT 'modern')
├── split_position (REAL, default 0.5, range 0.2-0.8, for 2l_1p/1p_2l formats)
├── hide_page_number (BOOLEAN, default false; suppresses folio rendering on this page only — pagination of other pages is unaffected, migration 025)
├── description (text displayed at top of page)
├── sort_order
├── created_at
└── updated_at

page_slots
├── id (PK, BIGSERIAL)
├── page_id (FK → book_pages, CASCADE)
├── slot_index
├── photo_uid (NULL for text/captions slots)
├── text_content (TEXT, default '', non-empty for text slots)
├── is_captions_slot (BOOLEAN, default FALSE, migration 026)
│                    Routes the page's photo captions into this slot instead
│                    of the bottom strip. At most one per page.
├── crop_x (REAL, default 0.5, range 0.0-1.0, horizontal crop center)
├── crop_y (REAL, default 0.5, range 0.0-1.0, vertical crop center)
├── crop_scale (REAL, default 1.0, range 0.1-1.0, zoom level: 1.0 = fill, lower = zoom in)
├── CHECK: at most one of {photo_uid, text_content, is_captions_slot} is set
├── UNIQUE(page_id, slot_index)
├── UNIQUE(page_id, photo_uid)
└── UNIQUE INDEX (page_id) WHERE is_captions_slot
```

### Captions slot

A slot can be flipped into "captions mode" instead of holding a photo or text.
When one of a page's slots is marked as a captions slot, the renderer routes
the same `FooterCaption` list that would normally go into the bottom caption
strip into that slot and suppresses the bottom strip for the page. The list
stacks vertically, one caption per paragraph, with the same numbered badges and
source text (`section_photos.description`) — only the position changes. Use
this when a single caption is long enough to overflow the bottom strip.

**Rendering inside the slot:**

- Each caption is its own paragraph (`\par` separator), so they actually
  stack vertically rather than flowing as one wrapping paragraph.
- Caption text is **justified** (block-aligned) inside the slot — the
  slot is wide enough that this looks clean. Last line of each caption
  remains left-aligned (LaTeX default for `\par`-terminated paragraphs).
- Each paragraph uses `\hangindent = badge_width + 1.5mm` so wrapped lines
  align under the first character of the caption text rather than under
  the badge — same visual pattern as a numbered list:

  ```
  [1] First caption that wraps to a
      second line aligned under "First"
  [2] Second caption
  ```

- The badge-to-text gap is a fixed `\hspace{1.5mm}` (rather than a
  font-dependent thin space) so the hanging indent always matches the
  exact text start position. The constant lives at
  `latex.slotCaptionBadgeGapMM` and is exposed to `book.tex` via the
  `slotCaptionIndentMM` template func — bump it in one place if you want
  more breathing room.

**API:**

- Assign via `PUT /api/v1/pages/:id/slots/:index` with `{ "captions": true }`.
- Clearing the slot (`DELETE /api/v1/pages/:id/slots/:index`) restores the
  bottom strip automatically on the next render.
- Replacing the slot with a photo or text also resets the flag.
- At most one captions slot per page is allowed; a second assignment returns
  HTTP 409.

Deleting a book cascades to all chapters, sections, pages, and slots.

## API Endpoints

### Fonts

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/fonts` | List all available fonts (`[{ id, display_name, category, google_family, google_spec }]`) |

### Books

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/books` | List all books |
| POST | `/api/v1/books` | Create a book (`{ title }`) |
| GET | `/api/v1/books/:id` | Get book with sections and pages |
| PUT | `/api/v1/books/:id` | Update book (`{ title, description, body_font, heading_font, body_font_size, body_line_height, h1_font_size, h2_font_size, caption_opacity, caption_font_size, heading_color_bleed, caption_badge_size, body_text_pad_mm }`) |
| DELETE | `/api/v1/books/:id` | Delete book (cascades) |

### Chapters

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/books/:id/chapters` | Create chapter (`{ title, color? }`) |
| PUT | `/api/v1/books/:id/chapters/reorder` | Reorder chapters (`{ chapter_ids: [...] }`) |
| PUT | `/api/v1/chapters/:id` | Update chapter (`{ title?, color? }`) |
| DELETE | `/api/v1/chapters/:id` | Delete chapter |

### Sections

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/books/:id/sections` | Create section (`{ title, chapter_id? }`) |
| PUT | `/api/v1/books/:id/sections/reorder` | Reorder sections (`{ ids: [...] }`) |
| PUT | `/api/v1/sections/:id` | Update section (`{ title?, chapter_id? }`) |
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

### Auto-Layout

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/books/:id/sections/:sectionId/auto-layout` | Generate pages from unassigned photos (`{ prefer_formats?, max_pages? }`) |

### Preflight

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/books/:id/preflight` | Validate book before export (returns `{ ok, errors, warnings, info, summary }`) |

### Text AI

All text endpoints use **GPT-5.4-mini** (single source of truth: `ai.TextModel`). `/text/check-and-save` runs a three-tier cache (in-memory → DB by `(source_type, source_id, field)` + content hash → OpenAI).

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/text/check` | Check Czech text for spelling, grammar, and readability (`{ text }`). Response includes `changes[]` (mechanical fixes) and `suggestions[]` (`{ severity: major\|minor, message }`) |
| POST | `/api/v1/text/check-and-save` | Like `/text/check` but keyed by `(source_type, source_id, field)` and persisted to `text_check_results` for cross-session cache and stale detection |
| POST | `/api/v1/text/rewrite` | Rewrite text to target length (`{ text, target_length }`) |
| POST | `/api/v1/text/consistency` | Style consistency analysis across a set of texts |
| GET | `/api/v1/books/{id}/text-check-status` | Persisted check status per text field, including `suggestions[]` |

## PDF Export

The book can be exported to a print-ready A4 landscape PDF via the "Export PDF" button in the editor header.

### How It Works

1. Backend collects all pages (ordered by section, then sort_order)
2. Loads section photo descriptions to build a caption map
3. Downloads high-resolution thumbnails (`fit_3840`) for all photos in slots
4. Computes layout geometry with configurable margins (asymmetric for binding)
5. Generates a LaTeX document using TikZ for precise photo placement with object-cover cropping
6. Compiles with `lualatex` (Czech typography via `polyglossia` + configurable Google Fonts)
7. Returns the PDF and an export report with DPI warnings

### Requirements

- `lualatex` must be installed on the server
- **TeX packages:** `texlive-luatex`, `texmf-dist-latexrecommended`, `texmf-dist-fontsrecommended`, `texmf-dist-langczechslovak`, `texmf-dist-pictures`
- **Additional LaTeX packages:** `enumitem`, `microtype`, `crop` (from `texmf-dist-latexrecommended` or installed separately)
- **Fonts:** Google Fonts (downloaded at Docker build time). Default: PT Serif (body) + Source Sans 3 (headings). 20 fonts available — see Typography Customization section
- **Font cache:** `luaotfload` requires a writable cache directory; set `TEXMFCACHE` and `TEXMFVAR` env vars if running as a non-root user (the Go code auto-sets both to the temp directory at runtime)
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
| `BaselineUnitMM` | 4.0 | Vertical rhythm unit (all spacing is multiples of this). Used for grid alignment and the test-page sample marker only — caption marker badges are sized via `caption_badge_size` typography setting, not this constant. |

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
| `1_fullbleed` | 0 | full page + bleed | 303mm | 216mm |
| `2_portrait` | 0,1 | 1-6, 7-12 | 130.5mm | 172mm |
| `4_landscape` | 0-3 | 1-6/7-12 × top/bottom | 130.5mm | 84mm |
| `2l_1p` | 0,1 (landscape) | 1-8 stacked | 175.3mm | 84mm |
| `2l_1p` | 2 (portrait) | 9-12 | 85.7mm | 172mm |
| `1p_2l` | 0 (portrait) | 1-4 | 85.7mm | 172mm |
| `1p_2l` | 1,2 (landscape) | 5-12 stacked | 175.3mm | 84mm |

Half-canvas height: (172 - 4) / 2 = 84mm.

`1_fullbleed` is the only format that bypasses the 12-column grid and the safe canvas: the photo is placed at TikZ coordinates `(-3.5, -3.5) → (300.5, 213.5)` mm so it covers the full 303 × 216 mm bleed area defined by `templates/book.tex`'s `crop` package, plus a 0.5 mm overflow on each side (`fullBleedRasterEpsilonMM` in `internal/latex/formats.go`) that prevents rasterizer integer-pixel-grid rounding from leaving a sub-mm white row at the page bottom. The overflow is hard-clipped by the PDF media box and invisible in the output. Folio rendering is forced off and the footer captions strip is suppressed for the page (pagination of other pages is unaffected).

**Note:** For `2l_1p` and `1p_2l` formats, the column split can be adjusted via the `split_position` field on `book_pages` (default 0.5 = 8:4 columns). This allows customizing the width ratio between landscape and portrait slots.

### Canvas Zone Clipping

The entire canvas zone is wrapped in a TikZ `\clip` scope that prevents any content (photos, text, markers) from bleeding into the header or footer zones. The clip area is expanded horizontally by the `heading_color_bleed` value (default 4mm) so colored heading boxes can extend into the margins. Photos remain constrained by their own slot-level clips.

### Running Headers

- **Verso (even) pages**: Section title flush left (outside edge), 7.5pt italic, `black!40`
- **Recto (odd) pages**: Page description flush right (outside edge), 7.5pt italic, `black!40`
- **Fallback**: If a recto page has no description, the section title is shown instead
- Header zone (4mm) is always reserved even when empty

### Photo Captions — Footer System

Captions are sourced from `section_photos.description` (section-scoped). Instead of per-slot inline captions, all captions are collected into the footer zone:

- **Single-photo pages**: Caption alone in footer, no marker number
- **Multi-photo pages**: Sequential marker numbers (1, 2, 3...) assigned to photos with captions
  - **Marker overlay**: bold number on outside edge of photo (away from binding). Square box sized via the `caption_badge_size` typography setting (default 4 mm); inner font scales automatically as `size_mm × 1.5` pt (so 4 mm → 6 pt, 8 mm → 12 pt). Always renders identically to the matching footer caption badge. Uses chapter color when set (with auto-contrast text), otherwise white on semi-transparent dark background
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

**Empty pages are preserved.** A page with no photos and no text in any of its slots still renders into the export — the canvas zone is empty, but the page frame, separation rule, and folio are drawn as usual, and the page is counted in pagination so subsequent pages keep their expected folios. Use this for deliberate blank pages (section breaks, placeholders).

**Per-page folio suppression.** Setting `hide_page_number = true` on a page suppresses rendering of *that page's* folio number only. The page is still counted, so pages that follow it keep their continuous numbering — the suppressed page just shows an empty corner where the folio would have been. Useful for full-bleed photos, divider pages, or any page where the folio would visually intrude.

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

Each section with a title generates a full-page divider with the title centered in 28pt heading font (SemiBold weight) with letter-spacing (LetterSpace=5). Paired decorative rules (100mm wide, `black!20`) appear above and below the title for visual balance. No page number is displayed on divider pages.

**Recto alignment:** Section dividers always appear on recto (right-hand, odd) pages. If a divider would land on a verso (even) page, a blank page is automatically inserted before it.

### Text Slots

Text slots support Markdown formatting with auto-detected rendering types:

| Syntax | Result |
|--------|--------|
| `# Heading` | Large bold heading (colored background when chapter has color) |
| `## Subheading` | Medium bold subheading (colored background when chapter has color) |
| `**bold**` | **Bold text** |
| `*italic*` | *Italic text* |
| `- item` | Bulleted list |
| `1. item` | Numbered list |
| `> quote` | Blockquote (italic) |
| `->text<-` | Center-aligned paragraph |
| `->text->` | Right-aligned paragraph |
| `\| A \| B \|` | Table (GFM pipe syntax) |
| Blank line | Paragraph break |

**Table column widths**: Add percentages to the separator row to control column widths in PDF output:
```
| Name | Age |
|--- 60% ---|--- 40% ---|
| Alice | 30 |
```
Without percentages, columns are equal-width. Uses `tabularx` with `X` columns in LaTeX.

**Text type auto-detection** (`DetectTextType`):
- **T1 (explanation)**: Default — 5% gray fill, 10/13pt body, headings via markdown
- **T2 (fact box)**: All lines are list items — 5% gray fill, left accent bar (2pt `black!20`), compact list
- **T3 (oral history)**: Contains `> ` blockquote lines — 5% gray fill, centered italic quote

All text slots render on a light gray background (`black!5`), replacing the previous white-on-dark style.

### Typography Customization

Each book has configurable typography settings that control both PDF rendering and the frontend live preview:

| Setting | Default | Range | Description |
|---------|---------|-------|-------------|
| `body_font` | `pt-serif` | See font registry | Google Font for body text |
| `heading_font` | `source-sans-3` | See font registry | Google Font for headings |
| `body_font_size` | 11.0 pt | 6–36 pt | Body text size |
| `body_line_height` | 15.0 pt | 8–48 pt | Body text leading |
| `h1_font_size` | 18.0 pt | 6–36 pt | H1 heading size |
| `h2_font_size` | 13.0 pt | 6–36 pt | H2 heading size |
| `caption_opacity` | 0.85 | 0.0–1.0 | Caption text opacity (LaTeX `black!N`) |
| `caption_font_size` | 9.0 pt | 6–16 pt | Photo caption size |
| `heading_color_bleed` | 4.0 mm | 0–20 mm | How far colored heading boxes extend beyond content width into margins |
| `caption_badge_size` | 4.0 mm | 2–12 mm | Square dimension of caption marker badges. Drives both the on-photo overlay marker and the footer caption badge so they always render identically. Inner number scales as `size_mm × 1.5` pt. |
| `body_text_pad_mm` | 4.0 mm | 0–10 mm | Inner horizontal padding added to body text only on the side of a text slot adjacent to a photo in mixed layouts (`2_portrait`, `4_landscape`, `1p_2l`, `2l_1p`). Page-edge sides and sides next to non-photo neighbours (text/captions/empty) get no padding. Headings compensate via the same value so their colored box still reaches the slot edge — heading appearance is unchanged. |

**Font Registry:** 24 fonts available (13 serif, 11 sans-serif), defined in `internal/latex/fonts.go`. Each font has a `LatexName` (for `fontspec` family lookup in LuaLaTeX) and `GoogleFamily`/`GoogleSpec` (for browser preview — non–Google Fonts use a visually similar fallback). Fonts are validated on save via `latex.ValidateFont()`.

Variable fonts where `fontspec`'s family auto-detection fails to find a Bold face (Crimson Pro, Lora, Merriweather, Bitter, Gelasio, Source Serif 4, Cormorant Garamond, Nunito Sans, Raleway, Montserrat) carry additional `LatexFile` / `LatexItalicFile` fields naming the upright and italic variable-font files (e.g. `CrimsonPro[wght].ttf`). `FontEntry.LatexDeclaration()` then emits a bracket-file `\setmainfont` / `\setsansfont` command with explicit `wght=400` / `wght=700` axis features so `\textbf{}` and `\textbf{\textit{}}` render the correct weights instead of falling back to the regular face. Static fonts and well-behaved variable fonts (Noto Serif, Open Sans, Roboto, Inter, IBM Plex Sans, Noto Sans) keep the simpler family-name declaration.

**Available fonts:**
- **Serif:** PT Serif (default body), Libertinus Serif, EB Garamond, Lora, Merriweather, Noto Serif, Crimson Pro, Source Serif 4, Cormorant Garamond, Bitter, Gelasio, Bookman Old Style, URW Bookman
- **Sans-serif:** Source Sans 3 (default headings), PT Sans, Noto Sans, Open Sans, Lato, Roboto, Inter, Fira Sans, IBM Plex Sans, Nunito Sans, Raleway, Montserrat

> **Note:** Bookman Old Style is a proprietary Microsoft font and is not bundled with the Docker image — selecting it will fail PDF export until the TTF files are added to a `fonts/` directory and copied into the image. Use `URW Bookman` (a free clone shipped via Artifex's `urw-base35` set) as a drop-in alternative.

**Font API:** `GET /api/v1/fonts` returns all available fonts with `id`, `display_name`, `category`, `google_family`, `google_spec`.

**Frontend live preview:** The TypographyTab loads fonts via Google Fonts CSS links and applies CSS custom properties for real-time preview. Font selections and size adjustments are debounce-saved (500ms).

### Czech Typography

The template uses `polyglossia` with Czech as the default language. The body font (configurable, default PT Serif) and heading font (configurable, default Source Sans 3) are loaded via `fontspec`. This provides proper Czech hyphenation, ligatures, and diacritics support.

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
3. **Format samples**: One page per format (`1_fullscreen`, `1_fullbleed`, `2_portrait`, `4_landscape`, `2l_1p`, `1p_2l`) with colored placeholder rectangles labeled with dimensions
4-5. **Additional format samples** (continued)
6. **Gutter-safe visualization**: Red overlay on the 8mm gutter-safe zone with sample marker placement

### Professional Print Features

**Bleed & Crop Marks:** The PDF includes 3mm bleed on all sides using the LaTeX `crop` package. The total paper size is 303×216mm (A4 landscape + 3mm per side). Crop marks (corner registration marks) are rendered in the bleed area for precise trimming. For single-page preview exports, crop marks are disabled (the `crop` package uses `off` mode) while maintaining the same paper geometry. The TikZ content coordinate system remains anchored to the A4 trim area — no coordinate adjustments needed.

**PDF Compatibility:** `\pdfvariable minorversion 4` ensures PDF 1.4 output for broad printer compatibility.

**Blank Page Insertion:** Section dividers always appear on recto (odd) pages. Blank pages are automatically inserted to maintain this convention. These appear in the export report with format `"blank"`.

### Slot Aspect Ratios

With asymmetric margins (20mm inside, 12mm outside) and 4mm column gutters:

| Format | Slot | Size (mm) | Ratio | Closest Standard |
|--------|------|-----------|-------|-----------------|
| `4_landscape` | each | 130.5 × 84 | 1.55:1 | 3:2 landscape |
| `2_portrait` | each | 130.5 × 172 | 0.76:1 | 3:4 portrait |
| `1_fullscreen` | single | 265 × 172 | 1.54:1 | 3:2 landscape |
| `1_fullbleed` | single | 303 × 216 | 1.40:1 | full A4+bleed |
| `2l_1p` | landscape | 175.3 × 84 | 2.09:1 | panoramic |
| `2l_1p` | portrait | 85.7 × 172 | 0.50:1 | narrow portrait |
| `1p_2l` | portrait | 85.7 × 172 | 0.50:1 | narrow portrait |
| `1p_2l` | landscape | 175.3 × 84 | 2.09:1 | panoramic |

`4_landscape`, `2_portrait`, and `1_fullscreen` work well with standard photo ratios. The mixed formats (`2l_1p`, `1p_2l`) use 8:4 column splits for wider/narrower slots. `1_fullbleed` covers the full A4+bleed area; the photo's intrinsic ratio is preserved by object-cover cropping (use `crop_x`/`crop_y`/`crop_scale` on the slot to control framing).

### Print Preparation Checklist

- **Recommended DPI**: 300+ for high-quality prints, 200+ minimum
- **Fonts**: All text is embedded via configurable Google Fonts (OpenType, loaded by `fontspec`)
- **Deterministic output**: PDF timestamps are suppressed for reproducible builds
- **Bleed**: 3mm bleed on all sides with crop marks — ready for professional trimming
- **Color**: Decorative elements use subtle grays (`black!20`–`black!50`) for clean print output
- **Review**: Check export report for DPI warnings before sending to printer

### Backend Files

| File | Description |
|------|-------------|
| `internal/latex/formats.go` | `LayoutConfig`, 12-column grid system, `FormatSlotsGrid`, page format slot positions |
| `internal/latex/fonts.go` | Font registry (20 Google Fonts), `GetFont()`, `ValidateFont()`, `AllFonts()` |
| `internal/latex/latex.go` | PDF generation, typography resolution, caption lookup, DPI computation, export report |
| `internal/latex/markdown.go` | Markdown-to-LaTeX converter for text slots |
| `internal/latex/validate.go` | Layout validation (zone integrity, overlaps, gutter-safe markers) |
| `internal/latex/testpages.go` | Diagnostic test PDF generator |
| `internal/latex/templates/book.tex` | LaTeX template with TikZ, polyglossia, configurable fonts and layout |
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

**Synchronous vs. background job:** the `GET /export-pdf` endpoint above is synchronous — the request blocks for the entire ~4-minute export. It is preserved for CLI / curl / MCP callers. The web UI uses a separate asynchronous job flow so users see two progress bars (server-side generation + client-side download):

```
POST   /api/v1/books/:id/export-pdf/job      # 202 { job_id } (409 if one is running for the same book)
GET    /api/v1/book-export/:jobId            # current state
GET    /api/v1/book-export/:jobId/events     # SSE stream: progress, completed, job_error, cancelled
GET    /api/v1/book-export/:jobId/download   # streams compiled PDF via http.ServeContent
DELETE /api/v1/book-export/:jobId            # cancel (SIGKILLs lualatex, removes temp file)
```

Progress is emitted at phase granularity: `fetching_metadata`, `downloading_photos` (with per-photo `current`/`total` via an atomic counter), `compiling_pass1`, `compiling_pass2`. The compiled PDF is written to a temp file (not kept in memory — 700 MB books are routine in production) and served via `http.ServeContent`, which handles `Content-Length` and range requests. A TTL sweeper keeps completed-but-unconsumed exports for 1 hour, consumed exports for 10 minutes (retry window for network blips), and failed/cancelled jobs for 5 minutes. Only one active export per book is permitted at a time. See `docs/API.md` for the full event payload schemas.

## Backend Architecture

### Go Files

| File | Description |
|------|-------------|
| `internal/database/types.go` | `PhotoBook`, `BookSection`, `SectionPhoto`, `BookPage`, `PageSlot` structs; `PageFormatSlotCount()` |
| `internal/database/repository.go` | `BookReader` and `BookWriter` interfaces |
| `internal/database/provider.go` | `RegisterBookWriter()`, `GetBookWriter()`, `GetBookReader()` |
| `internal/database/postgres/books.go` | `BookRepository` implementing `BookWriter` |
| `internal/web/handlers/books.go` | `BooksHandler` with all REST endpoints |
| `internal/web/handlers/book_export_job.go` | `BookExportJob` + manager, 5 job-flow handlers, background runner with progress translator |
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
| `web/src/pages/BookEditor/index.tsx` | Editor shell — tabs (Sections, Pages, Preview, Typography, Duplicates), title editing |
| `web/src/pages/BookEditor/hooks/useBookData.ts` | Book data fetching and section photo loading |
| `web/src/pages/BookEditor/hooks/useUndoRedo.ts` | Undo/redo stack for slot assignments (assign, clear, swap) |
| `web/src/hooks/useBookKeyboardNav.ts` | Shared keyboard navigation hook (W/S prev/next, E/D chapter jump) |
| `web/src/pages/BookEditor/SectionsTab.tsx` | Sections tab — sidebar + photo pool + cross-section drag-and-drop |
| `web/src/pages/BookEditor/SectionSidebar.tsx` | Sortable chapter and section list, placement stats (placed/total) |
| `web/src/pages/BookEditor/SectionPhotoPool.tsx` | Photo grid (fixed 3-column) with selection, add by ID, modal description + note editing via PhotoDescriptionDialog |
| `web/src/pages/BookEditor/PhotoBrowserModal.tsx` | Full-screen modal to browse library and add photos |
| `web/src/pages/BookEditor/PagesTab.tsx` | Pages tab — DndContext for drag-to-slot, minimap, undo/redo |
| `web/src/pages/BookEditor/PageSidebar.tsx` | Thumbnail previews, quick-add (+) button, collapsible sections (persisted) |
| `web/src/pages/BookEditor/PageMinimap.tsx` | Compact visual overview of all pages grouped by section |
| `web/src/pages/BookEditor/PageTemplate.tsx` | CSS grid page layout with droppable slots |
| `web/src/pages/BookEditor/PageSlot.tsx` | Individual slot component (both draggable and droppable) |
| `web/src/pages/BookEditor/UnassignedPool.tsx` | Draggable photos with L/P orientation badges, description/note icons |
| `web/src/pages/BookEditor/PhotoDescriptionDialog.tsx` | Modal dialog for editing photo description + creator note |
| `web/src/pages/BookEditor/PhotoActionOverlay.tsx` | Hover overlay with View Detail, Find Similar, Copy ID actions |
| `web/src/pages/BookEditor/PhotoInfoOverlay.tsx` | Photo info overlay component |
| `web/src/pages/BookEditor/PreviewTab.tsx` | Read-only scrollable book preview with page descriptions |
| `web/src/pages/BookEditor/TypographyTab.tsx` | Font selection, size controls, caption opacity, live preview |
| `web/src/pages/BookEditor/DuplicatesTab.tsx` | Cross-section duplicate finder with one-click removal |
| `web/src/constants/bookTypography.ts` | Typography CSS defaults, font registry cache, CSS variable helpers |
| `web/src/utils/fontLoader.ts` | Google Fonts CSS loader (deduplicates, uses `display=swap`) |
| `web/src/pages/PhotoDetail/AddToBookDropdown.tsx` | Two-step picker (book → section) for adding photo to a book |
| `web/src/pages/PhotoDetail/BookMembership.tsx` | Sidebar panel showing which books/sections a photo belongs to |

### Dependencies

- `@dnd-kit/core` — drag-and-drop primitives (draggable, droppable, DragOverlay)
- `@dnd-kit/sortable` — sortable lists for sections and pages
- `@dnd-kit/utilities` — CSS transform helpers

### Drag-and-Drop Behavior

**Sections Tab**: Chapters and sections are reorderable via `@dnd-kit/sortable`. On drag-end, calls `reorderChapters()` or `reorderSections()`. Sections can be assigned to chapters via a dropdown selector. Photos can be dragged between sections — select photos, drag to a target section in the sidebar, and drop to move them. Multi-photo dragging shows a count badge on the drag overlay. Custom collision detection differentiates photo drags (snap to sections) from sortable drags (chapters/sections). Section sidebar shows placement stats (placed/total photos) with green highlight when complete.

**Pages Tab**: Pages are grouped by section in the sidebar with collapsible headers (collapse state persisted to localStorage) showing section title and page count. Each page shows thumbnail previews of its slots in a mini grid matching the page format. A quick-add (+) button next to each section header opens a format picker popover for fast page creation. Completed pages have green highlight; partially filled pages have rose highlight. A minimap panel provides a compact visual overview of all pages grouped by section with color-coded completion indicators. Pages are reorderable via `@dnd-kit/sortable` within the same section; cross-section drag is blocked. Global page numbering (1, 2, 3...) is maintained across all sections. Creating a new page auto-expands the target section if collapsed. Undo/redo (Ctrl+Z / Ctrl+Shift+Z) tracks slot assignments with up to 50 entries per stack. Photo assignment uses `@dnd-kit/core`:
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
| `1_fullbleed` | `grid-cols-1 grid-rows-1` | Single slot fills page; the editor preview drops the card padding (`p-0 overflow-hidden`) so the photo extends to the card edge, mirroring the print full-bleed |

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
