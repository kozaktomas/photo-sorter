# Undo/Redo for Page Slot Assignments

Track page slot changes and allow undoing/redoing them with keyboard shortcuts.

## Requirements

- `Ctrl+Z` undoes the last slot action, `Ctrl+Shift+Z` (or `Ctrl+Y`) redoes
- Tracked actions:
  - **Assign**: photo or text placed into a slot (stores page ID, slot index, previous content, new content)
  - **Clear**: slot content removed (stores page ID, slot index, previous content)
  - **Swap**: two slots exchanged (stores page ID, slot A index, slot B index)
- Undo reverses the action by calling the appropriate API:
  - Assign undo: restore previous content (or clear if slot was empty)
  - Clear undo: re-assign the previous content
  - Swap undo: swap the same two slots again (swap is its own inverse)
- Redo replays the original action
- New actions after an undo truncate the redo history (standard undo behavior)
- Stack size limited to 50 actions
- Keyboard shortcuts do NOT fire when focus is in `<input>`, `<textarea>`, `<select>`, or `contenteditable` elements
- After undo/redo, refresh the book data to show updated state

## UI Details

- No visible undo/redo buttons needed (keyboard-only for now)
- Brief toast or subtle flash on the affected page slot after undo/redo to confirm the action (optional, nice-to-have)

## Implementation Notes

- New hook: `web/src/pages/BookEditor/hooks/useUndoRedo.ts`
- Action type: `{ type: 'assign' | 'clear' | 'swap'; pageId: string; slotIndex: number; slotIndexB?: number; prev?: { photoUid: string; textContent: string }; next?: { photoUid: string; textContent: string } }`
- Hook exposes: `push(action)`, `undo()`, `redo()`, `canUndo`, `canRedo`
- Integrate into `PagesTab.tsx`: wrap all slot-modifying operations to push onto the stack
- Use existing API functions: `assignSlot`, `clearSlot`, `swapSlots` for undo/redo execution
- Add `useEffect` in `PagesTab` for keyboard listener with proper input-field filtering
