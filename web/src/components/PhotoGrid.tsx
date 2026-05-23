import { useEffect, useRef } from 'react';
import { PhotoCardLink, PhotoCard } from './PhotoCard';
import type { Photo } from '../types';

interface PhotoGridProps {
  photos: Photo[];
  onPhotoClick?: (photo: Photo) => void;
  selectable?: boolean;
  selectedPhotos?: Set<string>;
  // Receives the click UID and the underlying mouse event so the caller can
  // route shift/ctrl/meta clicks (range select, additive toggle) without the
  // grid needing to know about anchors.
  onSelectionChange?: (uid: string, event?: React.MouseEvent) => void;
  // UID of the keyboard-focused card (drawn with a yellow ring). When set,
  // the grid scrolls that card into view as the focus moves.
  focusedUid?: string | null;
}

function FocusRing({ focused, children }: { focused: boolean; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focused) {
      ref.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [focused]);
  return (
    <div ref={ref} className={focused ? 'rounded-lg ring-2 ring-amber-400 ring-offset-2 ring-offset-slate-900' : ''}>
      {children}
    </div>
  );
}

export function PhotoGrid({ photos, onPhotoClick, selectable, selectedPhotos, onSelectionChange, focusedUid }: PhotoGridProps) {
  if (photos.length === 0) {
    return (
      <div className="text-center py-12 text-slate-400">
        No photos found
      </div>
    );
  }

  // Selection mode: render PhotoCard with selection props
  if (selectable && selectedPhotos && onSelectionChange) {
    return (
      <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
        {photos.map((photo) => (
          <FocusRing key={photo.uid} focused={focusedUid === photo.uid}>
            <PhotoCard
              photoUid={photo.uid}
              selectable
              selected={selectedPhotos.has(photo.uid)}
              onSelectionChange={(_, event) => onSelectionChange(photo.uid, event)}
            />
          </FocusRing>
        ))}
      </div>
    );
  }

  // If onPhotoClick is provided, use PhotoCard (div-based, no Link)
  // to avoid double navigation from both Link and onClick handler
  if (onPhotoClick) {
    return (
      <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
        {photos.map((photo) => (
          <FocusRing key={photo.uid} focused={focusedUid === photo.uid}>
            <PhotoCard
              photoUid={photo.uid}
              onClick={() => onPhotoClick(photo)}
            />
          </FocusRing>
        ))}
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
      {photos.map((photo) => (
        <FocusRing key={photo.uid} focused={focusedUid === photo.uid}>
          <PhotoCardLink
            photoUid={photo.uid}
            favorite={photo.favorite}
          />
        </FocusRing>
      ))}
    </div>
  );
}
