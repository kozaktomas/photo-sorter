import { useState, useCallback } from 'react';
import { addPhotosToAlbum, batchAddLabels, batchEditPhotos, removePhotosFromAlbum, getAlbums, getLabels } from '../api/client';
import { MAX_ALBUMS_FETCH, MAX_LABELS_FETCH } from '../constants';
import type { Album, Label } from '../types';

export interface ActionMessage {
  type: 'success' | 'error';
  text: string;
}

export interface UsePhotoSelectionReturn {
  selectedPhotos: Set<string>;
  toggleSelection: (photoUID: string) => void;
  selectAll: (uids: string[]) => void;
  deselectAll: () => void;
  albums: Album[];
  labels: Label[];
  selectedAlbum: string;
  setSelectedAlbum: (uid: string) => void;
  labelInput: string;
  setLabelInput: (label: string) => void;
  isAddingToAlbum: boolean;
  isAddingLabel: boolean;
  isBatchEditing: boolean;
  isRemovingFromAlbum: boolean;
  actionMessage: ActionMessage | null;
  setActionMessage: (msg: ActionMessage | null) => void;
  handleAddToAlbum: () => Promise<void>;
  handleAddLabel: () => Promise<void>;
  handleBatchEdit: (updates: { favorite?: boolean; private?: boolean }) => Promise<void>;
  handleRemoveFromAlbum: (albumUid: string) => Promise<void>;
}

export function usePhotoSelection(): UsePhotoSelectionReturn {
  const [selectedPhotos, setSelectedPhotos] = useState<Set<string>>(new Set());
  const [albums, setAlbums] = useState<Album[]>([]);
  const [labels, setLabels] = useState<Label[]>([]);
  const [selectedAlbum, setSelectedAlbum] = useState('');
  const [labelInput, setLabelInput] = useState('');
  const [isAddingToAlbum, setIsAddingToAlbum] = useState(false);
  const [isAddingLabel, setIsAddingLabel] = useState(false);
  const [isBatchEditing, setIsBatchEditing] = useState(false);
  const [isRemovingFromAlbum, setIsRemovingFromAlbum] = useState(false);
  const [actionMessage, setActionMessage] = useState<ActionMessage | null>(null);
  const [dataLoaded, setDataLoaded] = useState(false);

  const loadAlbumsAndLabels = useCallback(async () => {
    if (dataLoaded) return;
    try {
      const [albumsData, labelsData] = await Promise.all([
        getAlbums({ count: MAX_ALBUMS_FETCH, order: 'name' }),
        getLabels({ count: MAX_LABELS_FETCH, all: true }),
      ]);
      setAlbums(albumsData);
      setLabels(labelsData);
      setDataLoaded(true);
    } catch (err) {
      console.error('Failed to load albums/labels:', err);
    }
  }, [dataLoaded]);

  const toggleSelection = useCallback((photoUID: string) => {
    setSelectedPhotos(prev => {
      const next = new Set(prev);
      if (next.has(photoUID)) {
        next.delete(photoUID);
      } else {
        next.add(photoUID);
      }
      if (next.size === 1 && !dataLoaded) {
        loadAlbumsAndLabels();
      }
      return next;
    });
  }, [dataLoaded, loadAlbumsAndLabels]);

  const selectAll = useCallback((uids: string[]) => {
    setSelectedPhotos(new Set(uids));
    if (!dataLoaded) {
      loadAlbumsAndLabels();
    }
  }, [dataLoaded, loadAlbumsAndLabels]);

  const deselectAll = useCallback(() => {
    setSelectedPhotos(new Set());
  }, []);

  const handleAddToAlbum = useCallback(async () => {
    if (!selectedAlbum || selectedPhotos.size === 0) return;
    setIsAddingToAlbum(true);
    setActionMessage(null);
    try {
      const result = await addPhotosToAlbum(selectedAlbum, Array.from(selectedPhotos));
      setActionMessage({ type: 'success', text: `Added ${result.added} photos to album` });
      setSelectedPhotos(new Set());
      setSelectedAlbum('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add to album' });
    } finally {
      setIsAddingToAlbum(false);
    }
  }, [selectedAlbum, selectedPhotos]);

  const handleAddLabel = useCallback(async () => {
    if (!labelInput.trim() || selectedPhotos.size === 0) return;
    setIsAddingLabel(true);
    setActionMessage(null);
    try {
      const result = await batchAddLabels(Array.from(selectedPhotos), labelInput.trim());
      if (result.errors && result.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${result.updated} photos, ${result.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Added label to ${result.updated} photos` });
      }
      setSelectedPhotos(new Set());
      setLabelInput('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add label' });
    } finally {
      setIsAddingLabel(false);
    }
  }, [labelInput, selectedPhotos]);

  const handleBatchEdit = useCallback(async (updates: { favorite?: boolean; private?: boolean }) => {
    if (selectedPhotos.size === 0) return;
    setIsBatchEditing(true);
    setActionMessage(null);
    try {
      const result = await batchEditPhotos(Array.from(selectedPhotos), updates);
      if (result.errors && result.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${result.updated} photos, ${result.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Updated ${result.updated} photos` });
      }
      setSelectedPhotos(new Set());
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update photos' });
    } finally {
      setIsBatchEditing(false);
    }
  }, [selectedPhotos]);

  const handleRemoveFromAlbum = useCallback(async (albumUid: string) => {
    if (selectedPhotos.size === 0) return;
    setIsRemovingFromAlbum(true);
    setActionMessage(null);
    try {
      const result = await removePhotosFromAlbum(albumUid, Array.from(selectedPhotos));
      setActionMessage({ type: 'success', text: `Removed ${result.removed} photos from album` });
      setSelectedPhotos(new Set());
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to remove from album' });
    } finally {
      setIsRemovingFromAlbum(false);
    }
  }, [selectedPhotos]);

  return {
    selectedPhotos,
    toggleSelection,
    selectAll,
    deselectAll,
    albums,
    labels,
    selectedAlbum,
    setSelectedAlbum,
    labelInput,
    setLabelInput,
    isAddingToAlbum,
    isAddingLabel,
    isBatchEditing,
    isRemovingFromAlbum,
    actionMessage,
    setActionMessage,
    handleAddToAlbum,
    handleAddLabel,
    handleBatchEdit,
    handleRemoveFromAlbum,
  };
}
