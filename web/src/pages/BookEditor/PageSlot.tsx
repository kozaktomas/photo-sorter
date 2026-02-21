import { useState, useCallback } from 'react';
import { useDroppable, useDraggable } from '@dnd-kit/core';
import { useTranslation } from 'react-i18next';
import { X, Pencil, Type, Crop } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import { MarkdownContent } from '../../utils/markdown';

interface Props {
  pageId: string;
  slotIndex: number;
  photoUid: string;
  textContent?: string;
  cropX?: number;
  cropY?: number;
  cropScale?: number;
  onClear: () => void;
  onEditCrop?: () => void;
  description?: string;
  note?: string;
  onEditDescription?: () => void;
  onEditText?: () => void;
  onAddText?: () => void;
  className?: string;
}

export function PageSlotComponent({ pageId, slotIndex, photoUid, textContent, cropX, cropY, cropScale, onClear, onEditCrop, description, note, onEditDescription, onEditText, onAddText, className }: Props) {
  const { t } = useTranslation('pages');
  const [orientation, setOrientation] = useState<'L' | 'P' | null>(null);
  const droppableId = `slot-${pageId}-${slotIndex}`;
  const { isOver, setNodeRef: setDropRef } = useDroppable({
    id: droppableId,
    data: { pageId, slotIndex, photoUid, textContent },
  });

  const hasContent = !!photoUid || !!textContent;
  const draggableId = `slot-drag-${pageId}-${slotIndex}`;
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({
    id: draggableId,
    data: { photoUid, textContent, sourcePageId: pageId, sourceSlotIndex: slotIndex },
    disabled: !hasContent,
  });

  const combinedRef = useCallback((node: HTMLElement | null) => {
    setDropRef(node);
    setDragRef(node);
  }, [setDropRef, setDragRef]);

  return (
    <div
      ref={combinedRef}
      {...(hasContent ? { ...attributes, ...listeners } : {})}
      className={`relative rounded overflow-hidden transition-colors ${
        isOver ? 'ring-2 ring-rose-400' : ''
      } ${hasContent ? 'cursor-grab active:cursor-grabbing' : ''} ${
        isDragging ? 'opacity-30' : ''
      } ${className ?? ''}`}
    >
      {photoUid ? (
        <div className="group relative w-full h-full">
          <img
            src={getThumbnailUrl(photoUid, 'fit_720')}
            alt=""
            className="w-full h-full object-cover"
            style={{
              objectPosition: `${(cropX ?? 0.5) * 100}% ${(cropY ?? 0.5) * 100}%`,
              ...(cropScale && cropScale < 1 ? { transform: `scale(${1 / cropScale})`, transformOrigin: `${(cropX ?? 0.5) * 100}% ${(cropY ?? 0.5) * 100}%` } : {}),
            }}
            onLoad={(e) => {
              const img = e.currentTarget;
              setOrientation(img.naturalWidth >= img.naturalHeight ? 'L' : 'P');
            }}
          />
          <div className="absolute top-1 right-1 flex gap-0.5">
            {onEditCrop && (
              <button
                onClick={onEditCrop}
                onPointerDown={(e) => e.stopPropagation()}
                className="bg-black/60 hover:bg-black/80 text-white rounded p-0.5 transition-colors"
                title={t('books.editor.adjustCrop')}
              >
                <Crop className="h-3.5 w-3.5" />
              </button>
            )}
            <button
              onClick={onClear}
              onPointerDown={(e) => e.stopPropagation()}
              className="bg-black/60 hover:bg-red-600 text-white rounded p-0.5 transition-colors"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
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
      ) : textContent ? (
        <div className="group relative w-full h-full p-3">
          <MarkdownContent content={textContent} className="line-clamp-6" />
          <button
            onClick={onClear}
            onPointerDown={(e) => e.stopPropagation()}
            className="absolute top-1 right-1 bg-black/60 hover:bg-red-600 text-white rounded p-0.5 transition-colors"
          >
            <X className="h-3.5 w-3.5" />
          </button>
          {onEditText && (
            <button
              onClick={onEditText}
              onPointerDown={(e) => e.stopPropagation()}
              className="absolute top-1 left-1 bg-black/60 hover:bg-black/80 text-white rounded p-0.5 transition-colors"
              title={t('books.editor.editText')}
            >
              <Pencil className="h-3 w-3" />
            </button>
          )}
        </div>
      ) : (
        <div className="w-full h-full border-2 border-dashed border-slate-600 rounded flex flex-col items-center justify-center text-slate-500 text-xs gap-2">
          <span>{t('books.editor.dropHere')}</span>
          {onAddText && (
            <button
              onClick={onAddText}
              onPointerDown={(e) => e.stopPropagation()}
              className="flex items-center gap-1 px-2 py-1 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded text-xs transition-colors"
            >
              <Type className="h-3 w-3" />
              {t('books.editor.addText')}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
