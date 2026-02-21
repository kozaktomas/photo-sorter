import { PhotoCardLink, PhotoCard } from './PhotoCard';
import type { Photo } from '../types';

interface PhotoGridProps {
  photos: Photo[];
  photoprismDomain?: string;
  onPhotoClick?: (photo: Photo) => void;
  selectable?: boolean;
  selectedPhotos?: Set<string>;
  onSelectionChange?: (uid: string, selected: boolean) => void;
}

export function PhotoGrid({ photos, photoprismDomain, onPhotoClick, selectable, selectedPhotos, onSelectionChange }: PhotoGridProps) {
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
          <PhotoCard
            key={photo.uid}
            photoUid={photo.uid}
            photoprismDomain={photoprismDomain}
            selectable
            selected={selectedPhotos.has(photo.uid)}
            onSelectionChange={() => onSelectionChange(photo.uid, !selectedPhotos.has(photo.uid))}
          />
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
          <PhotoCard
            key={photo.uid}
            photoUid={photo.uid}
            photoprismDomain={photoprismDomain}
            onClick={() => onPhotoClick(photo)}
          />
        ))}
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
      {photos.map((photo) => (
        <PhotoCardLink
          key={photo.uid}
          photoUid={photo.uid}
          photoprismDomain={photoprismDomain}
          favorite={photo.favorite}
        />
      ))}
    </div>
  );
}
