import { useState, useEffect, useCallback } from 'react';
import { getPhoto, getPhotoFaces } from '../../../api/client';
import type { Photo } from '../../../types';

export type EmbeddingsStatus = 'unknown' | 'missing' | 'available';

export interface PhotoDataState {
  photo: Photo | null;
  loading: boolean;
  error: string | null;
  embeddingsStatus: EmbeddingsStatus;
}

export function usePhotoData(uid: string | undefined) {
  const [photo, setPhoto] = useState<Photo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [embeddingsStatus, setEmbeddingsStatus] = useState<EmbeddingsStatus>('unknown');

  // Load photo and check embeddings status when uid changes
  useEffect(() => {
    if (!uid) return;

    setLoading(true);
    setError(null);
    getPhoto(uid)
      .then(setPhoto)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : 'Unknown error'))
      .finally(() => setLoading(false));

    // Check embedding status
    getPhotoFaces(uid)
      .then(resp => {
        setEmbeddingsStatus(resp.faces_processed ? 'available' : 'missing');
      })
      .catch(() => {
        setEmbeddingsStatus('missing');
      });
  }, [uid]);

  const updateEmbeddingsStatus = (status: EmbeddingsStatus) => {
    setEmbeddingsStatus(status);
  };

  const refresh = useCallback(async () => {
    if (!uid) return;
    try {
      const next = await getPhoto(uid);
      setPhoto(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  }, [uid]);

  return {
    photo,
    loading,
    error,
    embeddingsStatus,
    updateEmbeddingsStatus,
    refresh,
  };
}
