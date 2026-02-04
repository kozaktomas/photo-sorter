import { useState, useEffect } from 'react';
import { getPhoto, getPhotoFaces, getConfig } from '../../../api/client';
import type { Photo, Config } from '../../../types';

export type EmbeddingsStatus = 'unknown' | 'missing' | 'available';

export interface PhotoDataState {
  photo: Photo | null;
  loading: boolean;
  error: string | null;
  config: Config | null;
  embeddingsStatus: EmbeddingsStatus;
}

export function usePhotoData(uid: string | undefined) {
  const [photo, setPhoto] = useState<Photo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [config, setConfig] = useState<Config | null>(null);
  const [embeddingsStatus, setEmbeddingsStatus] = useState<EmbeddingsStatus>('unknown');

  // Load config on mount
  useEffect(() => {
    getConfig().then(setConfig).catch(console.error);
  }, []);

  // Load photo and check embeddings status when uid changes
  useEffect(() => {
    if (!uid) return;

    setLoading(true);
    setError(null);
    getPhoto(uid)
      .then(setPhoto)
      .catch(err => setError(err.message))
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

  return {
    photo,
    loading,
    error,
    config,
    embeddingsStatus,
    updateEmbeddingsStatus,
  };
}
