import { useState, useCallback, useRef, useEffect } from 'react';
import { getPhotos } from '../../../api/client';
import { PHOTOS_PER_PAGE, PHOTOS_CACHE_KEY } from '../../../constants';
import type { Photo } from '../../../types';

interface PhotosCache {
  photos: Photo[];
  offset: number;
  hasMore: boolean;
  scrollY: number;
  filterKey: string;
}

interface FilterParams {
  search: string;
  selectedYear: number | '';
  selectedLabel: string;
  selectedAlbum: string;
  sortBy: string;
  filterKey: string;
}

export interface UsePhotosPaginationReturn {
  photos: Photo[];
  isLoading: boolean;
  isLoadingMore: boolean;
  hasMore: boolean;
  offset: number;
  loadPhotos: (resetOffset?: boolean) => Promise<void>;
  handleLoadMore: () => void;
  saveCache: () => void;
  restoredFromCache: boolean;
}

export function usePhotosPagination(filters: FilterParams): UsePhotosPaginationReturn {
  const restoredFromCache = useRef(false);
  const pendingScrollRestore = useRef<number | null>(null);

  // Try to restore from cache on initial mount
  const getInitialState = () => {
    try {
      const cached = sessionStorage.getItem(PHOTOS_CACHE_KEY);
      if (cached) {
        const cache: PhotosCache = JSON.parse(cached);
        if (cache.filterKey === filters.filterKey && cache.photos.length > 0) {
          restoredFromCache.current = true;
          pendingScrollRestore.current = cache.scrollY;
          return cache;
        }
      }
    } catch {
      // Ignore cache errors
    }
    return null;
  };

  const cachedState = useRef(getInitialState());

  const [photos, setPhotos] = useState<Photo[]>(cachedState.current?.photos || []);
  const [isLoading, setIsLoading] = useState(!cachedState.current);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(cachedState.current?.hasMore ?? true);
  const [offset, setOffset] = useState(cachedState.current?.offset || 0);

  // Restore scroll position after photos are rendered
  useEffect(() => {
    if (pendingScrollRestore.current !== null && photos.length > 0 && !isLoading) {
      requestAnimationFrame(() => {
        window.scrollTo(0, pendingScrollRestore.current!);
        pendingScrollRestore.current = null;
      });
    }
  }, [photos, isLoading]);

  const loadPhotos = useCallback(async (resetOffset = true) => {
    const currentOffset = resetOffset ? 0 : offset;
    if (resetOffset) {
      setIsLoading(true);
      setPhotos([]);
    } else {
      setIsLoadingMore(true);
    }

    try {
      const data = await getPhotos({
        count: PHOTOS_PER_PAGE,
        offset: currentOffset,
        order: filters.sortBy,
        q: filters.search || undefined,
        year: filters.selectedYear || undefined,
        label: filters.selectedLabel || undefined,
        album: filters.selectedAlbum || undefined,
      });

      if (resetOffset) {
        setPhotos(data);
        setOffset(PHOTOS_PER_PAGE);
      } else {
        setPhotos(prev => [...prev, ...data]);
        setOffset(currentOffset + PHOTOS_PER_PAGE);
      }
      setHasMore(data.length === PHOTOS_PER_PAGE);
    } catch (err) {
      console.error('Failed to load photos:', err);
    } finally {
      setIsLoading(false);
      setIsLoadingMore(false);
    }
  }, [filters.search, filters.selectedYear, filters.selectedLabel, filters.selectedAlbum, filters.sortBy, offset]);

  const handleLoadMore = useCallback(() => {
    if (!isLoadingMore && hasMore) {
      loadPhotos(false);
    }
  }, [isLoadingMore, hasMore, loadPhotos]);

  const saveCache = useCallback(() => {
    const cache: PhotosCache = {
      photos,
      offset,
      hasMore,
      scrollY: window.scrollY,
      filterKey: filters.filterKey,
    };
    sessionStorage.setItem(PHOTOS_CACHE_KEY, JSON.stringify(cache));
  }, [photos, offset, hasMore, filters.filterKey]);

  return {
    photos,
    isLoading,
    isLoadingMore,
    hasMore,
    offset,
    loadPhotos,
    handleLoadMore,
    saveCache,
    restoredFromCache: restoredFromCache.current,
  };
}
