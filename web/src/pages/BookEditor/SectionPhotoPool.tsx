import { useState, useCallback, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { Plus, Trash2, CheckSquare, Square } from 'lucide-react';
import { useDraggable } from '@dnd-kit/core';
import { removeSectionPhotos, getPhoto, addSectionPhotos, getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import { PhotoBrowserModal } from './PhotoBrowserModal';
import { PhotoDescriptionDialog } from './PhotoDescriptionDialog';
import type { SectionPhoto } from '../../types';

const GRID_COLS = 3;

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
        <PhotoInfoOverlay description={photo.description} note={photo.note} fileName={photo.file_name} />
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

  const [focusIndex, setFocusIndex] = useState(-1);
  const gridRef = useRef<HTMLDivElement>(null);

  // Clear selection and focus when switching sections
  useEffect(() => {
    setSelected(new Set());
    setFocusIndex(-1);
  }, [sectionId]);

  // Reset focus if it's out of bounds
  useEffect(() => {
    if (focusIndex >= photos.length) setFocusIndex(photos.length - 1);
  }, [photos.length, focusIndex]);

  const toggleSelect = useCallback((uid: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  }, []);

  const handleRemoveSelected = useCallback(async () => {
    if (selected.size === 0) return;
    try {
      await removeSectionPhotos(sectionId, Array.from(selected));
      setSelected(new Set());
      onReloadPhotos();
      onRefresh();
    } catch { /* silent */ }
  }, [selected, sectionId, onReloadPhotos, onRefresh]);

  // Grid keyboard navigation
  useEffect(() => {
    if (editingPhoto || showBrowser) return;

    const handler = (e: KeyboardEvent) => {
      const tag = (document.activeElement?.tagName ?? '').toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
      if (document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable) return;
      if (photos.length === 0) return;

      const { key } = e;

      if (key === 'ArrowRight' || key === 'ArrowLeft' || key === 'ArrowDown' || key === 'ArrowUp') {
        e.preventDefault();
        setFocusIndex(prev => {
          const cur = prev < 0 ? 0 : prev;
          if (key === 'ArrowRight') return Math.min(cur + 1, photos.length - 1);
          if (key === 'ArrowLeft') return Math.max(cur - 1, 0);
          if (key === 'ArrowDown') return Math.min(cur + GRID_COLS, photos.length - 1);
          if (key === 'ArrowUp') return Math.max(cur - GRID_COLS, 0);
          return cur;
        });
        return;
      }

      if (key === 'Enter' && focusIndex >= 0 && focusIndex < photos.length) {
        e.preventDefault();
        setEditingPhoto(photos[focusIndex]);
        return;
      }

      if (key === ' ' && focusIndex >= 0 && focusIndex < photos.length) {
        e.preventDefault();
        toggleSelect(photos[focusIndex].photo_uid);
        return;
      }

      if (key === 'Delete' && selected.size > 0) {
        e.preventDefault();
        void handleRemoveSelected();
        return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [editingPhoto, showBrowser, photos, focusIndex, selected, toggleSelect, handleRemoveSelected]);

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

      <div ref={gridRef} className="grid grid-cols-3 gap-3">
        {photos.map((photo, idx) => (
          <div key={photo.photo_uid} className={`rounded-lg ${idx === focusIndex ? 'ring-2 ring-rose-500' : ''}`}>
            <DraggablePhoto
              photo={photo}
              sectionId={sectionId}
              selected={selected}
              onToggleSelect={() => toggleSelect(photo.photo_uid)}
            >
              <div
                className="px-2 py-1.5 cursor-pointer hover:bg-slate-700/50 transition-colors text-center"
                onClick={() => setEditingPhoto(photo)}
              >
                <span className="text-xs text-slate-500 hover:text-slate-300">
                  {t('books.editor.editDescription')}
                </span>
              </div>
            </DraggablePhoto>
          </div>
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
