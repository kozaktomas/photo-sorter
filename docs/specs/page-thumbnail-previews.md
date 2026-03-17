# Page Sidebar Thumbnail Previews

Replace the verbose "Page N" labels in the PageSidebar with compact thumbnail previews showing what's in each page's slots.

## Requirements

- Each page item in `PageSidebar` (`SortablePageItem`) shows a row of tiny thumbnails representing its slots
- Photo slots show a mini thumbnail using `getThumbnailUrl(photo_uid, 'tile_50')`
- Text-only slots show a small `Type` icon (from lucide-react) with a muted background
- Empty slots show a small dashed-border placeholder rectangle
- The page number is shown as a compact label (just the number, e.g. "1", "2") to the left of the thumbnails
- The format label and slot fill count (e.g. "4L - 2/4") moves to a second line or tooltip
- Thumbnail size: approximately 20x20px with 1px gap, fitting within the 264px sidebar width

## UI Details

- Thumbnails are rendered in a flex row after the grip handle and page number
- Each thumbnail has `rounded-sm` and `object-cover` for consistent appearance
- The existing completion color coding (emerald for complete, rose for selected) is preserved on the outer container
- The delete button remains in its current position on the right
- If a page has more slots than can fit (unlikely with max 4 slots), show all of them — 4 x 20px + gaps easily fits in the available space

## Edge Cases

- Pages with no slots assigned show all placeholder rectangles
- Photos that fail to load show a fallback gray rectangle
- Text-only slots are visually distinct from empty slots (solid background vs dashed border)
