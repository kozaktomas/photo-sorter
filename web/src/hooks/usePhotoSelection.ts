import { useCallback, useEffect, useState } from 'react';
import { addPhotosToAlbum, batchAddLabels, batchEditPhotos, removePhotosFromAlbum, getAlbums, getLabels, getBooks, getBook, addSectionPhotos } from '../api/client';
import { MAX_ALBUMS_FETCH, MAX_LABELS_FETCH } from '../constants';
import type { Album, Label, PhotoBook, BookSection, BookChapter } from '../types';
import { useGridSelection, type SelectionClickEvent } from './useGridSelection';

export interface ActionMessage {
  type: 'success' | 'error';
  text: string;
}

export interface UsePhotoSelectionReturn {
  selectedPhotos: Set<string>;
  anchorUid: string | null;
  toggleSelection: (photoUID: string) => void;
  selectAll: (uids: string[]) => void;
  deselectAll: () => void;
  handleSelectionClick: (
    uid: string,
    orderedUids: string[],
    event: SelectionClickEvent,
  ) => void;
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
  books: PhotoBook[];
  selectedBookId: string;
  setSelectedBookId: (id: string) => Promise<void>;
  bookSections: BookSection[];
  bookChapters: BookChapter[];
  selectedSectionId: string;
  setSelectedSectionId: (id: string) => void;
  isAddingToSection: boolean;
  isLoadingBookSections: boolean;
  handleAddToBookSection: () => Promise<void>;
}

export function usePhotoSelection(): UsePhotoSelectionReturn {
  const grid = useGridSelection();
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
  const [books, setBooks] = useState<PhotoBook[]>([]);
  const [selectedBookId, setSelectedBookIdRaw] = useState('');
  const [bookSections, setBookSections] = useState<BookSection[]>([]);
  const [bookChapters, setBookChapters] = useState<BookChapter[]>([]);
  const [selectedSectionId, setSelectedSectionId] = useState('');
  const [isAddingToSection, setIsAddingToSection] = useState(false);
  const [isLoadingBookSections, setIsLoadingBookSections] = useState(false);

  const loadAlbumsAndLabels = useCallback(async () => {
    if (dataLoaded) return;
    try {
      const [albumsData, labelsData, booksData] = await Promise.all([
        getAlbums({ count: MAX_ALBUMS_FETCH, order: 'name' }),
        getLabels({ count: MAX_LABELS_FETCH, all: true }),
        getBooks(),
      ]);
      setAlbums(albumsData);
      setLabels(labelsData);
      setBooks(booksData);
      setDataLoaded(true);
    } catch (err) {
      console.error('Failed to load albums/labels:', err);
    }
  }, [dataLoaded]);

  // Pre-fetch the bulk-action dropdown data the first time a photo is
  // selected. Used to live inline in toggleSelection/selectAll; lifted to an
  // effect so every selection-changing path (shift-click range, Ctrl+A, etc.)
  // gets the same treatment without each call site having to remember.
  useEffect(() => {
    if (grid.selectedPhotos.size > 0 && !dataLoaded) {
      void loadAlbumsAndLabels();
    }
  }, [grid.selectedPhotos.size, dataLoaded, loadAlbumsAndLabels]);

  const handleAddToAlbum = useCallback(async () => {
    if (!selectedAlbum || grid.selectedPhotos.size === 0) return;
    setIsAddingToAlbum(true);
    setActionMessage(null);
    try {
      const result = await addPhotosToAlbum(selectedAlbum, Array.from(grid.selectedPhotos));
      setActionMessage({ type: 'success', text: `Added ${result.added} photos to album` });
      grid.deselectAll();
      setSelectedAlbum('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add to album' });
    } finally {
      setIsAddingToAlbum(false);
    }
  }, [selectedAlbum, grid]);

  const handleAddLabel = useCallback(async () => {
    if (!labelInput.trim() || grid.selectedPhotos.size === 0) return;
    setIsAddingLabel(true);
    setActionMessage(null);
    try {
      const result = await batchAddLabels(Array.from(grid.selectedPhotos), labelInput.trim());
      if (result.errors && result.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${result.updated} photos, ${result.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Added label to ${result.updated} photos` });
      }
      grid.deselectAll();
      setLabelInput('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add label' });
    } finally {
      setIsAddingLabel(false);
    }
  }, [labelInput, grid]);

  const handleBatchEdit = useCallback(async (updates: { favorite?: boolean; private?: boolean }) => {
    if (grid.selectedPhotos.size === 0) return;
    setIsBatchEditing(true);
    setActionMessage(null);
    try {
      const result = await batchEditPhotos(Array.from(grid.selectedPhotos), updates);
      if (result.errors && result.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${result.updated} photos, ${result.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Updated ${result.updated} photos` });
      }
      grid.deselectAll();
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update photos' });
    } finally {
      setIsBatchEditing(false);
    }
  }, [grid]);

  const setSelectedBookId = useCallback(async (bookId: string) => {
    setSelectedBookIdRaw(bookId);
    setSelectedSectionId('');
    setBookSections([]);
    setBookChapters([]);
    if (!bookId) return;
    setIsLoadingBookSections(true);
    try {
      const detail = await getBook(bookId);
      setBookSections(detail.sections);
      setBookChapters(detail.chapters);
    } catch (err) {
      console.error('Failed to load book sections:', err);
    } finally {
      setIsLoadingBookSections(false);
    }
  }, []);

  const handleAddToBookSection = useCallback(async () => {
    if (!selectedSectionId || grid.selectedPhotos.size === 0) return;
    setIsAddingToSection(true);
    setActionMessage(null);
    try {
      await addSectionPhotos(selectedSectionId, Array.from(grid.selectedPhotos));
      const section = bookSections.find(s => s.id === selectedSectionId);
      setActionMessage({
        type: 'success',
        text: `Added ${grid.selectedPhotos.size} photos to section ${section?.title ?? selectedSectionId}`,
      });
      grid.deselectAll();
      setSelectedBookIdRaw('');
      setSelectedSectionId('');
      setBookSections([]);
      setBookChapters([]);
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add to section' });
    } finally {
      setIsAddingToSection(false);
    }
  }, [selectedSectionId, grid, bookSections]);

  const handleRemoveFromAlbum = useCallback(async (albumUid: string) => {
    if (grid.selectedPhotos.size === 0) return;
    setIsRemovingFromAlbum(true);
    setActionMessage(null);
    try {
      const result = await removePhotosFromAlbum(albumUid, Array.from(grid.selectedPhotos));
      setActionMessage({ type: 'success', text: `Removed ${result.removed} photos from album` });
      grid.deselectAll();
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to remove from album' });
    } finally {
      setIsRemovingFromAlbum(false);
    }
  }, [grid]);

  return {
    selectedPhotos: grid.selectedPhotos,
    anchorUid: grid.anchorUid,
    toggleSelection: grid.toggleSelection,
    selectAll: grid.selectAll,
    deselectAll: grid.deselectAll,
    handleSelectionClick: grid.handleSelectionClick,
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
    books,
    selectedBookId,
    setSelectedBookId,
    bookSections,
    bookChapters,
    selectedSectionId,
    setSelectedSectionId,
    isAddingToSection,
    isLoadingBookSections,
    handleAddToBookSection,
  };
}
