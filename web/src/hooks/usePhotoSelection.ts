import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { addPhotosToAlbum, batchAddLabels, batchEditPhotos, removePhotosFromAlbum, getAlbums, getLabels, getBooks, getBook, addSectionPhotos } from '../api/client';
import { MAX_ALBUMS_FETCH, MAX_LABELS_FETCH } from '../constants';
import type { Album, Label, PhotoBook, BookSection, BookChapter } from '../types';
import { useGridSelection, type SelectionClickEvent } from './useGridSelection';
import { useToast } from '../components/Toast';

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
  const { t } = useTranslation('common');
  const toast = useToast();
  const grid = useGridSelection();
  const [albums, setAlbums] = useState<Album[]>([]);
  const [labels, setLabels] = useState<Label[]>([]);
  const [selectedAlbum, setSelectedAlbum] = useState('');
  const [labelInput, setLabelInput] = useState('');
  const [isAddingToAlbum, setIsAddingToAlbum] = useState(false);
  const [isAddingLabel, setIsAddingLabel] = useState(false);
  const [isBatchEditing, setIsBatchEditing] = useState(false);
  const [isRemovingFromAlbum, setIsRemovingFromAlbum] = useState(false);
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
    try {
      const result = await addPhotosToAlbum(selectedAlbum, Array.from(grid.selectedPhotos));
      toast.success(t('toasts.bulk.addedToAlbum', { count: result.added }));
      grid.deselectAll();
      setSelectedAlbum('');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('toasts.bulk.addToAlbumFailed'));
    } finally {
      setIsAddingToAlbum(false);
    }
  }, [selectedAlbum, grid, toast, t]);

  const handleAddLabel = useCallback(async () => {
    if (!labelInput.trim() || grid.selectedPhotos.size === 0) return;
    setIsAddingLabel(true);
    try {
      const result = await batchAddLabels(Array.from(grid.selectedPhotos), labelInput.trim());
      if (result.errors && result.errors.length > 0) {
        toast.error(t('toasts.bulk.addLabelPartial', {
          count: result.updated,
          errors: result.errors.length,
        }));
      } else {
        toast.success(t('toasts.bulk.addLabelDone', { count: result.updated }));
      }
      grid.deselectAll();
      setLabelInput('');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('toasts.bulk.addLabelFailed'));
    } finally {
      setIsAddingLabel(false);
    }
  }, [labelInput, grid, toast, t]);

  const handleBatchEdit = useCallback(async (updates: { favorite?: boolean; private?: boolean }) => {
    if (grid.selectedPhotos.size === 0) return;
    setIsBatchEditing(true);
    try {
      const result = await batchEditPhotos(Array.from(grid.selectedPhotos), updates);
      if (result.errors && result.errors.length > 0) {
        toast.error(t('toasts.bulk.favoritePartial', {
          count: result.updated,
          errors: result.errors.length,
        }));
      } else {
        toast.success(t('toasts.bulk.favoriteDone', { count: result.updated }));
      }
      grid.deselectAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('toasts.bulk.updateFailed'));
    } finally {
      setIsBatchEditing(false);
    }
  }, [grid, toast, t]);

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
    const count = grid.selectedPhotos.size;
    try {
      await addSectionPhotos(selectedSectionId, Array.from(grid.selectedPhotos));
      const section = bookSections.find(s => s.id === selectedSectionId);
      toast.success(t('toasts.bulk.addToSectionDone', {
        count,
        section: section?.title ?? selectedSectionId,
      }));
      grid.deselectAll();
      setSelectedBookIdRaw('');
      setSelectedSectionId('');
      setBookSections([]);
      setBookChapters([]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('toasts.bulk.addToSectionFailed'));
    } finally {
      setIsAddingToSection(false);
    }
  }, [selectedSectionId, grid, bookSections, toast, t]);

  const handleRemoveFromAlbum = useCallback(async (albumUid: string) => {
    if (grid.selectedPhotos.size === 0) return;
    setIsRemovingFromAlbum(true);
    try {
      const result = await removePhotosFromAlbum(albumUid, Array.from(grid.selectedPhotos));
      toast.success(t('toasts.bulk.removedFromAlbum', { count: result.removed }));
      grid.deselectAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('toasts.bulk.removeFromAlbumFailed'));
    } finally {
      setIsRemovingFromAlbum(false);
    }
  }, [grid, toast, t]);

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
