import { useState, useCallback } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BookOpen, ArrowLeft, Pencil, Trash2, Check, X } from 'lucide-react';
import { updateBook, deleteBook } from '../../api/client';
import { LoadingState } from '../../components/LoadingState';
import { useBookData } from './hooks/useBookData';
import { SectionsTab } from './SectionsTab';
import { PagesTab } from './PagesTab';
import { PreviewTab } from './PreviewTab';

type Tab = 'sections' | 'pages' | 'preview';

const VALID_TABS: Tab[] = ['sections', 'pages', 'preview'];

function isValidTab(value: string | null): value is Tab {
  return value !== null && VALID_TABS.includes(value as Tab);
}

export function BookEditorPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { t } = useTranslation('pages');
  const { book, setBook, loading, error, refresh, sectionPhotos, loadSectionPhotos } = useBookData(id!);

  const tabParam = searchParams.get('tab');
  const activeTab: Tab = isValidTab(tabParam) ? tabParam : 'pages';
  const pageParam = searchParams.get('page');

  const handleTabChange = useCallback((tab: Tab) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      next.set('tab', tab);
      if (tab !== 'pages' && tab !== 'preview') {
        next.delete('page');
      }
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const handlePageSelect = useCallback((pageId: string | null) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (pageId) {
        next.set('page', pageId);
      } else {
        next.delete('page');
      }
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState('');

  const handleStartEdit = () => {
    if (book) {
      setEditTitle(book.title);
      setEditing(true);
    }
  };

  const handleSaveTitle = async () => {
    if (!book || !editTitle.trim()) return;
    try {
      await updateBook(book.id, { title: editTitle.trim() });
      setEditing(false);
      refresh();
    } catch { /* silent */ }
  };

  const handleDelete = async () => {
    if (!book || !confirm(t('books.deleteConfirm'))) return;
    try {
      await deleteBook(book.id);
      navigate('/books');
    } catch { /* silent */ }
  };

  const tabs: { key: Tab; label: string }[] = [
    { key: 'pages', label: t('books.editor.pagesTab') },
    { key: 'sections', label: t('books.editor.sectionsTab') },
    { key: 'preview', label: t('books.editor.previewTab') },
  ];

  return (
    <div>
      <button
        onClick={() => navigate('/books')}
        className="flex items-center gap-1 text-slate-400 hover:text-white mb-4 transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        {t('books.title')}
      </button>

      <LoadingState isLoading={loading} error={error} isEmpty={!book} emptyTitle="Book not found">
        {book && (
          <>
            <div className="flex items-center justify-between mb-6">
              <div className="flex items-center gap-3">
                <BookOpen className="h-6 w-6 text-rose-400" />
                {editing ? (
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      value={editTitle}
                      onChange={(e) => setEditTitle(e.target.value)}
                      onKeyDown={(e) => e.key === 'Enter' && handleSaveTitle()}
                      className="px-2 py-1 bg-slate-800 border border-slate-600 rounded text-white text-xl font-bold focus:outline-none focus:ring-1 focus:ring-rose-500"
                      autoFocus
                    />
                    <button onClick={handleSaveTitle} className="text-green-400 hover:text-green-300">
                      <Check className="h-5 w-5" />
                    </button>
                    <button onClick={() => setEditing(false)} className="text-slate-400 hover:text-white">
                      <X className="h-5 w-5" />
                    </button>
                  </div>
                ) : (
                  <h1 className="text-2xl font-bold text-white">{book.title}</h1>
                )}
              </div>
              <div className="flex items-center gap-2">
                {!editing && (
                  <button onClick={handleStartEdit} className="text-slate-400 hover:text-white p-1 transition-colors">
                    <Pencil className="h-4 w-4" />
                  </button>
                )}
                <button onClick={handleDelete} className="text-slate-400 hover:text-red-400 p-1 transition-colors">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>

            <div className="flex border-b border-slate-700 mb-6">
              {tabs.map(({ key, label }) => (
                <button
                  key={key}
                  onClick={() => handleTabChange(key)}
                  className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
                    activeTab === key
                      ? 'border-rose-500 text-rose-400'
                      : 'border-transparent text-slate-400 hover:text-white'
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>

            {activeTab === 'sections' && (
              <SectionsTab
                book={book}
                sectionPhotos={sectionPhotos}
                loadSectionPhotos={loadSectionPhotos}
                onRefresh={refresh}
              />
            )}
            {activeTab === 'pages' && (
              <PagesTab
                book={book}
                setBook={setBook}
                sectionPhotos={sectionPhotos}
                loadSectionPhotos={loadSectionPhotos}
                onRefresh={refresh}
                initialPageId={pageParam}
                onPageSelect={handlePageSelect}
              />
            )}
            {activeTab === 'preview' && (
              <PreviewTab book={book} sectionPhotos={sectionPhotos} loadSectionPhotos={loadSectionPhotos} initialPageId={pageParam} />
            )}
          </>
        )}
      </LoadingState>
    </div>
  );
}
