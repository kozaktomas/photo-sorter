import { useState, useEffect } from 'react';
import { getPhotoFaces, getSubjects, computeFaces } from '../../../api/client';
import type { PhotoFacesResponse, Subject } from '../../../types';
import type { EmbeddingsStatus } from './usePhotoData';

export interface FacesDataState {
  facesData: PhotoFacesResponse | null;
  subjects: Subject[];
  loading: boolean;
  error: string | null;
  facesNotComputed: boolean;
  facesLoaded: boolean;
  isComputing: boolean;
  computeError: string | null;
}

interface UseFacesDataReturn extends FacesDataState {
  loadFaces: () => Promise<void>;
  computeFacesForPhoto: () => Promise<void>;
  setFacesData: React.Dispatch<React.SetStateAction<PhotoFacesResponse | null>>;
}

export function useFacesData(
  uid: string | undefined,
  onEmbeddingsStatusChange: (status: EmbeddingsStatus) => void
): UseFacesDataReturn {
  const [facesData, setFacesData] = useState<PhotoFacesResponse | null>(null);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [facesNotComputed, setFacesNotComputed] = useState(false);
  const [facesLoaded, setFacesLoaded] = useState(false);
  const [isComputing, setIsComputing] = useState(false);
  const [computeError, setComputeError] = useState<string | null>(null);

  // Reset all state when photo UID changes
  useEffect(() => {
    setFacesData(null);
    setError(null);
    setFacesNotComputed(false);
    setFacesLoaded(false);
    setComputeError(null);
  }, [uid]);

  const loadFaces = async () => {
    if (!uid) return;

    try {
      setLoading(true);
      setError(null);
      setFacesNotComputed(false);
      const [facesResponse, subjectsResponse] = await Promise.all([
        getPhotoFaces(uid),
        getSubjects({ count: 1000 }),
      ]);
      setFacesData(facesResponse);
      setSubjects(subjectsResponse);
      setFacesLoaded(true);
      onEmbeddingsStatusChange(facesResponse.embeddings_count > 0 ? 'available' : 'missing');
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to load faces';
      if (errorMsg.includes('face data not available') || errorMsg.includes('not available')) {
        setFacesNotComputed(true);
        setFacesLoaded(true);
      } else {
        setError(errorMsg);
      }
    } finally {
      setLoading(false);
    }
  };

  const computeFacesForPhoto = async () => {
    if (!uid) return;

    setIsComputing(true);
    setComputeError(null);
    try {
      const result = await computeFaces(uid);
      if (result.success) {
        setFacesNotComputed(false);
        onEmbeddingsStatusChange('available');
        const [facesResponse, subjectsResponse] = await Promise.all([
          getPhotoFaces(uid),
          getSubjects({ count: 1000 }),
        ]);
        setFacesData(facesResponse);
        setSubjects(subjectsResponse);
        setFacesLoaded(true);
      } else {
        setComputeError(result.error || 'Face computation failed');
      }
    } catch (err) {
      setComputeError(err instanceof Error ? err.message : 'Face computation failed');
    } finally {
      setIsComputing(false);
    }
  };

  return {
    facesData,
    subjects,
    loading,
    error,
    facesNotComputed,
    facesLoaded,
    isComputing,
    computeError,
    loadFaces,
    computeFacesForPhoto,
    setFacesData,
  };
}
