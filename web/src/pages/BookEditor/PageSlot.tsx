import { useState, useEffect, useCallback } from 'react';
import { useDroppable, useDraggable } from '@dnd-kit/core';
import { useTranslation } from 'react-i18next';
import { X, Pencil, Type, Crop, MessageSquareText } from 'lucide-react';
import { getThumbnailUrl, getPhoto } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import { MarkdownContent } from '../../utils/markdown';
import { computeEffectiveDpi } from '../../utils/pageFormats';
import type { PageFormat } from '../../types';

interface Props {
  pageId: string;
  slotIndex: number;
  photoUid: string;
  textContent?: string;
  isCaptionsSlot?: boolean;
  cropX?: number;
  cropY?: number;
  cropScale?: number;
  format?: PageFormat;
  splitPosition?: number | null;
  onClear: () => void;
  onEditCrop?: () => void;
  description?: string;
  note?: string;
  fileName?: string;
  onEditDescription?: () => void;
  onEditText?: () => void;
  onAddText?: () => void;
  onAddCaptions?: () => void;
  chapterColor?: string;
  bleedLeft?: boolean;
  bleedRight?: boolean;
  textPaddingClass?: string;
  className?: string;
}

export function PageSlotComponent({ pageId, slotIndex, photoUid, textContent, isCaptionsSlot, cropX, cropY, cropScale, format, splitPosition, onClear, onEditCrop, description, note, fileName, onEditDescription, onEditText, onAddText, onAddCaptions, chapterColor, bleedLeft, bleedRight, textPaddingClass, className }: Props) {
  const { t } = useTranslation('pages');
  const [orientation, setOrientation] = useState<'L' | 'P' | null>(null);
  const [dpi, setDpi] = useState<number | null>(null);
  const droppableId = `slot-${pageId}-${slotIndex}`;
  const { isOver, setNodeRef: setDropRef } = useDroppable({
    id: droppableId,
    data: { pageId, slotIndex, photoUid, textContent },
  });

  const hasContent = !!photoUid || !!textContent || !!isCaptionsSlot;
  const draggableId = `slot-drag-${pageId}-${slotIndex}`;
  const { attributes, listeners, setNodeRef: setDragRef, isDragging } = useDraggable({
    id: draggableId,
    data: { photoUid, textContent, sourcePageId: pageId, sourceSlotIndex: slotIndex },
    disabled: !hasContent || !!isCaptionsSlot,
  });

  const combinedRef = useCallback((node: HTMLElement | null) => {
    setDropRef(node);
    setDragRef(node);
  }, [setDropRef, setDragRef]);

  // Fetch original photo dimensions for accurate DPI calculation
  useEffect(() => {
    if (!photoUid || !format) {
      setDpi(null);
      return;
    }
    let cancelled = false;
    getPhoto(photoUid).then(photo => {
      if (!cancelled && photo.width && photo.height) {
        setDpi(computeEffectiveDpi(photo.width, photo.height, format, slotIndex, splitPosition));
      }
    }).catch(() => { /* ignore */ });
    return () => { cancelled = true; };
  }, [photoUid, format, slotIndex, splitPosition]);

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
          {dpi !== null && (
            <span
              className={`absolute bottom-1 left-1 bg-black/60 rounded px-1 text-[10px] font-mono ${
                dpi >= 300 ? 'text-green-400' : dpi >= 200 ? 'text-amber-400' : 'text-red-400'
              }`}
              title={t('books.editor.dpiLabel')}
            >
              {dpi}
            </span>
          )}
          <PhotoInfoOverlay
            description={description}
            note={note}
            fileName={fileName}
            orientation={orientation}
          />
          <PhotoActionOverlay photoUid={photoUid} />
        </div>
      ) : textContent ? (
        <div className={`group relative w-full h-full p-3 ${textPaddingClass ?? ''}`}>
          <MarkdownContent content={textContent} className="line-clamp-6" chapterColor={chapterColor} bleedLeft={bleedLeft} bleedRight={bleedRight} />
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
      ) : isCaptionsSlot ? (
        <div className="relative w-full h-full border-2 border-dashed border-rose-500/60 bg-rose-900/10 rounded flex flex-col items-center justify-center text-rose-200 text-xs gap-2 p-3 text-center">
          <MessageSquareText className="h-5 w-5 text-rose-400" />
          <span className="font-semibold">{t('books.editor.captionsSlotLabel')}</span>
          <span className="text-rose-300/80 text-[10px]">{t('books.editor.captionsSlotHint')}</span>
          <button
            onClick={onClear}
            onPointerDown={(e) => e.stopPropagation()}
            className="absolute top-1 right-1 bg-black/60 hover:bg-red-600 text-white rounded p-0.5 transition-colors"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      ) : (
        <div className="w-full h-full border-2 border-dashed border-slate-600 rounded flex flex-col items-center justify-center text-slate-500 text-xs gap-2">
          <span>{t('books.editor.dropHere')}</span>
          <div className="flex flex-wrap gap-1 justify-center">
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
            {onAddCaptions && (
              <button
                onClick={onAddCaptions}
                onPointerDown={(e) => e.stopPropagation()}
                className="flex items-center gap-1 px-2 py-1 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded text-xs transition-colors"
                title={t('books.editor.useForCaptionsTitle')}
              >
                <MessageSquareText className="h-3 w-3" />
                {t('books.editor.useForCaptions')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
