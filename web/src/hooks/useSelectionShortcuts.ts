import { useEffect, useRef } from 'react';

interface Options {
  enabled?: boolean;
  // Fired on Ctrl/Cmd+A. Receive nothing — the caller already knows what
  // "everything in the grid" means.
  onSelectAll?: () => void;
  // Fired on Esc. Caller should typically only wire this when a selection is
  // actually active, so an empty Esc still lets modals etc. close themselves.
  onClear?: () => void;
}

// Adds the two cross-page selection shortcuts the rest of the keyboard
// system can't easily express: Ctrl/Cmd+A (browser-claimed combo that the
// global registry intentionally ignores) and Esc (often handled by modals,
// so callers gate `onClear` to only fire when they actually own the gesture).
//
// Stashes handlers in refs so callers don't have to memoize, and skips
// keystrokes that originate inside form fields.
export function useSelectionShortcuts({ enabled = true, onSelectAll, onClear }: Options) {
  const onSelectAllRef = useRef(onSelectAll);
  const onClearRef = useRef(onClear);
  onSelectAllRef.current = onSelectAll;
  onClearRef.current = onClear;

  useEffect(() => {
    if (!enabled) return;

    function onKeyDown(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null;
      if (target instanceof HTMLElement) {
        const tag = target.tagName.toLowerCase();
        if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
        if (target.isContentEditable) return;
      }

      // Ctrl/Cmd+A → select all rendered photos.
      if (
        (e.ctrlKey || e.metaKey) &&
        !e.shiftKey &&
        !e.altKey &&
        e.key.toLowerCase() === 'a' &&
        onSelectAllRef.current
      ) {
        e.preventDefault();
        onSelectAllRef.current();
        return;
      }

      // Esc → clear selection. We only swallow the key if the caller actually
      // wired `onClear`, so modals/dialogs that own Esc keep working when no
      // selection is active.
      if (
        e.key === 'Escape' &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey &&
        onClearRef.current
      ) {
        e.preventDefault();
        onClearRef.current();
        return;
      }
    }

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [enabled]);
}
