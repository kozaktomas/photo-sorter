import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Plus, Trash2, CheckSquare, Square } from 'lucide-react';
import { removeSectionPhotos, updateSectionPhoto } from '../../api/client';
import { getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoBrowserModal } from './PhotoBrowserModal';
import type { SectionPhoto } from '../../types';

interface Props {
  sectionId: string;
  photos: SectionPhoto[];
  onRefresh: () => void;
  onReloadPhotos: () => void;
}

export function SectionPhotoPool({ sectionId, photos, onRefresh, onReloadPhotos }: Props) {
  const { t } = useTranslation('pages');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [showBrowser, setShowBrowser] = useState(false);
  const [editingDesc, setEditingDesc] = useState<string | null>(null);
  const [descText, setDescText] = useState('');
  const [editingNote, setEditingNote] = useState<string | null>(null);
  const [noteText, setNoteText] = useState('');

  const toggleSelect = useCallback((uid: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  }, []);

  const handleRemoveSelected = async () => {
    if (selected.size === 0) return;
    try {
      await removeSectionPhotos(sectionId, Array.from(selected));
      setSelected(new Set());
      onReloadPhotos();
      onRefresh();
    } catch { /* silent */ }
  };

  const handleDescBlur = async (photoUid: string) => {
    const photo = photos.find(p => p.photo_uid === photoUid);
    try {
      await updateSectionPhoto(sectionId, photoUid, descText, photo?.note || '');
      onReloadPhotos();
    } catch { /* silent */ }
    setEditingDesc(null);
  };

  const handleNoteBlur = async (photoUid: string) => {
    const photo = photos.find(p => p.photo_uid === photoUid);
    try {
      await updateSectionPhoto(sectionId, photoUid, photo?.description || '', noteText);
      onReloadPhotos();
    } catch { /* silent */ }
    setEditingNote(null);
  };

  const handlePhotosAdded = () => {
    setShowBrowser(false);
    onReloadPhotos();
    onRefresh();
  };

  if (photos.length === 0) {
    return (
      <div className="flex-1">
        <div className="flex justify-end mb-3">
          <button
            onClick={() => setShowBrowser(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-rose-600 hover:bg-rose-700 text-white text-sm rounded transition-colors"
          >
            <Plus className="h-4 w-4" />
            {t('books.editor.addPhotos')}
          </button>
        </div>
        <div className="text-center text-slate-500 py-12">{t('books.editor.noPhotos')}</div>
        {showBrowser && (
          <PhotoBrowserModal
            sectionId={sectionId}
            existingUids={[]}
            onClose={() => setShowBrowser(false)}
            onAdded={handlePhotosAdded}
          />
        )}
      </div>
    );
  }

  return (
    <div className="flex-1">
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-slate-400">
          {photos.length} {t('books.photos')}
          {selected.size > 0 && ` (${selected.size} selected)`}
        </span>
        <div className="flex gap-2">
          {selected.size > 0 && (
            <button
              onClick={handleRemoveSelected}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t('books.editor.removeSelected')}
            </button>
          )}
          <button
            onClick={() => setShowBrowser(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-rose-600 hover:bg-rose-700 text-white text-sm rounded transition-colors"
          >
            <Plus className="h-4 w-4" />
            {t('books.editor.addPhotos')}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
        {photos.map((photo) => (
          <div key={photo.photo_uid} className="bg-slate-800 border border-slate-700 rounded-lg overflow-hidden">
            <div className="group relative cursor-pointer" onClick={() => toggleSelect(photo.photo_uid)}>
              <img
                src={getThumbnailUrl(photo.photo_uid, 'tile_224')}
                alt=""
                className="w-full aspect-square object-cover"
                loading="lazy"
              />
              <div className="absolute top-1.5 left-1.5">
                {selected.has(photo.photo_uid) ? (
                  <CheckSquare className="h-5 w-5 text-rose-400" />
                ) : (
                  <Square className="h-5 w-5 text-white/50" />
                )}
              </div>
              <PhotoActionOverlay photoUid={photo.photo_uid} />
            </div>
            <div className="p-2 space-y-1">
              {/* Description */}
              {editingDesc === photo.photo_uid ? (
                <textarea
                  value={descText}
                  onChange={(e) => setDescText(e.target.value)}
                  onBlur={() => handleDescBlur(photo.photo_uid)}
                  onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleDescBlur(photo.photo_uid); } }}
                  className="w-full px-2 py-1 bg-slate-900 border border-slate-600 rounded text-xs text-white resize-none focus:outline-none focus:ring-1 focus:ring-rose-500"
                  rows={2}
                  autoFocus
                />
              ) : (
                <div
                  onClick={() => { setEditingDesc(photo.photo_uid); setDescText(photo.description); }}
                  className={`text-xs min-h-[1.5rem] px-1 py-0.5 rounded cursor-text ${
                    photo.description ? 'text-slate-300' : 'text-slate-600 italic'
                  }`}
                >
                  {photo.description || t('books.editor.descriptionPlaceholder')}
                </div>
              )}
              {/* Note */}
              {editingNote === photo.photo_uid ? (
                <textarea
                  value={noteText}
                  onChange={(e) => setNoteText(e.target.value)}
                  onBlur={() => handleNoteBlur(photo.photo_uid)}
                  onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleNoteBlur(photo.photo_uid); } }}
                  className="w-full px-2 py-1 bg-slate-900 border border-amber-700/50 rounded text-xs text-white resize-none focus:outline-none focus:ring-1 focus:ring-amber-500"
                  rows={2}
                  autoFocus
                />
              ) : (
                <div
                  onClick={() => { setEditingNote(photo.photo_uid); setNoteText(photo.note); }}
                  className={`text-xs min-h-[1.5rem] px-1 py-0.5 rounded cursor-text ${
                    photo.note ? 'text-amber-400/80' : 'text-slate-600 italic'
                  }`}
                >
                  {photo.note || t('books.editor.notePlaceholder')}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>

      {showBrowser && (
        <PhotoBrowserModal
          sectionId={sectionId}
          existingUids={photos.map(p => p.photo_uid)}
          onClose={() => setShowBrowser(false)}
          onAdded={handlePhotosAdded}
        />
      )}
    </div>
  );
}
