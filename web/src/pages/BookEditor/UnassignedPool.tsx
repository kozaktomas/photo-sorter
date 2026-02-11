import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useDraggable } from '@dnd-kit/core';
import { getThumbnailUrl } from '../../api/client';
import { PhotoActionOverlay } from './PhotoActionOverlay';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import type { SectionPhoto } from '../../types';

interface Props {
  photoUids: string[];
  sectionPhotos?: SectionPhoto[];
  onEditDescription?: (photoUid: string) => void;
}

function DraggablePhoto({ uid, description, note }: {
  uid: string;
  description: string;
  note: string;
}) {
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
      <PhotoInfoOverlay
        description={description}
        note={note}
        orientation={orientation}
        compact
      />
      <PhotoActionOverlay photoUid={uid} />
    </div>
  );
}

export function UnassignedPool({ photoUids, sectionPhotos }: Props) {
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
              description={sp?.description ?? ''}
              note={sp?.note ?? ''}
            />
          );
        })}
      </div>
    </div>
  );
}
