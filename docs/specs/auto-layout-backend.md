# Auto-Layout Backend Endpoint

A new API endpoint that generates pages with optimal format choices based on photo orientations in a section's unassigned pool.

## Requirements

- `POST /api/v1/books/{id}/sections/{sectionId}/auto-layout` generates pages for unassigned photos
- Request body (all optional):
  - `prefer_formats`: array of allowed formats (default: all 5 formats)
  - `max_pages`: maximum pages to create (default: unlimited)
- The endpoint:
  1. Gets all section photos from the database
  2. Gets existing pages for this section to determine which photos are already assigned to slots
  3. Filters to unassigned photos only
  4. Classifies each photo as landscape or portrait by querying PhotoPrism for photo dimensions (use `GetPhotos` with the UIDs)
  5. Runs the layout algorithm (see below) to determine page formats
  6. Creates pages and assigns photos to slots in the database
  7. Returns the list of created pages with their slots
- Response: `{ pages_created: number, photos_placed: number, pages: []BookPage }`

## Layout Algorithm

Priority order for photo groups:
1. **4 landscapes** → `4_landscape` (2x2 grid)
2. **2 landscapes + 1 portrait** → `2l_1p`
3. **1 portrait + 2 landscapes** → `1p_2l`
4. **2 portraits** → `2_portrait`
5. **Remaining singles** → `1_fullscreen`

Steps:
1. Separate photos into landscape and portrait lists (landscape = width >= height)
2. While 4+ landscapes remain: create `4_landscape` page
3. While 2+ landscapes and 1+ portrait remain: alternate `2l_1p` and `1p_2l` pages
4. While 2+ portraits remain: create `2_portrait` page
5. While 1+ landscape remain: pair with portrait if available as `2_portrait`, else `1_fullscreen`
6. Remaining portraits: `1_fullscreen`

## Implementation Notes

- Add handler method to `BooksHandler` in `internal/web/handlers/books.go`
- Register route in the books router
- Photo dimension lookup: use `PhotoPrism.GetPhotos()` with filter, check `Width`/`Height` fields on the Photo struct
- Create pages with sequential sort_order after existing pages
- Use existing `CreatePage` and `AssignSlot` repository methods
- Update `docs/API.md` with the new endpoint
