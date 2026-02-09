import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useDraggable } from '@dnd-kit/core';
import { AlignLeft, StickyNote } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import type { SectionPhoto } from '../../types';

interface Props {
  photoUids: string[];
  sectionPhotos?: SectionPhoto[];
  onEditDescription?: (photoUid: string) => void;
}

function DraggablePhoto({ uid, hasDescription, hasNote, onEditDescription }: {
  uid: string;
  hasDescription: boolean;
  hasNote: boolean;
  onEditDescription?: () => void;
}) {
  const { t } = useTranslation('pages');
  const [orientation, setOrientation] = useState<'L' | 'P' | null>(null);
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: `photo-${uid}`,
    data: { photoUid: uid },
  });
  const style = transform ? {
    transform: `translate(${transform.x}px, ${transform.y}px)`,
    opacity: isDragging ? 0.5 : 1,
  } : undefined;

  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      style={style}
      className="group relative cursor-grab active:cursor-grabbing rounded overflow-hidden border border-slate-700 hover:border-rose-500/50 transition-colors"
    >
      <img
        src={getThumbnailUrl(uid, 'fit_720')}
        alt=""
        className="w-full aspect-square object-cover"
        loading="lazy"
        onLoad={(e) => {
          const img = e.currentTarget;
          setOrientation(img.naturalWidth >= img.naturalHeight ? 'L' : 'P');
        }}
      />
      {orientation && (
        <span className={`absolute bottom-0.5 right-0.5 text-[9px] font-bold leading-none px-1 py-0.5 rounded ${
          orientation === 'L' ? 'bg-blue-600/80 text-blue-100' : 'bg-amber-600/80 text-amber-100'
        }`}>
          {orientation === 'L' ? t('books.editor.orientationLandscape') : t('books.editor.orientationPortrait')}
        </span>
      )}
      {onEditDescription && (hasDescription || hasNote) && (
        <div className="absolute top-0.5 left-0.5 flex gap-0.5">
          {hasDescription && (
            <span className="bg-black/60 rounded p-0.5">
              <AlignLeft className="h-2.5 w-2.5 text-blue-400" />
            </span>
          )}
          {hasNote && (
            <span className="bg-black/60 rounded p-0.5">
              <StickyNote className="h-2.5 w-2.5 text-amber-400" />
            </span>
          )}
        </div>
      )}
      <PhotoActionOverlay photoUid={uid} />
    </div>
  );
}

export function UnassignedPool({ photoUids, sectionPhotos, onEditDescription }: Props) {
  const { t } = useTranslation('pages');

  const photoLookup = useMemo(() => {
    const map = new Map<string, SectionPhoto>();
    sectionPhotos?.forEach(sp => map.set(sp.photo_uid, sp));
    return map;
  }, [sectionPhotos]);

  if (photoUids.length === 0) {
    return (
      <div className="text-sm text-slate-500 py-4 text-center">
        {t('books.editor.noUnassigned')}
      </div>
    );
  }

  return (
    <div>
      <h3 className="text-sm font-medium text-slate-400 mb-2">
        {t('books.editor.unassignedPhotos')} ({photoUids.length})
      </h3>
      <div className="grid grid-cols-4 md:grid-cols-6 gap-2">
        {photoUids.map(uid => {
          const sp = photoLookup.get(uid);
          return (
            <DraggablePhoto
              key={uid}
              uid={uid}
              hasDescription={!!sp?.description}
              hasNote={!!sp?.note}
              onEditDescription={onEditDescription ? () => onEditDescription(uid) : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}
