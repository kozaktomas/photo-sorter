import { useState, useCallback, useEffect } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BookOpen, ArrowLeft, Pencil, Trash2, Check, X, Download, BarChart3 } from 'lucide-react';
import { updateBook, deleteBook, exportBookPDF, preflightBook } from '../../api/client';
import type { PreflightResponse } from '../../types';
import { LoadingState } from '../../components/LoadingState';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useBookData } from './hooks/useBookData';
import { BookStatsPanel } from './BookStatsPanel';
import { SectionsTab } from './SectionsTab';
import { PagesTab } from './PagesTab';
import { PreviewTab } from './PreviewTab';
import { DuplicatesTab } from './DuplicatesTab';
import { TextsTab } from './TextsTab';
import { PreflightModal } from './PreflightModal';
import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp';

type Tab = 'sections' | 'pages' | 'preview' | 'duplicates' | 'texts';

const VALID_TABS: Tab[] = ['sections', 'pages', 'preview', 'duplicates', 'texts'];

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
  const sectionParam = searchParams.get('section');

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

  const [showShortcutsHelp, setShowShortcutsHelp] = useState(false);

  // Global keyboard shortcuts: 1-5 for tabs, ? for help
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (document.activeElement?.tagName ?? '').toLowerCase();
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
      if (document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable) return;

      // Tab switching: 1-5
      const tabIndex = parseInt(e.key) - 1;
      if (tabIndex >= 0 && tabIndex < VALID_TABS.length) {
        e.preventDefault();
        handleTabChange(VALID_TABS[tabIndex]);
        return;
      }

      // ? for help (Shift+/ on US layout, or just ?)
      if (e.key === '?') {
        e.preventDefault();
        setShowShortcutsHelp(true);
        return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [handleTabChange]);

  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState('');
  const [exporting, setExporting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [preflightData, setPreflightData] = useState<PreflightResponse | null>(null);
  const [preflightLoading, setPreflightLoading] = useState(false);
  const [showPreflight, setShowPreflight] = useState(false);
  const [showStats, setShowStats] = useState(() => {
    try { return localStorage.getItem(`book-stats-${id}`) === 'true'; } catch { return false; }
  });

  const toggleStats = useCallback(() => {
    setShowStats(prev => {
      const next = !prev;
      try { localStorage.setItem(`book-stats-${id}`, String(next)); } catch { /* ignore */ }
      return next;
    });
  }, [id]);

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
      void refresh();
    } catch (e) { console.error('Failed to save title:', e); }
  };

  const handleDelete = () => {
    if (!book) return;
    setShowDeleteConfirm(true);
  };

  const confirmDelete = async () => {
    if (!book) return;
    setShowDeleteConfirm(false);
    try {
      await deleteBook(book.id);
      void navigate('/books');
    } catch (e) { console.error('Failed to delete book:', e); }
  };

  const handleExportPDF = async () => {
    if (!book || exporting) return;
    setPreflightLoading(true);
    setShowPreflight(true);
    try {
      const result = await preflightBook(book.id);
      setPreflightData(result);
      if (result.ok) {
        // No issues — export directly
        setShowPreflight(false);
        setPreflightData(null);
        await doExport();
      }
    } catch (e) {
      console.error('Preflight failed:', e);
      setShowPreflight(false);
    }
    setPreflightLoading(false);
  };

  const doExport = async () => {
    if (!book || exporting) return;
    setShowPreflight(false);
    setPreflightData(null);
    setExporting(true);
    try {
      await exportBookPDF(book.id);
    } catch (e) { console.error('Failed to export PDF:', e); }
    setExporting(false);
  };

  const handleGoToPage = (pageNumber: number) => {
    if (!book) return;
    // Find the page by its display number (1-based sort order)
    const page = book.pages?.[pageNumber - 1];
    if (page) {
      setShowPreflight(false);
      setPreflightData(null);
      handleTabChange('pages');
      handlePageSelect(page.id);
    }
  };

  const handleNavigateToPage = useCallback((pageId: string) => {
    handleTabChange('pages');
    handlePageSelect(pageId);
  }, [handleTabChange, handlePageSelect]);

  const handleNavigateToSection = useCallback((sectionId: string) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      next.set('tab', 'sections');
      next.set('section', sectionId);
      next.delete('page');
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const tabs: { key: Tab; label: string }[] = [
    { key: 'pages', label: t('books.editor.pagesTab') },
    { key: 'sections', label: t('books.editor.sectionsTab') },
    { key: 'texts', label: t('books.editor.textsTab') },
    { key: 'preview', label: t('books.editor.previewTab') },
    { key: 'duplicates', label: t('books.editor.duplicatesTab') },
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
                      onKeyDown={(e) => { if (e.key === 'Enter') void handleSaveTitle(); }}
                      className="px-2 py-1 bg-slate-800 border border-slate-600 rounded text-white text-xl font-bold focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
                      autoFocus
                    />
                    <button onClick={() => void handleSaveTitle()} className="text-green-400 hover:text-green-300">
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
                  <>
                    <button
                      onClick={toggleStats}
                      className={`p-1 transition-colors ${showStats ? 'text-rose-400' : 'text-slate-400 hover:text-white'}`}
                      title={t('books.editor.statsToggle')}
                    >
                      <BarChart3 className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => void handleExportPDF()}
                      disabled={exporting || !book.pages?.length}
                      className="text-slate-400 hover:text-white p-1 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                      title={exporting ? t('books.editor.exporting') : t('books.editor.exportPDF')}
                    >
                      <Download className={`h-4 w-4 ${exporting ? 'animate-pulse' : ''}`} />
                    </button>
                    <button onClick={handleStartEdit} className="text-slate-400 hover:text-white p-1 transition-colors">
                      <Pencil className="h-4 w-4" />
                    </button>
                  </>
                )}
                <button onClick={handleDelete} className="text-slate-400 hover:text-red-400 p-1 transition-colors">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>

            <div className="flex items-center border-b border-slate-700 mb-6">
              {tabs.map(({ key, label }, idx) => (
                <button
                  key={key}
                  onClick={() => handleTabChange(key)}
                  className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
                    activeTab === key
                      ? 'border-rose-500 text-rose-400'
                      : 'border-transparent text-slate-400 hover:text-white'
                  }`}
                  title={`${idx + 1}`}
                >
                  {label}
                </button>
              ))}
              <button
                onClick={() => setShowShortcutsHelp(true)}
                className="ml-auto px-2 py-1 text-xs text-slate-500 hover:text-slate-300 transition-colors"
                title={t('books.editor.keyboardShortcuts')}
              >
                {t('books.editor.pressForHelp')}
              </button>
            </div>

            {showStats && <BookStatsPanel book={book} sectionPhotos={sectionPhotos} />}

            {activeTab === 'sections' && (
              <SectionsTab
                book={book}
                sectionPhotos={sectionPhotos}
                loadSectionPhotos={loadSectionPhotos}
                onRefresh={refresh}
                initialSectionId={sectionParam}
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
            {activeTab === 'texts' && (
              <TextsTab
                book={book}
                sectionPhotos={sectionPhotos}
                loadSectionPhotos={loadSectionPhotos}
                onRefresh={refresh}
                onNavigateToPage={handleNavigateToPage}
                onNavigateToSection={handleNavigateToSection}
              />
            )}
            {activeTab === 'duplicates' && (
              <DuplicatesTab
                book={book}
                sectionPhotos={sectionPhotos}
                loadSectionPhotos={loadSectionPhotos}
                onRefresh={refresh}
              />
            )}
          </>
        )}
      </LoadingState>

      <ConfirmDialog
        open={showDeleteConfirm}
        title={t('books.deleteBook')}
        message={t('books.deleteConfirm')}
        confirmLabel={t('books.deleteBook')}
        variant="danger"
        onConfirm={confirmDelete}
        onCancel={() => setShowDeleteConfirm(false)}
      />

      <KeyboardShortcutsHelp
        open={showShortcutsHelp}
        onClose={() => setShowShortcutsHelp(false)}
      />

      {showPreflight && (
        <PreflightModal
          data={preflightData ?? { ok: true, errors: [], warnings: [], info: [], summary: { total_pages: 0, total_photos: 0, filled_slots: 0, total_slots: 0 } }}
          loading={preflightLoading}
          onExport={() => void doExport()}
          onClose={() => { setShowPreflight(false); setPreflightData(null); }}
          onGoToPage={handleGoToPage}
        />
      )}
    </div>
  );
}
