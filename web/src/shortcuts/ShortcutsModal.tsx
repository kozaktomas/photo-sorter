import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Keyboard } from 'lucide-react';
import {
  SCOPE_TITLE_KEYS,
  SHORTCUT_SCOPE_ORDER,
  SHORTCUTS,
  type ShortcutScope,
} from './registry';

interface Props {
  open: boolean;
  onClose: () => void;
}

export function ShortcutsModal({ open, onClose }: Props) {
  const { t } = useTranslation('pages');

  useEffect(() => {
    if (!open) return;
    function handler(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open, onClose]);

  if (!open) return null;

  const grouped = new Map<ShortcutScope, typeof SHORTCUTS>();
  for (const scope of SHORTCUT_SCOPE_ORDER) grouped.set(scope, []);
  for (const shortcut of SHORTCUTS) {
    grouped.get(shortcut.scope)?.push(shortcut);
  }

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/70 p-4"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="shortcuts-modal-title"
    >
      <div
        className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl w-full max-w-lg overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700">
          <div className="flex items-center gap-2">
            <Keyboard className="h-4 w-4 text-rose-400" />
            <h2 id="shortcuts-modal-title" className="text-sm font-medium text-white">
              {t('shortcuts.title')}
            </h2>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-white"
            aria-label={t('shortcuts.close')}
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4 space-y-5 max-h-[70vh] overflow-y-auto">
          {SHORTCUT_SCOPE_ORDER.map((scope) => {
            const items = grouped.get(scope) ?? [];
            if (items.length === 0) return null;
            return (
              <section key={scope}>
                <h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">
                  {t(SCOPE_TITLE_KEYS[scope])}
                </h3>
                <ul className="space-y-1">
                  {items.map((shortcut) => (
                    <li
                      key={shortcut.id}
                      className="flex items-center justify-between py-1"
                    >
                      <span className="text-sm text-slate-300">
                        {t(shortcut.descriptionKey)}
                      </span>
                      <kbd className="px-2 py-0.5 bg-slate-900 border border-slate-600 rounded text-xs text-slate-300 font-mono">
                        {shortcut.displayKeys}
                      </kbd>
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      </div>
    </div>
  );
}
