import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Keyboard } from 'lucide-react';

interface Props {
  open: boolean;
  onClose: () => void;
}

interface ShortcutEntry {
  keys: string;
  labelKey: string;
}

interface ShortcutGroup {
  titleKey: string;
  shortcuts: ShortcutEntry[];
}

export function KeyboardShortcutsHelp({ open, onClose }: Props) {
  const { t } = useTranslation('pages');

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open, onClose]);

  if (!open) return null;

  const groups: ShortcutGroup[] = [
    {
      titleKey: 'books.editor.shortcuts.globalTitle',
      shortcuts: [
        { keys: '1–5', labelKey: 'books.editor.shortcuts.switchTab' },
        { keys: '?', labelKey: 'books.editor.shortcuts.showHelp' },
      ],
    },
    {
      titleKey: 'books.editor.shortcuts.gridTitle',
      shortcuts: [
        { keys: '← → ↑ ↓', labelKey: 'books.editor.shortcuts.navigatePhotos' },
        { keys: 'Enter', labelKey: 'books.editor.shortcuts.openEdit' },
        { keys: 'Space', labelKey: 'books.editor.shortcuts.toggleSelect' },
        { keys: 'Delete', labelKey: 'books.editor.shortcuts.removeSelected' },
      ],
    },
    {
      titleKey: 'books.editor.shortcuts.pagesTitle',
      shortcuts: [
        { keys: 'W / S', labelKey: 'books.editor.shortcuts.prevNextPage' },
        { keys: 'E / D', labelKey: 'books.editor.shortcuts.prevNextSection' },
        { keys: 'Enter', labelKey: 'books.editor.shortcuts.openSlotEdit' },
        { keys: 'Escape', labelKey: 'books.editor.shortcuts.deselectSlot' },
      ],
    },
    {
      titleKey: 'books.editor.shortcuts.modalTitle',
      shortcuts: [
        { keys: 'Escape', labelKey: 'books.editor.shortcuts.closeDialog' },
        { keys: 'Ctrl+Enter', labelKey: 'books.editor.shortcuts.saveAndClose' },
        { keys: 'Ctrl+Shift+C', labelKey: 'books.editor.shortcuts.aiTextCheck' },
      ],
    },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70" onClick={onClose}>
      <div
        className="bg-slate-800 border border-slate-700 rounded-lg w-full max-w-md mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700">
          <div className="flex items-center gap-2">
            <Keyboard className="h-4 w-4 text-rose-400" />
            <h3 className="text-sm font-medium text-white">{t('books.editor.keyboardShortcuts')}</h3>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4 space-y-4 max-h-[70vh] overflow-y-auto">
          {groups.map((group) => (
            <div key={group.titleKey}>
              <h4 className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">
                {t(group.titleKey)}
              </h4>
              <div className="space-y-1">
                {group.shortcuts.map((shortcut) => (
                  <div key={shortcut.labelKey} className="flex items-center justify-between py-1">
                    <span className="text-sm text-slate-300">{t(shortcut.labelKey)}</span>
                    <kbd className="px-2 py-0.5 bg-slate-900 border border-slate-600 rounded text-xs text-slate-300 font-mono">
                      {shortcut.keys}
                    </kbd>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
