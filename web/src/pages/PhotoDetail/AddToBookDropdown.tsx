import { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { BookOpen, ChevronLeft, ChevronRight, Check, Loader2, X } from 'lucide-react';
import { Button } from '../../components/Button';
import { getBooks, getBook, addSectionPhotos } from '../../api/client';
import type { PhotoBook, BookDetail } from '../../types';

interface Props {
  photoUid: string;
  onAdded?: () => void;
}

export function AddToBookDropdown({ photoUid, onAdded }: Props) {
  const { t } = useTranslation('pages');
  const [isOpen, setIsOpen] = useState(false);
  const [books, setBooks] = useState<PhotoBook[] | null>(null);
  const [selectedBook, setSelectedBook] = useState<BookDetail | null>(null);
  const [loadingBooks, setLoadingBooks] = useState(false);
  const [loadingBook, setLoadingBook] = useState(false);
  const [adding, setAdding] = useState(false);
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => {
    setIsOpen(false);
    setSelectedBook(null);
    setFeedback(null);
  }, []);

  // Click-outside dismissal
  useEffect(() => {
    if (!isOpen) return;
    const handleMouseDown = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        close();
      }
    };
    document.addEventListener('mousedown', handleMouseDown);
    return () => document.removeEventListener('mousedown', handleMouseDown);
  }, [isOpen, close]);

  // Escape key dismissal
  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, close]);

  const handleToggle = async () => {
    if (isOpen) {
      close();
      return;
    }
    setIsOpen(true);
    setSelectedBook(null);
    setFeedback(null);
    if (!books) {
      setLoadingBooks(true);
      try {
        const result = await getBooks();
        setBooks(result);
      } catch {
        setFeedback({ type: 'error', text: t('photoDetail.failedToLoadBooks') });
      } finally {
        setLoadingBooks(false);
      }
    }
  };

  const handleSelectBook = async (bookId: string) => {
    setLoadingBook(true);
    setFeedback(null);
    try {
      const detail = await getBook(bookId);
      setSelectedBook(detail);
    } catch {
      setFeedback({ type: 'error', text: t('photoDetail.failedToLoadSections') });
    } finally {
      setLoadingBook(false);
    }
  };

  const handleSelectSection = async (sectionId: string, sectionTitle: string) => {
    setAdding(true);
    setFeedback(null);
    try {
      await addSectionPhotos(sectionId, [photoUid]);
      setFeedback({ type: 'success', text: t('photoDetail.addedToSection', { section: sectionTitle }) });
      onAdded?.();
      setTimeout(() => close(), 1500);
    } catch {
      setFeedback({ type: 'error', text: t('photoDetail.failedToAdd') });
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="relative" ref={dropdownRef}>
      <Button variant="ghost" size="sm" onClick={handleToggle} title={t('photoDetail.addToBookTitle')}>
        <BookOpen className="h-4 w-4 mr-1" />
        {t('photoDetail.addToBook')}
      </Button>

      {isOpen && (
        <div className="absolute right-0 top-full mt-1 w-72 bg-slate-800 border border-slate-700 rounded-lg shadow-xl z-50 overflow-hidden">
          {/* Feedback state */}
          {feedback && (
            <div className={`flex items-center gap-2 px-4 py-3 ${feedback.type === 'success' ? 'text-green-400' : 'text-red-400'}`}>
              {feedback.type === 'success' ? <Check className="h-4 w-4 shrink-0" /> : <X className="h-4 w-4 shrink-0" />}
              <span className="text-sm">{feedback.text}</span>
            </div>
          )}

          {/* Loading books */}
          {loadingBooks && !feedback && (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="h-5 w-5 animate-spin text-slate-400" />
            </div>
          )}

          {/* Book list */}
          {!loadingBooks && !selectedBook && !feedback && books && (
            <>
              <div className="px-4 py-2 border-b border-slate-700">
                <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">{t('photoDetail.addToBookTitle')}</span>
              </div>
              {books.length === 0 ? (
                <div className="px-4 py-4 text-sm text-slate-500 text-center">{t('photoDetail.noBooks')}</div>
              ) : (
                <div className="max-h-64 overflow-y-auto">
                  {books.map((book) => (
                    <button
                      key={book.id}
                      onClick={() => handleSelectBook(book.id)}
                      className="w-full flex items-center justify-between px-4 py-2.5 hover:bg-slate-700/50 transition-colors text-left"
                    >
                      <div className="min-w-0">
                        <div className="text-sm text-white truncate">{book.title}</div>
                        <div className="text-xs text-slate-500">{book.section_count} sections</div>
                      </div>
                      <ChevronRight className="h-4 w-4 text-slate-500 shrink-0 ml-2" />
                    </button>
                  ))}
                </div>
              )}
            </>
          )}

          {/* Loading sections */}
          {loadingBook && !feedback && (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="h-5 w-5 animate-spin text-slate-400" />
            </div>
          )}

          {/* Section list */}
          {!loadingBook && selectedBook && !feedback && (
            <>
              <div className="px-4 py-2 border-b border-slate-700 flex items-center gap-2">
                <button onClick={() => setSelectedBook(null)} className="text-slate-400 hover:text-white transition-colors">
                  <ChevronLeft className="h-4 w-4" />
                </button>
                <span className="text-xs font-medium text-slate-400 uppercase tracking-wider truncate">{selectedBook.title}</span>
              </div>
              {selectedBook.sections.length === 0 ? (
                <div className="px-4 py-4 text-sm text-slate-500 text-center">{t('photoDetail.noSections')}</div>
              ) : (
                <div className="max-h-64 overflow-y-auto">
                  {selectedBook.sections.map((section) => (
                    <button
                      key={section.id}
                      onClick={() => handleSelectSection(section.id, section.title)}
                      disabled={adding}
                      className="w-full flex items-center justify-between px-4 py-2.5 hover:bg-slate-700/50 transition-colors text-left disabled:opacity-50"
                    >
                      <div className="min-w-0">
                        <div className="text-sm text-white truncate">{section.title}</div>
                        <div className="text-xs text-slate-500">{section.photo_count} photos</div>
                      </div>
                      {adding ? (
                        <Loader2 className="h-4 w-4 animate-spin text-slate-400 shrink-0 ml-2" />
                      ) : null}
                    </button>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
