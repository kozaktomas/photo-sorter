# Refresh docs/photo-book.md

`docs/photo-book.md` is the deep dive on the photo book feature (planner + LaTeX PDF export). Bring it in sync with the current schema, layout system, typography settings, and export pipeline.

## Scope

- File to edit: `docs/photo-book.md` only.
- Source of truth: `internal/database/postgres/migrations/` (for tables: `photo_books`, `book_chapters`, `book_sections`, `section_photos`, `book_pages`, `page_slots`, `text_versions`, `text_check_results`), `internal/latex/` (PDF rendering + validation), `internal/web/handlers/books.go` + `internal/web/handlers/book_export_job.go`, and `web/src/pages/BookEditor/`.

## What to verify and update

1. **Data model** — every column on `photo_books`, `book_chapters`, `book_sections`, `section_photos`, `book_pages`, and `page_slots` must be listed and described accurately. Key fields to confirm:
   - Typography on `photo_books`: `body_font`, `heading_font`, `body_font_size`, `body_line_height`, `h1_font_size`, `h2_font_size`, `caption_opacity`, `caption_font_size`, `heading_color_bleed`, `caption_badge_size`, `body_text_pad_mm`.
   - `book_chapters.color` for per-chapter theme.
   - `book_pages.style` (modern / archival), `book_pages.split_position` (0.2-0.8), `book_pages.hide_page_number`.
   - `page_slots`: `photo_uid` / `text_content` / `is_captions_slot` / `is_contents_slot` mutual exclusion; `crop_x` / `crop_y` / `crop_scale`.
2. **Hierarchy** — Book → Chapters (optional) → Sections → Pages → Slots. Confirm cross-section moves are atomic via `BookWriter.MovePageToSection`.
3. **Page formats** — list every supported format with slot count and layout (`4_landscape`, `2l_1p`, `1p_2l`, `2_portrait`, `1_fullscreen`, `1_fullbleed`). Confirm `1_fullbleed` covers A4+3mm bleed (303×216 mm) and auto-suppresses folio + footer captions; manual-only (not produced by auto-layout).
4. **Layout grid** — 12-column grid, fixed zones (header 4 mm / canvas 172 mm / footer 8 mm), asymmetric margins (inside 20 mm / outside 12 mm), adjustable split for mixed layouts.
5. **Text slots** — GFM markdown support: headings, bold, italic, lists, blockquotes, tables (with optional column-width percentages), alignment macros (`->text<-`, `->text->`). T1 / T2 / T3 type auto-detection. Rendering: frontend via marked + DOMPurify with `<colgroup>` width injection; PDF via `tabularx` with `\hsize`-scaled `X` columns.
6. **Captions slot + contents slot** — exclusive per-page; routing of FooterCaption list / table of contents into the slot grid; bottom captions strip suppressed when a captions slot is active.
7. **Auto-layout** — described correctly; does not produce `1_fullbleed`.
8. **Preflight** — empty slots, low DPI, unplaced photos; `photo_quality=low|medium|original` tier-specific warnings (e.g. `original_downgrade` for primaries below 3 840 px on the longest side).
9. **Export pipeline** — synchronous `GET /api/v1/books/{id}/export-pdf` for CLI/MCP; async job flow (`POST .../export-pdf/job`, SSE events, download, cancel) for the UI; phases `fetching_metadata`, `downloading_photos`, `compiling_pass1`, `compiling_pass2`; HEIC/RAW fallback to `fit_7680` thumbnail; original tier capped at 8 000 px longest side.
10. **MCP surface parity** — `update_book` accepts the full typography payload; `update_page` accepts `hide_page_number` and cross-section moves; `assign_captions_slot` and `assign_contents_slot` route page lists; `auto_layout`, `preflight`, and the PDF job flow are web-API-only.
11. **Fonts** — pointer to the registry in `internal/latex/` and to the host-side install requirement (`make install-fonts`); Bookman Old Style is proprietary and not auto-installed.
12. **Text tools** — AI text check (3-tier cache: in-memory → DB → OpenAI; `suggestions[]` with severity `major`/`minor`), rewrite (length adjustment), consistency check across all book texts; pricing in `internal/config/prices.yaml`.

## Rules

- Code is the source of truth. Open the migration and handler before documenting.
- Do not invent fields, formats, layouts, or behaviors. Grep before you write.
- Preserve the existing section structure; do surgical edits.
- Where behavior is tricky (slot mutual exclusion, fullbleed folio suppression, blank pages preserved end-to-end), state it explicitly.
- Avoid duplicating the REST endpoint table that lives in `docs/API.md`; link instead.

## Done when

- Every column on every photo-book-related table matches the migrations.
- Every page format, slot type, and typography setting present in code is documented.
- The export pipeline (sync + async job flow with photo_quality tiers) is accurately described.
- MCP parity statements match `internal/mcp/` handlers.
- No documentation of removed fields, removed formats, or removed flags.