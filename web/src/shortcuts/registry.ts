// Shared registry of every keyboard shortcut surfaced in the app.
//
// The global listener (useGlobalShortcuts) looks shortcuts up here, and the
// `?` overlay (ShortcutsModal) renders the same definitions. Adding a new
// shortcut means one entry here plus matching i18n keys under `shortcuts.*`
// in pages.json — no parallel registries to drift apart.

export type ShortcutScope = 'global' | 'photosGrid' | 'photoDetail' | 'bookEditor';

export interface ShortcutDef {
  // Stable identifier used by pages to register a handler.
  id: string;
  scope: ShortcutScope;
  // Raw KeyboardEvent.key values that should trigger this shortcut. Letter
  // entries are matched case-insensitively so `Shift+f` still fires `f`.
  keys: string[];
  // Pretty display string for the cheatsheet (e.g. "J / →").
  displayKeys: string;
  // i18n key under the `pages` namespace.
  descriptionKey: string;
}

export const SHORTCUT_SCOPE_ORDER: ShortcutScope[] = [
  'global',
  'photosGrid',
  'photoDetail',
  'bookEditor',
];

export const SCOPE_TITLE_KEYS: Record<ShortcutScope, string> = {
  global: 'shortcuts.scopes.global',
  photosGrid: 'shortcuts.scopes.photosGrid',
  photoDetail: 'shortcuts.scopes.photoDetail',
  bookEditor: 'shortcuts.scopes.bookEditor',
};

export const SHORTCUTS: ShortcutDef[] = [
  // Global
  {
    id: 'global.help',
    scope: 'global',
    keys: ['?'],
    displayKeys: '?',
    descriptionKey: 'shortcuts.items.global.help',
  },

  // Photos grid
  {
    id: 'photosGrid.next',
    scope: 'photosGrid',
    keys: ['j'],
    displayKeys: 'J',
    descriptionKey: 'shortcuts.items.photosGrid.next',
  },
  {
    id: 'photosGrid.prev',
    scope: 'photosGrid',
    keys: ['k'],
    displayKeys: 'K',
    descriptionKey: 'shortcuts.items.photosGrid.prev',
  },
  {
    id: 'photosGrid.toggleSelect',
    scope: 'photosGrid',
    keys: [' '],
    displayKeys: 'Space',
    descriptionKey: 'shortcuts.items.photosGrid.toggleSelect',
  },
  {
    id: 'photosGrid.openDetail',
    scope: 'photosGrid',
    keys: ['Enter'],
    displayKeys: 'Enter',
    descriptionKey: 'shortcuts.items.photosGrid.openDetail',
  },
  // The next two live in the cheatsheet for discoverability only. They use
  // modifier keys (Ctrl/Cmd+A) or are claimed by modals (Esc), neither of
  // which the global registry's matcher handles — `useSelectionShortcuts`
  // owns the actual key handling and is wired per page.
  {
    id: 'photosGrid.selectAll',
    scope: 'photosGrid',
    keys: [],
    displayKeys: 'Ctrl/⌘ + A',
    descriptionKey: 'shortcuts.items.photosGrid.selectAll',
  },
  {
    id: 'photosGrid.clearSelection',
    scope: 'photosGrid',
    keys: [],
    displayKeys: 'Esc',
    descriptionKey: 'shortcuts.items.photosGrid.clearSelection',
  },

  // Photo detail
  {
    id: 'photoDetail.next',
    scope: 'photoDetail',
    keys: ['j', 'ArrowRight'],
    displayKeys: 'J / →',
    descriptionKey: 'shortcuts.items.photoDetail.next',
  },
  {
    id: 'photoDetail.prev',
    scope: 'photoDetail',
    keys: ['k', 'ArrowLeft'],
    displayKeys: 'K / ←',
    descriptionKey: 'shortcuts.items.photoDetail.prev',
  },
  {
    id: 'photoDetail.favorite',
    scope: 'photoDetail',
    keys: ['f'],
    displayKeys: 'F',
    descriptionKey: 'shortcuts.items.photoDetail.favorite',
  },
  {
    id: 'photoDetail.edit',
    scope: 'photoDetail',
    keys: ['e'],
    displayKeys: 'E',
    descriptionKey: 'shortcuts.items.photoDetail.edit',
  },
  {
    id: 'photoDetail.archive',
    scope: 'photoDetail',
    keys: ['a'],
    displayKeys: 'A',
    descriptionKey: 'shortcuts.items.photoDetail.archive',
  },
  {
    id: 'photoDetail.close',
    scope: 'photoDetail',
    keys: ['Escape'],
    displayKeys: 'Esc',
    descriptionKey: 'shortcuts.items.photoDetail.close',
  },

  // Book editor — surfaced here for discoverability but the actual key
  // handling stays inside `useBookKeyboardNav` (which predates this
  // registry). Keep the ids prefixed `bookEditor.*` and out of the active
  // handler map so the two systems can't collide.
  {
    id: 'bookEditor.prevItem',
    scope: 'bookEditor',
    keys: ['w'],
    displayKeys: 'W',
    descriptionKey: 'shortcuts.items.bookEditor.prevItem',
  },
  {
    id: 'bookEditor.nextItem',
    scope: 'bookEditor',
    keys: ['s'],
    displayKeys: 'S',
    descriptionKey: 'shortcuts.items.bookEditor.nextItem',
  },
  {
    id: 'bookEditor.prevChapter',
    scope: 'bookEditor',
    keys: ['e'],
    displayKeys: 'E',
    descriptionKey: 'shortcuts.items.bookEditor.prevChapter',
  },
  {
    id: 'bookEditor.nextChapter',
    scope: 'bookEditor',
    keys: ['d'],
    displayKeys: 'D',
    descriptionKey: 'shortcuts.items.bookEditor.nextChapter',
  },
];

// Shortcut ids that the global listener should never dispatch — the
// definitions exist so the cheatsheet can list them, but a separate
// component owns the actual key handling.
export const NON_DISPATCHED_SHORTCUTS: ReadonlySet<string> = new Set([
  'bookEditor.prevItem',
  'bookEditor.nextItem',
  'bookEditor.prevChapter',
  'bookEditor.nextChapter',
  'photosGrid.selectAll',
  'photosGrid.clearSelection',
]);

function isLetterKey(s: string): boolean {
  return s.length === 1 && /^[a-zA-Z]$/.test(s);
}

// Match a KeyboardEvent.key against a shortcut's `keys`. Letter keys match
// case-insensitively (capslock tolerance) but only when Shift is NOT held,
// so plain `f` and a hypothetical `Shift+f` stay distinct. Non-letter keys
// (Enter, Escape, ArrowLeft, `?`, ` `, etc.) match exact strings only.
export function shortcutMatchesKey(
  shortcut: ShortcutDef,
  eventKey: string,
  shiftKey: boolean,
): boolean {
  for (const candidate of shortcut.keys) {
    if (isLetterKey(candidate)) {
      if (shiftKey) continue;
      if (candidate.toLowerCase() === eventKey.toLowerCase()) return true;
    } else if (candidate === eventKey) {
      return true;
    }
  }
  return false;
}
