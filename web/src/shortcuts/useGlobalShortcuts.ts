import { useEffect } from 'react';
import { NON_DISPATCHED_SHORTCUTS, SHORTCUTS, shortcutMatchesKey } from './registry';
import { useShortcutHandlersRef } from './useRegisterShortcut';

// Installs a single window-level keydown listener that walks the SHORTCUTS
// registry and dispatches to whichever handler the active page has
// registered. Mount this exactly once, high in the tree (the Layout).
//
// Noop conditions:
//   - focus is inside an <input>, <textarea>, or contenteditable element
//   - any of Ctrl / Meta / Alt is held (we never claim bare browser combos)
export function useGlobalShortcuts() {
  const handlersRef = useShortcutHandlersRef();

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      const target = e.target as HTMLElement | null;
      if (target instanceof HTMLElement) {
        const tag = target.tagName.toLowerCase();
        if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
        if (target.isContentEditable) return;
      }

      for (const shortcut of SHORTCUTS) {
        if (NON_DISPATCHED_SHORTCUTS.has(shortcut.id)) continue;
        if (!shortcutMatchesKey(shortcut, e.key, e.shiftKey)) continue;
        const handler = handlersRef.current.get(shortcut.id);
        if (!handler) continue;
        e.preventDefault();
        handler();
        return;
      }
    }

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [handlersRef]);
}
