import { useState, useEffect, useCallback, useMemo } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { getAlbumPhotos, getPhotos } from '../../../api/client';
import { ALBUM_PHOTOS_CACHE_KEY, LABEL_PHOTOS_CACHE_KEY, PHOTOS_CACHE_KEY } from '../../../constants';
import type { Photo } from '../../../types';

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
  const source = searchParams.get('source');

  // Load photo list from cache or API
  useEffect(() => {
    if (!albumUid && !labelSlug && source !== 'photos') {
      setPhotoUids([]);
      return;
    }

    // Handle Photos page navigation (cache only, no API fallback)
    if (source === 'photos') {
      const cached = sessionStorage.getItem(PHOTOS_CACHE_KEY);
      if (cached) {
        try {
          const data = JSON.parse(cached) as { photos: Photo[] };
          if (data.photos && data.photos.length > 0) {
            setPhotoUids(data.photos.map((p) => p.uid));
            return;
          }
        } catch {
          // Cache parse failed
        }
      }
      setPhotoUids([]);
      return;
    }

    // Handle album navigation
    if (albumUid) {
      const cached = sessionStorage.getItem(ALBUM_PHOTOS_CACHE_KEY);
      if (cached) {
        try {
          const data = JSON.parse(cached) as PhotosCache;
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
          const data = JSON.parse(cached) as PhotosCache;
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
  }, [albumUid, labelSlug, source]);

  // Compute current index
  const currentIndex = useMemo(() => {
    if (!photoUid || photoUids.length === 0) return -1;
    return photoUids.indexOf(photoUid);
  }, [photoUid, photoUids]);

  const hasPrev = currentIndex > 0;
  const hasNext = currentIndex >= 0 && currentIndex < photoUids.length - 1;

  const getQueryParam = useCallback(() => {
    if (albumUid) return `album=${albumUid}`;
    if (labelSlug) return `label=${labelSlug}`;
    return 'source=photos';
  }, [albumUid, labelSlug]);

  const goToPrev = useCallback(() => {
    if (!hasPrev || currentIndex <= 0) return;
    const prevUid = photoUids[currentIndex - 1];
    void navigate(`/photos/${prevUid}?${getQueryParam()}`, { replace: true });
  }, [hasPrev, currentIndex, photoUids, getQueryParam, navigate]);

  const goToNext = useCallback(() => {
    if (!hasNext || currentIndex < 0) return;
    const nextUid = photoUids[currentIndex + 1];
    void navigate(`/photos/${nextUid}?${getQueryParam()}`, { replace: true });
  }, [hasNext, currentIndex, photoUids, getQueryParam, navigate]);

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
