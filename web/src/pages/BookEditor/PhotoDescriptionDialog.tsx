import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { X } from 'lucide-react';
import { getThumbnailUrl, updateSectionPhoto } from '../../api/client';

interface Props {
  sectionId: string;
  photoUid: string;
  description: string;
  note: string;
  onSaved: () => void;
  onClose: () => void;
}

export function PhotoDescriptionDialog({ sectionId, photoUid, description, note, onSaved, onClose }: Props) {
  const { t } = useTranslation('pages');
  const [desc, setDesc] = useState(description);
  const [noteText, setNoteText] = useState(note);
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSectionPhoto(sectionId, photoUid, desc, noteText);
      onSaved();
    } catch {
      /* silent */
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70" onClick={onClose}>
      <div
        className="bg-slate-800 border border-slate-700 rounded-lg w-full max-w-md mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700">
          <h3 className="text-sm font-medium text-white">{t('books.editor.photoDetailsTitle')}</h3>
          <button onClick={onClose} className="text-slate-400 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4 space-y-4">
          <div className="flex justify-center">
            <img
              src={getThumbnailUrl(photoUid, 'fit_720')}
              alt=""
              className="max-h-40 rounded object-contain"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              {t('books.editor.descriptionLabel')}
            </label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder={t('books.editor.descriptionPlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus:ring-1 focus:ring-rose-500"
              rows={3}
              autoFocus
            />
            <p className="text-xs text-slate-500 mt-1">{t('books.editor.descriptionHelp')}</p>
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              {t('books.editor.noteLabel')}
            </label>
            <textarea
              value={noteText}
              onChange={(e) => setNoteText(e.target.value)}
              placeholder={t('books.editor.notePlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus:ring-1 focus:ring-amber-500"
              rows={2}
            />
            <p className="text-xs text-slate-500 mt-1">{t('books.editor.noteHelp')}</p>
          </div>
        </div>

        <div className="flex justify-end gap-2 px-4 py-3 border-t border-slate-700">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm text-slate-400 hover:text-white transition-colors"
          >
            {t('books.editor.closeModal')}
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-1.5 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white text-sm rounded transition-colors"
          >
            {saving ? '...' : t('books.editor.saveButton')}
          </button>
        </div>
      </div>
    </div>
  );
}
