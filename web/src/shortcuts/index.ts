export { ShortcutsProvider } from './ShortcutsContext';
export { useRegisterShortcut } from './useRegisterShortcut';
export { ShortcutsModal } from './ShortcutsModal';
export { useGlobalShortcuts } from './useGlobalShortcuts';
export {
  SHORTCUTS,
  SHORTCUT_SCOPE_ORDER,
  SCOPE_TITLE_KEYS,
  shortcutMatchesKey,
  NON_DISPATCHED_SHORTCUTS,
} from './registry';
export type { ShortcutDef, ShortcutScope } from './registry';
