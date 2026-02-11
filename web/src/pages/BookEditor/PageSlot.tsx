import { useState, useCallback } from 'react';
import { useDroppable, useDraggable } from '@dnd-kit/core';
import { useTranslation } from 'react-i18next';
import { X, Pencil } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';

interface Props {
  pageId: string;
  slotIndex: number;
  photoUid: string;
  onClear: () => void;
  description?: string;
  note?: string;
  onEditDescription?: () => void;
  className?: string;
}

export function PageSlotComponent({ pageId, slotIndex, photoUid, onClear, description, note, onEditDescription, className }: Props) {
  const { t } = useTranslation('pages');
  const [orientation, setOrientation] = useState<'L' | 'P' | null>(null);
  const droppableId = `slot-${pageId}-${slotIndex}`;
  const { isOver, setNodeRef: setDropRef } = useDroppable({
    id: droppableId,
    data: { pageId, slotIndex, photoUid },
  });

  const draggableId = `slot-drag-${pageId}-${slotIndex}`;
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({
    id: draggableId,
    data: { photoUid, sourcePageId: pageId, sourceSlotIndex: slotIndex },
    disabled: !photoUid,
  });

  const combinedRef = useCallback((node: HTMLElement | null) => {
    setDropRef(node);
    setDragRef(node);
  }, [setDropRef, setDragRef]);

  return (
    <div
      ref={combinedRef}
      {...(photoUid ? { ...attributes, ...listeners } : {})}
      className={`relative rounded overflow-hidden transition-colors ${
        isOver ? 'ring-2 ring-rose-400' : ''
      } ${photoUid ? 'cursor-grab active:cursor-grabbing' : ''} ${
        isDragging ? 'opacity-30' : ''
      } ${className || ''}`}
    >
      {photoUid ? (
        <div className="group relative w-full h-full">
          <img
            src={getThumbnailUrl(photoUid, 'fit_720')}
            alt=""
            className="w-full h-full object-cover"
            onLoad={(e) => {
              const img = e.currentTarget;
              setOrientation(img.naturalWidth >= img.naturalHeight ? 'L' : 'P');
            }}
          />
          <button
            onClick={onClear}
            onPointerDown={(e) => e.stopPropagation()}
            className="absolute top-1 right-1 bg-black/60 hover:bg-red-600 text-white rounded p-0.5 transition-colors"
          >
            <X className="h-3.5 w-3.5" />
          </button>
          {onEditDescription && (
            <button
              onClick={onEditDescription}
              onPointerDown={(e) => e.stopPropagation()}
              className="absolute top-1 left-1 bg-black/60 hover:bg-black/80 text-white rounded p-0.5 transition-colors"
              title={t('books.editor.descriptionLabel')}
            >
              <Pencil className="h-3 w-3" />
            </button>
          )}
          <PhotoInfoOverlay
            description={description}
            note={note}
            orientation={orientation}
          />
          <PhotoActionOverlay photoUid={photoUid} />
        </div>
      ) : (
        <div className="w-full h-full border-2 border-dashed border-slate-600 rounded flex items-center justify-center text-slate-500 text-xs">
          {t('books.editor.dropHere')}
        </div>
      )}
    </div>
  );
}
