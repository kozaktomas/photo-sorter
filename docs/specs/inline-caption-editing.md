# Inline Caption Editing from Page Slots

Allow editing a photo's caption (description) and note directly from the page slot in the Pages tab, without switching to the Sections tab.

## Requirements

- Photo slots in the page editor show a pencil icon button (already exists via `onEditDescription` prop) that opens the `PhotoDescriptionDialog`
- The dialog is already implemented (`PhotoDescriptionDialog.tsx`) and accepts `photoUid`, `sectionId`, `description`, `note`
- Currently the `onEditDescription` callback is only wired up in `SectionPhotoPool.tsx`, not in `PagesTab.tsx`
- Wire `onEditDescription` in `PagesTab.tsx` for every photo slot: when clicked, open `PhotoDescriptionDialog` with the photo's current description/note from `sectionPhotos`
- After saving (dialog calls `updateSectionPhoto` API), refresh the section photos to reflect the change
- The description pencil button should appear on hover, same position as in `PageSlot.tsx` (top-left, already coded)

## Requirements Detail

- Find the photo's section from the page's `section_id`
- Look up the photo's current description/note from `sectionPhotos[sectionId]`
- Open `PhotoDescriptionDialog` with these values
- On save, call existing `updateSectionPhoto(sectionId, photoUid, { description, note })` and reload section photos

## Implementation Notes

- Modify `web/src/pages/BookEditor/PagesTab.tsx` to:
  1. Add state for the description dialog: `editingPhoto: { pageId, slotIndex, photoUid, sectionId } | null`
  2. Pass `onEditDescription` to each `PageSlotComponent` that has a photo
  3. Render `PhotoDescriptionDialog` when `editingPhoto` is set
- The `PageSlotComponent` already accepts and renders the `onEditDescription` prop - it just needs to be provided from PagesTab
- No new components needed, no new API calls needed
