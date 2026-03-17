# Section Photo Placement Stats

Show per-section progress stats in the Sections tab sidebar so the user can see how many photos have been placed on pages vs remaining.

## Requirements

- In `SectionSidebar`, each section item displays placement stats below the title alongside the existing photo count
- Stats show: photos placed on pages / total photos (e.g. "15/21 placed")
- "Placed" means the photo UID appears in any page slot within that section's pages
- Count unique photo UIDs only (same photo in multiple slots counts once)
- Stats update after any page slot change (assign, clear, swap) when `onRefresh` triggers
- Use muted color (slate-500) for the stats text, emerald accent when fully placed (all photos assigned)

## Implementation Notes

- Pass `book.pages` as a new `pages: BookPage[]` prop from `SectionsTab` to `SectionSidebar`, then to `SortableItem`
- Compute placed count: filter pages by `section_id`, flatMap slots, collect unique `photo_uid` values
- Display next to the existing `Image` icon + photo count line (line 113-115 in `SectionSidebar.tsx`)
