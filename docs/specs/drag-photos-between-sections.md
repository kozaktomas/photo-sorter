# Drag Photos Between Sections

Allow dragging photos from one section's photo pool to another section in the Sections tab sidebar.

## Requirements

- Photos in `SectionPhotoPool` become draggable (in addition to existing click-to-select)
- Section items in `SectionSidebar` become valid drop targets
- When a photo is dropped on a different section:
  1. Remove the photo from the source section (`removeSectionPhotos`)
  2. Add the photo to the target section (`addSectionPhotos`)
  3. Refresh the book data
- Visual feedback during drag:
  - The dragged photo shows as a small thumbnail in a `DragOverlay`
  - Target section items highlight when a photo hovers over them (e.g. rose border glow)
  - The source section does not highlight as a valid drop target
- Dragging must coexist with click-to-select (use `PointerSensor` with distance threshold >=5px to distinguish click from drag)
- Multiple selected photos can be dragged together (drag initiates from any selected photo, all selected photos move)

## UI Details

- Drag overlay shows a single thumbnail (16x16 or 24x24) with a badge showing count if multiple selected
- Valid drop target sections show `border-rose-400 bg-rose-500/10` highlight
- Invalid/same section shows no highlight change
- After successful drop, the photo pool updates to reflect the move

## Implementation Notes

- Wrap `SectionsTab` content in a `DndContext` from `@dnd-kit/core`
- Add `useDraggable` to photo cards in `SectionPhotoPool` with data `{ photoUid, sourceSectionId }`
- Add `useDroppable` to section items in `SectionSidebar` (or use the existing sortable IDs with a new prefix)
- Handle `onDragEnd` in `SectionsTab`: check if source section !== target section, then call remove + add APIs
- The DnD context in SectionsTab is separate from the existing DnD context in SectionSidebar (which handles section reordering)
