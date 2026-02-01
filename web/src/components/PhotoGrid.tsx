import { PhotoCardLink } from './PhotoCard';
import type { Photo } from '../types';

interface PhotoGridProps {
  photos: Photo[];
  photoprismDomain?: string;
  onPhotoClick?: (photo: Photo) => void;
}

export function PhotoGrid({ photos, photoprismDomain, onPhotoClick }: PhotoGridProps) {
  if (photos.length === 0) {
    return (
      <div className="text-center py-12 text-slate-400">
        No photos found
      </div>
    );
  }

  // If onPhotoClick is provided, we need to intercept the click
  // Otherwise, PhotoCardLink handles navigation
  if (onPhotoClick) {
    return (
      <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
        {photos.map((photo) => (
          <div key={photo.uid} onClick={() => onPhotoClick(photo)}>
            <PhotoCardLink
              photoUid={photo.uid}
              photoprismDomain={photoprismDomain}
              favorite={photo.favorite}
            />
          </div>
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
