import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BookOpen, Plus, Trash2, Layers, FileText, Image } from 'lucide-react';
import { getBooks, createBook, deleteBook } from '../../api/client';
import { LoadingState } from '../../components/LoadingState';
import type { PhotoBook } from '../../types';

export function BooksPage() {
  const { t } = useTranslation('pages');
  const [books, setBooks] = useState<PhotoBook[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newTitle, setNewTitle] = useState('');
  const [creating, setCreating] = useState(false);

  const loadBooks = async () => {
    try {
      setLoading(true);
      const data = await getBooks();
      setBooks(data || []);
      setError(null);
    } catch {
      setError('Failed to load books');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadBooks(); }, []);

  const handleCreate = async () => {
    if (!newTitle.trim()) return;
    setCreating(true);
    try {
      await createBook(newTitle.trim());
      setNewTitle('');
      await loadBooks();
    } catch {
      setError('Failed to create book');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(t('books.deleteConfirm'))) return;
    try {
      await deleteBook(id);
      await loadBooks();
    } catch {
      setError('Failed to delete book');
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-white flex items-center gap-2">
            <BookOpen className="h-6 w-6 text-rose-400" />
            {t('books.title')}
          </h1>
          <p className="text-slate-400 mt-1">{t('books.subtitle')}</p>
        </div>
      </div>

      <div className="flex gap-2 mb-6">
        <input
          type="text"
          value={newTitle}
          onChange={(e) => setNewTitle(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') void handleCreate(); }}
          placeholder={t('books.titlePlaceholder')}
          className="flex-1 px-3 py-2 bg-slate-800 border border-slate-700 rounded-md text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-rose-500"
        />
        <button
          onClick={handleCreate}
          disabled={creating || !newTitle.trim()}
          className="flex items-center gap-2 px-4 py-2 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white rounded-md transition-colors"
        >
          <Plus className="h-4 w-4" />
          {t('books.createBook')}
        </button>
      </div>

      <LoadingState isLoading={loading} error={error} isEmpty={books.length === 0} emptyTitle={t('books.noBooks')}>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {books.map((book) => (
            <div key={book.id} className="bg-slate-800 border border-slate-700 rounded-lg overflow-hidden hover:border-rose-500/50 transition-colors">
              <Link to={`/books/${book.id}`} className="block p-4">
                <h3 className="text-lg font-semibold text-white truncate">{book.title}</h3>
                {book.description && (
                  <p className="text-slate-400 text-sm mt-1 line-clamp-2">{book.description}</p>
                )}
                <div className="flex gap-4 mt-3 text-sm text-slate-500">
                  <span className="flex items-center gap-1">
                    <Layers className="h-3.5 w-3.5" />
                    {book.section_count} {t('books.sections')}
                  </span>
                  <span className="flex items-center gap-1">
                    <FileText className="h-3.5 w-3.5" />
                    {book.page_count} {t('books.pages')}
                  </span>
                  <span className="flex items-center gap-1">
                    <Image className="h-3.5 w-3.5" />
                    {book.photo_count} {t('books.photos')}
                  </span>
                </div>
              </Link>
              <div className="px-4 pb-3 flex justify-end">
                <button
                  onClick={(e) => { e.preventDefault(); void handleDelete(book.id); }}
                  className="text-slate-500 hover:text-red-400 transition-colors p-1"
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      </LoadingState>
    </div>
  );
}
