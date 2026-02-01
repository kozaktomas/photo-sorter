import { useState, useEffect, useCallback, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { getAlbumPhotos, getPhotos } from '../../../api/client';
import { ALBUM_PHOTOS_CACHE_KEY, LABEL_PHOTOS_CACHE_KEY } from '../../../constants';

interface PhotosCache {
  id: string;
  photoUids: string[];
}

interface PhotoNavigationState {
  albumUid: string | null;
  labelSlug: string | null;
  currentIndex: number;
  totalPhotos: number;
  hasPrev: boolean;
  hasNext: boolean;
  isLoading: boolean;
  goToPrev: () => void;
  goToNext: () => void;
}

export function usePhotoNavigation(photoUid: string | undefined): PhotoNavigationState {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [photoUids, setPhotoUids] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const albumUid = searchParams.get('album');
  const labelSlug = searchParams.get('label');

  // Load photo list from cache or API
  useEffect(() => {
    if (!albumUid && !labelSlug) {
      setPhotoUids([]);
      return;
    }

    // Handle album navigation
    if (albumUid) {
      const cached = sessionStorage.getItem(ALBUM_PHOTOS_CACHE_KEY);
      if (cached) {
        try {
          const data: PhotosCache = JSON.parse(cached);
          if (data.id === albumUid && data.photoUids.length > 0) {
            setPhotoUids(data.photoUids);
            return;
          }
        } catch {
          // Cache parse failed, will fetch from API
        }
      }

      // Fetch from API
      setIsLoading(true);
      getAlbumPhotos(albumUid, { count: 500 })
        .then((photos) => {
          const uids = photos.map((p) => p.uid);
          setPhotoUids(uids);
          sessionStorage.setItem(
            ALBUM_PHOTOS_CACHE_KEY,
            JSON.stringify({ id: albumUid, photoUids: uids })
          );
        })
        .catch((err) => {
          console.error('Failed to load album photos for navigation:', err);
          setPhotoUids([]);
        })
        .finally(() => {
          setIsLoading(false);
        });
      return;
    }

    // Handle label navigation
    if (labelSlug) {
      const cached = sessionStorage.getItem(LABEL_PHOTOS_CACHE_KEY);
      if (cached) {
        try {
          const data: PhotosCache = JSON.parse(cached);
          if (data.id === labelSlug && data.photoUids.length > 0) {
            setPhotoUids(data.photoUids);
            return;
          }
        } catch {
          // Cache parse failed, will fetch from API
        }
      }

      // Fetch from API using label slug
      setIsLoading(true);
      getPhotos({ label: labelSlug, count: 500 })
        .then((photos) => {
          const uids = photos.map((p) => p.uid);
          setPhotoUids(uids);
          sessionStorage.setItem(
            LABEL_PHOTOS_CACHE_KEY,
            JSON.stringify({ id: labelSlug, photoUids: uids })
          );
        })
        .catch((err) => {
          console.error('Failed to load label photos for navigation:', err);
          setPhotoUids([]);
        })
        .finally(() => {
          setIsLoading(false);
        });
    }
  }, [albumUid, labelSlug]);

  // Compute current index
  const currentIndex = useMemo(() => {
    if (!photoUid || photoUids.length === 0) return -1;
    return photoUids.indexOf(photoUid);
  }, [photoUid, photoUids]);

  const hasPrev = currentIndex > 0;
  const hasNext = currentIndex >= 0 && currentIndex < photoUids.length - 1;

  const goToPrev = useCallback(() => {
    if (!hasPrev || currentIndex <= 0) return;
    const prevUid = photoUids[currentIndex - 1];
    const queryParam = albumUid ? `album=${albumUid}` : `label=${labelSlug}`;
    navigate(`/photos/${prevUid}?${queryParam}`);
  }, [hasPrev, currentIndex, photoUids, albumUid, labelSlug, navigate]);

  const goToNext = useCallback(() => {
    if (!hasNext || currentIndex < 0) return;
    const nextUid = photoUids[currentIndex + 1];
    const queryParam = albumUid ? `album=${albumUid}` : `label=${labelSlug}`;
    navigate(`/photos/${nextUid}?${queryParam}`);
  }, [hasNext, currentIndex, photoUids, albumUid, labelSlug, navigate]);

  return {
    albumUid,
    labelSlug,
    currentIndex,
    totalPhotos: photoUids.length,
    hasPrev,
    hasNext,
    isLoading,
    goToPrev,
    goToNext,
  };
}
