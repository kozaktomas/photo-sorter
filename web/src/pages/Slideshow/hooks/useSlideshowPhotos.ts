import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getAlbumPhotos, getAlbum, getPhotos, getLabel } from '../../../api/client';
import type { Photo } from '../../../types';

interface SlideshowPhotosResult {
  photos: Photo[];
  title: string;
  isLoading: boolean;
  error: string | null;
  sourceType: 'album' | 'label' | null;
  sourceId: string | null;
}

export function useSlideshowPhotos(): SlideshowPhotosResult {
  const [searchParams] = useSearchParams();
  const albumUid = searchParams.get('album');
  const labelUid = searchParams.get('label');

  const [photos, setPhotos] = useState<Photo[]>([]);
  const [title, setTitle] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const sourceType = albumUid ? 'album' : labelUid ? 'label' : null;
  const sourceId = albumUid || labelUid;

  useEffect(() => {
    if (!sourceType || !sourceId) {
      setError('No album or label specified');
      setIsLoading(false);
      return;
    }

    async function load() {
      setIsLoading(true);
      setError(null);
      try {
        if (sourceType === 'album') {
          const [photosData, albumData] = await Promise.all([
            getAlbumPhotos(sourceId!, { count: 500 }),
            getAlbum(sourceId!),
          ]);
          setPhotos(photosData);
          setTitle(albumData.title);
        } else {
          // Fetch label first to get slug for photo search
          const labelData = await getLabel(sourceId!);
          const photosData = await getPhotos({ label: labelData.slug, count: 500 });
          setPhotos(photosData);
          setTitle(labelData.name);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load photos');
      } finally {
        setIsLoading(false);
      }
    }

    void load();
  }, [sourceType, sourceId]);

  return { photos, title, isLoading, error, sourceType, sourceId };
}
