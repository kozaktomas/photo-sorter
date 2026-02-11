# Photo Book

A planning tool for organizing photos into a printed landscape photo book. This is a planning-only tool — no PDF export.

## Workflow

1. **Create a book** — Give it a title
2. **Define sections** — Named groups (e.g., "Childhood", "Wedding", "Vacation")
3. **Prepick photos** — Browse the library and add photos to sections
4. **Write descriptions** — Add a description (caption for print) and optional note (internal) to each photo
5. **Create pages** — Choose a page format and assign to a section
6. **Add page descriptions** — Optional text displayed at the top of each page
7. **Assign photos to slots** — Drag photos from the unassigned pool into page slots
8. **Preview** — Review the full book layout with page descriptions and photo captions

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
├── description (text displayed at top of page)
├── sort_order
├── created_at
└── updated_at

page_slots
├── id (PK, BIGSERIAL)
├── page_id (FK → book_pages, CASCADE)
├── slot_index
├── photo_uid
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
| POST | `/api/v1/books/:id/pages` | Create page (`{ format, section_id? }`) |
| PUT | `/api/v1/books/:id/pages/reorder` | Reorder pages (`{ ids: [...] }`) |
| PUT | `/api/v1/pages/:id` | Update page (`{ format, section_id, description }`) |
| DELETE | `/api/v1/pages/:id` | Delete page |

### Slots

| Method | Endpoint | Description |
|--------|----------|-------------|
| PUT | `/api/v1/pages/:id/slots/:index` | Assign photo to slot (`{ photo_uid }`) |
| POST | `/api/v1/pages/:id/slots/swap` | Swap two slots atomically (`{ slot_a, slot_b }`) |
| DELETE | `/api/v1/pages/:id/slots/:index` | Clear slot |

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
- `AssignSlot`: UPSERT via `INSERT ... ON CONFLICT(page_id, slot_index) DO UPDATE`
- `ClearSlot`: `DELETE FROM page_slots WHERE page_id = $1 AND slot_index = $2`
- `SwapSlots`: Atomic swap in a transaction — reads both slots, deletes both, re-inserts with swapped `photo_uid` values. Required because `UNIQUE(page_id, photo_uid)` prevents concurrent updates
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
