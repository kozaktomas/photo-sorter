# Cross-Section Duplicate Finder

A new tab in the book editor that identifies photos appearing in multiple sections, with the ability to remove them from specific sections.

## Requirements

- Add a "Duplicates" tab to the book editor tab bar (after "Preview")
- When the tab is activated, load photos for all sections (using existing `loadSectionPhotos`)
- Scan all section photos and find photo UIDs that appear in 2 or more sections
- Display each duplicate photo as a card showing:
  - Photo thumbnail (using `getThumbnailUrl`)
  - List of section names where the photo appears
  - A remove button (X or trash icon) next to each section name
- Clicking remove calls `removeSectionPhotos(sectionId, [photoUid])` and refreshes the data
- Show a count of total duplicates at the top (e.g. "Found 5 photos in multiple sections")
- Show an empty state when no duplicates are found: "No duplicate photos found across sections"
- Show a loading state while section photos are being loaded

## UI Details

- Cards use the standard dark theme (bg-slate-800, border-slate-700)
- Photo thumbnail is 80x80px on the left side of each card
- Section names are listed to the right with their remove buttons
- Remove button is a small red-on-hover trash/X icon next to the section name
- After removing, the card updates immediately (photo may disappear if down to 1 section)

## Implementation Notes

- New file: `web/src/pages/BookEditor/DuplicatesTab.tsx`
- Add `'duplicates'` to the `Tab` type union in `BookEditor/index.tsx`
- Add i18n keys for both `cs` and `en` locales (tab label, empty state, count)
- All data comes from existing `sectionPhotos` — no new API endpoints needed
- Build a `Map<string, string[]>` mapping `photo_uid` to array of `section_id`s
