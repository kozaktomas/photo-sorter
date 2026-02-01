import { useState, useEffect } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import type { Photo, PhotoFace, MatchAction } from '../../types';

const actionColors: Record<MatchAction, string> = {
  create_marker: 'border-red-500',
  assign_person: 'border-yellow-500',
  already_done: 'border-green-500',
  unassign_person: 'border-orange-500',
};

interface PhotoDisplayProps {
  photo: Photo;
  faces: PhotoFace[] | undefined;
  selectedFaceIndex: number | null;
  onFaceSelect: (index: number) => void;
  hasPrev?: boolean;
  hasNext?: boolean;
  onPrev?: () => void;
  onNext?: () => void;
  currentIndex?: number;
  totalPhotos?: number;
}

export function PhotoDisplay({ photo, faces, selectedFaceIndex, onFaceSelect, hasPrev, hasNext, onPrev, onNext, currentIndex, totalPhotos }: PhotoDisplayProps) {
  const [imageLoaded, setImageLoaded] = useState(false);

  // Reset imageLoaded when photo changes so face boxes don't persist
  useEffect(() => {
    setImageLoaded(false);
  }, [photo.uid]);

  const showNavigation = hasPrev || hasNext;
  const showCounter = totalPhotos !== undefined && totalPhotos > 0 && currentIndex !== undefined && currentIndex >= 0;

  return (
    <div className="flex-1 flex items-center justify-center p-6 bg-slate-950/50 overflow-hidden relative group">
      {/* Left navigation arrow */}
      {showNavigation && (
        <button
          onClick={onPrev}
          disabled={!hasPrev}
          className={`absolute left-4 top-1/2 -translate-y-1/2 z-10 p-2 rounded-full bg-black/50 backdrop-blur-sm transition-all ${
            hasPrev
              ? 'text-white hover:bg-black/70 cursor-pointer opacity-0 group-hover:opacity-100'
              : 'text-slate-600 cursor-not-allowed opacity-0 group-hover:opacity-50'
          }`}
          aria-label="Previous photo"
        >
          <ChevronLeft className="h-8 w-8" />
        </button>
      )}

      {/* Right navigation arrow */}
      {showNavigation && (
        <button
          onClick={onNext}
          disabled={!hasNext}
          className={`absolute right-4 top-1/2 -translate-y-1/2 z-10 p-2 rounded-full bg-black/50 backdrop-blur-sm transition-all ${
            hasNext
              ? 'text-white hover:bg-black/70 cursor-pointer opacity-0 group-hover:opacity-100'
              : 'text-slate-600 cursor-not-allowed opacity-0 group-hover:opacity-50'
          }`}
          aria-label="Next photo"
        >
          <ChevronRight className="h-8 w-8" />
        </button>
      )}

      {/* Position counter */}
      {showCounter && (
        <div className="absolute bottom-4 left-1/2 -translate-x-1/2 z-10 px-3 py-1.5 rounded-full bg-black/50 backdrop-blur-sm text-white text-sm font-medium opacity-0 group-hover:opacity-100 transition-all">
          {currentIndex + 1} / {totalPhotos}
        </div>
      )}

      <div className="relative inline-block">
        <img
          src={getThumbnailUrl(photo.uid, 'fit_1920')}
          alt={photo.title || photo.file_name}
          className="max-h-[calc(100vh-12rem)] max-w-full h-auto rounded-lg"
          onLoad={() => setImageLoaded(true)}
        />

        {/* Face bounding boxes */}
        {imageLoaded && faces?.map((face, index) => (
          <div
            key={face.face_index}
            className={`absolute border-2 cursor-pointer transition-all ${
              selectedFaceIndex === index ? 'border-blue-400 shadow-lg shadow-blue-500/30' : actionColors[face.action]
            } ${selectedFaceIndex === index ? 'ring-2 ring-blue-400 ring-offset-2 ring-offset-transparent' : ''}`}
            style={{
              left: `${face.bbox_rel[0] * 100}%`,
              top: `${face.bbox_rel[1] * 100}%`,
              width: `${face.bbox_rel[2] * 100}%`,
              height: `${face.bbox_rel[3] * 100}%`,
            }}
            onClick={() => onFaceSelect(index)}
          >
            {/* Face number badge */}
            <div className={`absolute -top-5 -left-1 text-xs px-1.5 py-0.5 rounded ${
              selectedFaceIndex === index ? 'bg-blue-500 text-white' : 'bg-slate-800 text-slate-300'
            }`}>
              #{index + 1}
            </div>

            {/* Name badge for assigned faces */}
            {face.marker_name && (
              <div className="absolute -bottom-6 left-0 right-0 text-xs bg-black/70 text-white px-1 py-0.5 rounded truncate text-center">
                {face.marker_name}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
