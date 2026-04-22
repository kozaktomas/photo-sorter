import { useRef, useState, useCallback, useEffect } from 'react';
import { assignSlot, assignTextSlot, assignCaptionsSlot, assignContentsSlot, clearSlot, swapSlots, updatePage } from '../../../api/client';

export interface SlotContent {
  photoUid: string;
  textContent: string;
  isCaptions?: boolean;
  isContents?: boolean;
}

const EMPTY_CONTENT: SlotContent = { photoUid: '', textContent: '' };

function isEmptyContent(c: SlotContent): boolean {
  return !c.photoUid && !c.textContent && !c.isCaptions && !c.isContents;
}

export type SlotAction =
  | { type: 'assign'; pageId: string; slotIndex: number; prev: SlotContent; next: SlotContent }
  | { type: 'clear'; pageId: string; slotIndex: number; prev: SlotContent }
  | { type: 'swap'; pageId: string; slotIndexA: number; slotIndexB: number }
  | { type: 'move_page'; pageId: string; fromSectionId: string; toSectionId: string };

/** A single undo entry: one or more sub-actions executed atomically. */
export type UndoEntry = SlotAction[];

const MAX_STACK_SIZE = 50;

async function executeAction(action: SlotAction): Promise<void> {
  switch (action.type) {
    case 'assign':
      if (action.next.photoUid) {
        await assignSlot(action.pageId, action.slotIndex, action.next.photoUid);
      } else if (action.next.textContent) {
        await assignTextSlot(action.pageId, action.slotIndex, action.next.textContent);
      } else if (action.next.isCaptions) {
        await assignCaptionsSlot(action.pageId, action.slotIndex);
      } else if (action.next.isContents) {
        await assignContentsSlot(action.pageId, action.slotIndex);
      } else {
        await clearSlot(action.pageId, action.slotIndex);
      }
      break;
    case 'clear':
      await clearSlot(action.pageId, action.slotIndex);
      break;
    case 'swap':
      await swapSlots(action.pageId, action.slotIndexA, action.slotIndexB);
      break;
    case 'move_page':
      await updatePage(action.pageId, { section_id: action.toSectionId });
      break;
  }
}

function reverseAction(action: SlotAction): SlotAction {
  switch (action.type) {
    case 'assign':
      if (!isEmptyContent(action.prev)) {
        return { type: 'assign', pageId: action.pageId, slotIndex: action.slotIndex, prev: action.next, next: action.prev };
      }
      return { type: 'clear', pageId: action.pageId, slotIndex: action.slotIndex, prev: action.next };
    case 'clear':
      return { type: 'assign', pageId: action.pageId, slotIndex: action.slotIndex, prev: EMPTY_CONTENT, next: action.prev };
    case 'swap':
      return action; // swap is its own inverse
    case 'move_page':
      return { type: 'move_page', pageId: action.pageId, fromSectionId: action.toSectionId, toSectionId: action.fromSectionId };
  }
}

export function useUndoRedo(onRefresh: () => void, enabled = true) {
  const undoStack = useRef<UndoEntry[]>([]);
  const redoStack = useRef<UndoEntry[]>([]);
  const [canUndo, setCanUndo] = useState(false);
  const [canRedo, setCanRedo] = useState(false);
  const busyRef = useRef(false);

  const updateFlags = useCallback(() => {
    setCanUndo(undoStack.current.length > 0);
    setCanRedo(redoStack.current.length > 0);
  }, []);

  const push = useCallback((entry: UndoEntry) => {
    undoStack.current.push(entry);
    if (undoStack.current.length > MAX_STACK_SIZE) {
      undoStack.current.shift();
    }
    redoStack.current = [];
    updateFlags();
  }, [updateFlags]);

  const undo = useCallback(async () => {
    if (busyRef.current || undoStack.current.length === 0) return;
    busyRef.current = true;
    const entry = undoStack.current.pop()!;
    try {
      const reversed = [...entry].reverse().map(reverseAction);
      for (const action of reversed) {
        await executeAction(action);
      }
      redoStack.current.push(entry);
      updateFlags();
      onRefresh();
    } catch {
      onRefresh();
    } finally {
      busyRef.current = false;
    }
  }, [onRefresh, updateFlags]);

  const redo = useCallback(async () => {
    if (busyRef.current || redoStack.current.length === 0) return;
    busyRef.current = true;
    const entry = redoStack.current.pop()!;
    try {
      for (const action of entry) {
        await executeAction(action);
      }
      undoStack.current.push(entry);
      updateFlags();
      onRefresh();
    } catch {
      onRefresh();
    } finally {
      busyRef.current = false;
    }
  }, [onRefresh, updateFlags]);

  useEffect(() => {
    if (!enabled) return;
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable) return;

      const key = e.key.toLowerCase();
      if ((e.ctrlKey || e.metaKey) && key === 'z' && !e.shiftKey) {
        e.preventDefault();
        void undo();
      } else if ((e.ctrlKey || e.metaKey) && ((key === 'z' && e.shiftKey) || key === 'y')) {
        e.preventDefault();
        void redo();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [undo, redo, enabled]);

  return { push, undo, redo, canUndo, canRedo };
}
