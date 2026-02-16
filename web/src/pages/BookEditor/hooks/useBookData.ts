import { useState, useEffect, useCallback } from 'react';
import { getBook, getSectionPhotos } from '../../../api/client';
import type { BookDetail, SectionPhoto } from '../../../types';

export function useBookData(bookId: string) {
  const [book, setBook] = useState<BookDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [sectionPhotos, setSectionPhotos] = useState<Record<string, SectionPhoto[]>>({});

  const fetchBook = useCallback(async (showLoading: boolean) => {
    try {
      if (showLoading) setLoading(true);
      const data = await getBook(bookId);
      setBook(data);
      setError(null);
    } catch {
      setError('Failed to load book');
    } finally {
      if (showLoading) setLoading(false);
    }
  }, [bookId]);

  const refresh = useCallback(() => fetchBook(false), [fetchBook]);

  useEffect(() => { void fetchBook(true); }, [fetchBook]);

  const loadSectionPhotos = useCallback(async (sectionId: string) => {
    try {
      const photos = await getSectionPhotos(sectionId);
      setSectionPhotos(prev => ({ ...prev, [sectionId]: photos || [] }));
    } catch (e) {
      console.error('Failed to load section photos:', e);
    }
  }, []);

  return { book, setBook, loading, error, refresh, sectionPhotos, loadSectionPhotos };
}
