import { useCallback, useState } from 'react';

// Shape of the click that drives selection. We only read the modifier flags,
// so callers can pass either a React.MouseEvent or a plain object — handy for
// the keyboard space-toggle path where there is no real mouse event.
export interface SelectionClickEvent {
  shiftKey?: boolean;
  metaKey?: boolean;
  ctrlKey?: boolean;
}

export interface UseGridSelectionReturn {
  selectedPhotos: Set<string>;
  // UID of the most recently toggled photo. The next shift-click extends the
  // selection from here to the clicked card. Null when no toggle has happened
  // yet (or after `deselectAll`).
  anchorUid: string | null;
  // Plain toggle (also updates anchor). Used by keyboard Space and by any
  // caller that already routes its own modifier logic.
  toggleSelection: (uid: string) => void;
  selectAll: (uids: string[]) => void;
  deselectAll: () => void;
  // Modifier-aware click router. `orderedUids` MUST be the array of UIDs as
  // they appear in the grid the user is looking at — that's how the range is
  // computed.
  handleSelectionClick: (
    uid: string,
    orderedUids: string[],
    event: SelectionClickEvent,
  ) => void;
}

// Reusable selection state for any grid of photos. Tracks the anchor UID so
// shift-click can select a contiguous range. Pages that need the full bulk-
// action surface (add to album, label, etc.) wrap this with
// `usePhotoSelection`; pages that only need primitives (Trash, etc.) call it
// directly.
export function useGridSelection(): UseGridSelectionReturn {
  const [selectedPhotos, setSelectedPhotos] = useState<Set<string>>(new Set());
  const [anchorUid, setAnchorUid] = useState<string | null>(null);

  const toggleSelection = useCallback((uid: string) => {
    setSelectedPhotos((prev) => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
    setAnchorUid(uid);
  }, []);

  const selectAll = useCallback((uids: string[]) => {
    setSelectedPhotos(new Set(uids));
    // Anchor stays put — Ctrl+A or a Select All button shouldn't reset the
    // last shift-click pivot the user was reaching for.
  }, []);

  const deselectAll = useCallback(() => {
    setSelectedPhotos(new Set());
    setAnchorUid(null);
  }, []);

  const handleSelectionClick = useCallback(
    (uid: string, orderedUids: string[], event: SelectionClickEvent) => {
      const shift = !!event.shiftKey;
      const meta = !!(event.metaKey ?? event.ctrlKey);

      // Shift-click: extend from anchor to clicked card. Falls back to a plain
      // toggle if there is no anchor or the anchor has scrolled out of view
      // (e.g. filters changed since the user last clicked).
      if (shift && anchorUid && anchorUid !== uid) {
        const fromIdx = orderedUids.indexOf(anchorUid);
        const toIdx = orderedUids.indexOf(uid);
        if (fromIdx !== -1 && toIdx !== -1) {
          const [start, end] = fromIdx < toIdx ? [fromIdx, toIdx] : [toIdx, fromIdx];
          setSelectedPhotos((prev) => {
            const next = new Set(prev);
            for (let i = start; i <= end; i++) next.add(orderedUids[i]);
            return next;
          });
          return;
        }
      }

      // Ctrl/Cmd-click: toggle single, leave anchor alone.
      if (meta) {
        setSelectedPhotos((prev) => {
          const next = new Set(prev);
          if (next.has(uid)) next.delete(uid);
          else next.add(uid);
          return next;
        });
        return;
      }

      // Plain click: toggle single, move anchor.
      toggleSelection(uid);
    },
    [anchorUid, toggleSelection],
  );

  return {
    selectedPhotos,
    anchorUid,
    toggleSelection,
    selectAll,
    deselectAll,
    handleSelectionClick,
  };
}
