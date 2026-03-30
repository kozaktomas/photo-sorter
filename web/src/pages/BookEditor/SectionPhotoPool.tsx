import { useState, useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Plus, Trash2, CheckSquare, Square } from 'lucide-react';
import { useDraggable } from '@dnd-kit/core';
import { removeSectionPhotos, getPhoto, addSectionPhotos, getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoBrowserModal } from './PhotoBrowserModal';
import { PhotoDescriptionDialog } from './PhotoDescriptionDialog';
import type { SectionPhoto } from '../../types';

function DraggablePhoto({ photo, sectionId, selected, onToggleSelect, children }: {
  photo: SectionPhoto;
  sectionId: string;
  selected: Set<string>;
  onToggleSelect: () => void;
  children: React.ReactNode;
}) {
  const isInSelection = selected.has(photo.photo_uid);
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `photo-${photo.photo_uid}`,
    data: {
      type: 'photo',
      photoUid: photo.photo_uid,
      sourceSectionId: sectionId,
      selectedUids: isInSelection && selected.size > 1 ? Array.from(selected) : [photo.photo_uid],
    },
  });

  return (
    <div className={`bg-slate-800 border border-slate-700 rounded-lg overflow-hidden ${isDragging ? 'opacity-30' : ''}`}>
      <div
        ref={setNodeRef}
        {...attributes}
        {...listeners}
        className="group relative cursor-pointer"
        onClick={onToggleSelect}
      >
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
      {children}
    </div>
  );
}

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
  const [editingPhoto, setEditingPhoto] = useState<SectionPhoto | null>(null);
  const [addByIdValue, setAddByIdValue] = useState('');
  const [addByIdError, setAddByIdError] = useState('');
  const [addByIdLoading, setAddByIdLoading] = useState(false);

  // Clear selection when switching sections
  useEffect(() => {
    setSelected(new Set());
  }, [sectionId]);

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

  const handleDescriptionSaved = () => {
    setEditingPhoto(null);
    onReloadPhotos();
  };

  const handleAddById = async () => {
    const uid = addByIdValue.trim();
    if (!uid) return;
    if (photos.some(p => p.photo_uid === uid)) {
      setAddByIdError(t('books.editor.photoAlreadyInSection'));
      return;
    }
    setAddByIdLoading(true);
    setAddByIdError('');
    try {
      await getPhoto(uid);
      await addSectionPhotos(sectionId, [uid]);
      setAddByIdValue('');
      onReloadPhotos();
      onRefresh();
    } catch {
      setAddByIdError(t('books.editor.photoNotFound'));
    } finally {
      setAddByIdLoading(false);
    }
  };

  const handlePhotosAdded = () => {
    setShowBrowser(false);
    onReloadPhotos();
    onRefresh();
  };

  if (photos.length === 0) {
    return (
      <div className="flex-1">
        <div className="flex justify-end items-center gap-2 mb-3">
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={addByIdValue}
              onChange={(e) => { setAddByIdValue(e.target.value); setAddByIdError(''); }}
              onKeyDown={(e) => { if (e.key === 'Enter') void handleAddById(); }}
              placeholder={t('books.editor.addByIdPlaceholder')}
              className="w-32 px-2 py-1.5 bg-slate-800 border border-slate-600 rounded text-sm text-white placeholder-slate-500 focus:outline-none focus:border-rose-500"
              disabled={addByIdLoading}
            />
            <button
              onClick={() => void handleAddById()}
              disabled={addByIdLoading || !addByIdValue.trim()}
              className="px-2.5 py-1.5 bg-slate-700 hover:bg-slate-600 disabled:opacity-50 text-white text-sm rounded transition-colors"
            >
              {t('books.editor.addById')}
            </button>
            {addByIdError && <span className="text-xs text-red-400">{addByIdError}</span>}
          </div>
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
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={addByIdValue}
              onChange={(e) => { setAddByIdValue(e.target.value); setAddByIdError(''); }}
              onKeyDown={(e) => { if (e.key === 'Enter') void handleAddById(); }}
              placeholder={t('books.editor.addByIdPlaceholder')}
              className="w-32 px-2 py-1.5 bg-slate-800 border border-slate-600 rounded text-sm text-white placeholder-slate-500 focus:outline-none focus:border-rose-500"
              disabled={addByIdLoading}
            />
            <button
              onClick={() => void handleAddById()}
              disabled={addByIdLoading || !addByIdValue.trim()}
              className="px-2.5 py-1.5 bg-slate-700 hover:bg-slate-600 disabled:opacity-50 text-white text-sm rounded transition-colors"
            >
              {t('books.editor.addById')}
            </button>
            {addByIdError && <span className="text-xs text-red-400">{addByIdError}</span>}
          </div>
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

      <div className="grid grid-cols-3 gap-3">
        {photos.map((photo) => (
          <DraggablePhoto
            key={photo.photo_uid}
            photo={photo}
            sectionId={sectionId}
            selected={selected}
            onToggleSelect={() => toggleSelect(photo.photo_uid)}
          >
            <div
              className="p-2 space-y-1 cursor-pointer hover:bg-slate-700/50 transition-colors"
              onClick={() => setEditingPhoto(photo)}
            >
              <div className={`text-xs truncate px-1 py-0.5 ${
                photo.description ? 'text-slate-300' : 'text-slate-600 italic'
              }`}>
                {photo.description || t('books.editor.descriptionPlaceholder')}
              </div>
              <div className={`text-xs truncate px-1 py-0.5 ${
                photo.note ? 'text-amber-400/80' : 'text-slate-600 italic'
              }`}>
                {photo.note || t('books.editor.notePlaceholder')}
              </div>
            </div>
          </DraggablePhoto>
        ))}
      </div>

      {editingPhoto && (
        <PhotoDescriptionDialog
          sectionId={sectionId}
          photoUid={editingPhoto.photo_uid}
          description={editingPhoto.description}
          note={editingPhoto.note}
          onSaved={handleDescriptionSaved}
          onClose={() => setEditingPhoto(null)}
        />
      )}

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
