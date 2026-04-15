import { useState, useMemo, useCallback, useRef, useEffect, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { SpellCheck, ArrowLeftRight, Loader2, Check, DollarSign, ChevronDown, ChevronUp, ExternalLink, X, BarChart3, Search, Image as ImageIcon, Type, Download, AlertTriangle } from 'lucide-react';
import { checkTextAndSave, rewriteText, checkTextConsistency, updateSectionPhoto, assignTextSlot, getTextCheckStatus } from '../../api/client';
import type { TextCheckStatusEntry, TextSuggestion } from '../../api/client';
import { StatsGrid } from '../../components/StatsGrid';
import { CheckSuggestionsList } from './CheckSuggestionsList';
import type { BookDetail, SectionPhoto } from '../../types';

type TargetLength = 'much_shorter' | 'shorter' | 'longer' | 'much_longer';

type CheckStatus = 'unchecked' | 'clean' | 'has_errors' | 'stale';

interface EntryChapter {
  title: string;
  color: string;
}

interface PageRef {
  pageId: string;
  pageNumber: number;
  slotIndex: number;
}

interface TextEntry {
  id: string;
  text: string;
  source: 'section_photo' | 'text_slot';
  sectionId: string;
  sectionTitle: string;
  chapter: EntryChapter | null;
  pageRefs: PageRef[];
  // Section photo fields
  photoUid?: string;
  photoNote?: string;
  // Text slot fields
  pageId?: string;
  pageNumber?: number;
  slotIndex?: number;
}

interface CheckResult {
  corrected_text: string;
  readability_score: number;
  changes: string[];
  suggestions: TextSuggestion[];
  cost_czk: number;
  cached: boolean;
}

interface RewriteResult {
  rewritten_text: string;
  cost_czk: number;
  cached: boolean;
}

function omit<T>(obj: Record<string, T>, key: string): Record<string, T> {
  const { [key]: _, ...rest } = obj;
  return rest;
}

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
  onNavigateToPage: (pageId: string) => void;
  onNavigateToSection: (sectionId: string) => void;
}

export function TextsTab({ book, sectionPhotos, loadSectionPhotos, onRefresh, onNavigateToPage, onNavigateToSection }: Props) {
  const { t } = useTranslation('pages');

  // Load all section photos on mount
  useState(() => {
    for (const section of book.sections) {
      if (!sectionPhotos[section.id]) {
        loadSectionPhotos(section.id);
      }
    }
  });

  // Persisted check status from the database
  const [checkStatuses, setCheckStatuses] = useState<Record<string, CheckStatus>>({});
  const [persistedResults, setPersistedResults] = useState<Record<string, TextCheckStatusEntry>>({});

  // Load persisted check status on mount and after book changes
  useEffect(() => {
    void (async () => {
      try {
        const status = await getTextCheckStatus(book.id);
        setPersistedResults(status);
      } catch { /* silent */ }
    })();
  }, [book.id]);

  // Expanded row state
  const [expandedId, setExpandedId] = useState<string | null>(null);

  // AI operation state per row
  const [checking, setChecking] = useState<string | null>(null);
  const [rewriting, setRewriting] = useState<string | null>(null);
  const [checkResults, setCheckResults] = useState<Record<string, CheckResult>>({});
  const [rewriteResults, setRewriteResults] = useState<Record<string, RewriteResult>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [targetLengths, setTargetLengths] = useState<Record<string, TargetLength>>({});

  // Batch check state
  const [isBatchChecking, setIsBatchChecking] = useState(false);
  const [batchProgress, setBatchProgress] = useState(0);
  const [batchTotal, setBatchTotal] = useState(0);
  const [batchTotalCost, setBatchTotalCost] = useState<number | null>(null);
  const batchCancelledRef = useRef(false);

  // Search state
  const [searchQuery, setSearchQuery] = useState('');

  // Consistency check state
  const [isConsistencyChecking, setIsConsistencyChecking] = useState(false);
  const [consistencyResult, setConsistencyResult] = useState<{
    consistency_score: number;
    tone: string;
    issues: { text_id: string; problem: string; suggestion: string }[];
    cost_czk: number;
    cached: boolean;
  } | null>(null);

  // Helper to compute the persisted status key for a text entry
  const getPersistedKey = useCallback((entry: TextEntry): string => {
    if (entry.source === 'section_photo' && entry.sectionId && entry.photoUid) {
      return `section_photo:${entry.sectionId}:${entry.photoUid}:description`;
    }
    if (entry.source === 'text_slot' && entry.pageId != null && entry.slotIndex != null) {
      return `page_slot:${entry.pageId}:${entry.slotIndex}:text_content`;
    }
    return '';
  }, []);

  // Helper to get source info for check-and-save API
  const getSourceInfo = useCallback((entry: TextEntry): { sourceType: string; sourceId: string; field: string } | null => {
    if (entry.source === 'section_photo' && entry.sectionId && entry.photoUid) {
      return { sourceType: 'section_photo', sourceId: `${entry.sectionId}:${entry.photoUid}`, field: 'description' };
    }
    if (entry.source === 'text_slot' && entry.pageId != null && entry.slotIndex != null) {
      return { sourceType: 'page_slot', sourceId: `${entry.pageId}:${entry.slotIndex}`, field: 'text_content' };
    }
    return null;
  }, []);

  // Build text entries from book data
  const textEntries = useMemo<TextEntry[]>(() => {
    // Global page numbering by sort_order
    const sortedPages = [...book.pages].sort((a, b) => a.sort_order - b.sort_order);
    const globalPageByPageId = new Map<string, number>();
    const pageRefsByPhotoUid = new Map<string, PageRef[]>();
    for (let i = 0; i < sortedPages.length; i++) {
      const page = sortedPages[i];
      const num = i + 1;
      globalPageByPageId.set(page.id, num);
      const seenOnPage = new Map<string, number>();
      for (const slot of page.slots) {
        if (!slot.photo_uid || seenOnPage.has(slot.photo_uid)) continue;
        seenOnPage.set(slot.photo_uid, slot.slot_index);
        const ref: PageRef = { pageId: page.id, pageNumber: num, slotIndex: slot.slot_index };
        const existing = pageRefsByPhotoUid.get(slot.photo_uid);
        if (existing) existing.push(ref);
        else pageRefsByPhotoUid.set(slot.photo_uid, [ref]);
      }
    }

    // Chapter lookup by section id
    const chapterById = new Map<string, EntryChapter>();
    for (const ch of book.chapters) {
      chapterById.set(ch.id, { title: ch.title, color: ch.color });
    }
    const chapterBySectionId = new Map<string, EntryChapter | null>();
    const sectionById = new Map<string, typeof book.sections[number]>();
    for (const section of book.sections) {
      sectionById.set(section.id, section);
      chapterBySectionId.set(section.id, section.chapter_id ? chapterById.get(section.chapter_id) ?? null : null);
    }

    const entries: TextEntry[] = [];

    // Photo descriptions from sections
    for (const section of book.sections) {
      const photos = sectionPhotos[section.id] || [];
      const chapter = chapterBySectionId.get(section.id) ?? null;
      for (const photo of photos) {
        if (photo.description?.trim()) {
          entries.push({
            id: `sp-${section.id}-${photo.photo_uid}`,
            text: photo.description,
            source: 'section_photo',
            sectionId: section.id,
            sectionTitle: section.title,
            chapter,
            pageRefs: pageRefsByPhotoUid.get(photo.photo_uid) ?? [],
            photoUid: photo.photo_uid,
            photoNote: photo.note,
          });
        }
      }
    }

    // Text slots from pages
    for (const page of book.pages) {
      for (const slot of page.slots) {
        if (slot.text_content?.trim()) {
          const section = sectionById.get(page.section_id);
          const chapter = chapterBySectionId.get(page.section_id) ?? null;
          const globalPage = globalPageByPageId.get(page.id) ?? 0;
          entries.push({
            id: `ts-${page.id}-${slot.slot_index}`,
            text: slot.text_content,
            source: 'text_slot',
            sectionId: page.section_id,
            sectionTitle: section?.title ?? '?',
            chapter,
            pageRefs: globalPage > 0 ? [{ pageId: page.id, pageNumber: globalPage, slotIndex: slot.slot_index }] : [],
            pageId: page.id,
            pageNumber: globalPage,
            slotIndex: slot.slot_index,
          });
        }
      }
    }

    // Sort by first page number, then by slot index; unplaced items go last
    entries.sort((a, b) => {
      const pa = a.pageRefs[0]?.pageNumber ?? Number.POSITIVE_INFINITY;
      const pb = b.pageRefs[0]?.pageNumber ?? Number.POSITIVE_INFINITY;
      if (pa !== pb) return pa - pb;
      const sa = a.pageRefs[0]?.slotIndex ?? Number.POSITIVE_INFINITY;
      const sb = b.pageRefs[0]?.slotIndex ?? Number.POSITIVE_INFINITY;
      return sa - sb;
    });

    return entries;
  }, [book, sectionPhotos]);

  // Merge persisted results into check statuses + hydrate suggestions
  useEffect(() => {
    if (Object.keys(persistedResults).length === 0) return;
    setCheckStatuses(prev => {
      const next = { ...prev };
      for (const entry of textEntries) {
        // Skip entries that already have a session-based status
        if (next[entry.id]) continue;
        const persistedKey = getPersistedKey(entry);
        const persisted = persistedResults[persistedKey];
        if (persisted) {
          if (persisted.is_stale) {
            next[entry.id] = 'stale';
          } else {
            next[entry.id] = persisted.status;
          }
        }
      }
      return next;
    });
    setCheckResults(prev => {
      const next = { ...prev };
      for (const entry of textEntries) {
        if (next[entry.id]) continue;
        const persistedKey = getPersistedKey(entry);
        const persisted = persistedResults[persistedKey];
        if (!persisted || persisted.is_stale) continue;
        const hasSuggestions = (persisted.suggestions?.length ?? 0) > 0;
        const hasChanges = (persisted.changes?.length ?? 0) > 0;
        if (!hasSuggestions && !hasChanges) continue;
        next[entry.id] = {
          corrected_text: persisted.corrected_text ?? '',
          readability_score: persisted.readability_score ?? 0,
          changes: persisted.changes ?? [],
          suggestions: persisted.suggestions ?? [],
          cost_czk: 0,
          cached: true,
        };
      }
      return next;
    });
  }, [persistedResults, textEntries, getPersistedKey]);

  // Stats
  const totalTexts = textEntries.length;
  const checkedCount = Object.values(checkStatuses).filter(s => s === 'clean').length;
  const errorCount = Object.values(checkStatuses).filter(s => s === 'has_errors').length;
  const staleCount = Object.values(checkStatuses).filter(s => s === 'stale').length;
  const majorSuggestionCount = Object.values(checkResults).filter(
    r => r.suggestions?.some(s => s.severity === 'major')
  ).length;
  const totalReadingTime = useMemo(() => {
    let total = 0;
    for (const e of textEntries) {
      const wc = e.text.trim().split(/\s+/).filter(Boolean).length;
      if (wc >= 10) total += Math.ceil((wc / 200) * 2) / 2;
      else if (wc > 0) total += 0.5;
    }
    return Math.ceil(total * 2) / 2;
  }, [textEntries]);

  // Filtered entries based on search
  const filteredEntries = useMemo(() => {
    if (!searchQuery.trim()) return textEntries;
    const q = searchQuery.toLowerCase();
    return textEntries.filter(entry => {
      const haystack = [
        entry.text,
        entry.sectionTitle,
        entry.chapter?.title ?? '',
        entry.pageRefs.map(r => r.pageNumber).join(' '),
      ].join(' ').toLowerCase();
      return haystack.includes(q);
    });
  }, [textEntries, searchQuery]);

  // Highlight matching substring in text
  const highlightMatch = useCallback((text: string, maxLen?: number) => {
    const display = maxLen && text.length > maxLen ? text.slice(0, maxLen) + '...' : text;
    if (!searchQuery.trim()) return <>{display}</>;
    const q = searchQuery.trim();
    const idx = display.toLowerCase().indexOf(q.toLowerCase());
    if (idx === -1) return <>{display}</>;
    return (
      <>
        {display.slice(0, idx)}
        <mark className="bg-yellow-500/30 text-inherit rounded-sm px-0.5">{display.slice(idx, idx + q.length)}</mark>
        {display.slice(idx + q.length)}
      </>
    );
  }, [searchQuery]);

  const handleCheck = useCallback(async (entry: TextEntry) => {
    setChecking(entry.id);
    setErrors(prev => ({ ...prev, [entry.id]: '' }));
    setRewriteResults(prev => omit(prev, entry.id));
    setExpandedId(entry.id);
    try {
      const source = getSourceInfo(entry);
      let result: CheckResult;
      if (source) {
        result = await checkTextAndSave(source.sourceType, source.sourceId, source.field, entry.text);
      } else {
        // Fallback (shouldn't happen) — import is already removed, use check-and-save with empty source
        result = await checkTextAndSave('', '', '', entry.text);
      }
      setCheckResults(prev => ({ ...prev, [entry.id]: result }));
      setCheckStatuses(prev => ({
        ...prev,
        [entry.id]: result.changes.length === 0 ? 'clean' : 'has_errors',
      }));
    } catch (err) {
      setErrors(prev => ({ ...prev, [entry.id]: err instanceof Error ? err.message : 'Check failed' }));
    } finally {
      setChecking(null);
    }
  }, [getSourceInfo]);

  const handleRewrite = useCallback(async (entry: TextEntry) => {
    const length = targetLengths[entry.id] || 'shorter';
    setRewriting(entry.id);
    setErrors(prev => ({ ...prev, [entry.id]: '' }));
    setCheckResults(prev => omit(prev, entry.id));
    setExpandedId(entry.id);
    try {
      const result = await rewriteText(entry.text, length);
      setRewriteResults(prev => ({ ...prev, [entry.id]: result }));
    } catch (err) {
      setErrors(prev => ({ ...prev, [entry.id]: err instanceof Error ? err.message : 'Rewrite failed' }));
    } finally {
      setRewriting(null);
    }
  }, [targetLengths]);

  const acceptCheck = useCallback(async (entry: TextEntry) => {
    const result = checkResults[entry.id];
    if (!result) return;
    try {
      if (entry.source === 'section_photo' && entry.sectionId && entry.photoUid) {
        await updateSectionPhoto(entry.sectionId, entry.photoUid, result.corrected_text, entry.photoNote || '');
      } else if (entry.source === 'text_slot' && entry.pageId != null && entry.slotIndex != null) {
        await assignTextSlot(entry.pageId, entry.slotIndex, result.corrected_text);
      }
      setCheckResults(prev => omit(prev, entry.id));
      setCheckStatuses(prev => ({ ...prev, [entry.id]: 'clean' }));
      onRefresh();
    } catch (err) {
      setErrors(prev => ({ ...prev, [entry.id]: err instanceof Error ? err.message : 'Save failed' }));
    }
  }, [checkResults, onRefresh]);

  const acceptRewrite = useCallback(async (entry: TextEntry) => {
    const result = rewriteResults[entry.id];
    if (!result) return;
    try {
      if (entry.source === 'section_photo' && entry.sectionId && entry.photoUid) {
        await updateSectionPhoto(entry.sectionId, entry.photoUid, result.rewritten_text, entry.photoNote || '');
      } else if (entry.source === 'text_slot' && entry.pageId != null && entry.slotIndex != null) {
        await assignTextSlot(entry.pageId, entry.slotIndex, result.rewritten_text);
      }
      setRewriteResults(prev => omit(prev, entry.id));
      onRefresh();
    } catch (err) {
      setErrors(prev => ({ ...prev, [entry.id]: err instanceof Error ? err.message : 'Save failed' }));
    }
  }, [rewriteResults, onRefresh]);

  const handleBatchCheck = useCallback(async () => {
    // Skip texts that are already checked and not stale
    const needsCheck = textEntries.filter(e => {
      const status = checkStatuses[e.id];
      return !status || status === 'unchecked' || status === 'stale';
    });
    if (needsCheck.length === 0) return;

    batchCancelledRef.current = false;
    setIsBatchChecking(true);
    setBatchProgress(0);
    setBatchTotal(needsCheck.length);
    setBatchTotalCost(null);

    let totalCost = 0;

    for (let i = 0; i < needsCheck.length; i++) {
      if (batchCancelledRef.current) break;

      const entry = needsCheck[i];
      setBatchProgress(i + 1);
      setChecking(entry.id);
      setErrors(prev => ({ ...prev, [entry.id]: '' }));

      try {
        const source = getSourceInfo(entry);
        let result: CheckResult;
        if (source) {
          result = await checkTextAndSave(source.sourceType, source.sourceId, source.field, entry.text);
        } else {
          result = await checkTextAndSave('', '', '', entry.text);
        }
        setCheckResults(prev => ({ ...prev, [entry.id]: result }));
        setCheckStatuses(prev => ({
          ...prev,
          [entry.id]: result.changes.length === 0 ? 'clean' : 'has_errors',
        }));
        totalCost += result.cost_czk;
      } catch (err) {
        setErrors(prev => ({ ...prev, [entry.id]: err instanceof Error ? err.message : 'Check failed' }));
      } finally {
        setChecking(null);
      }
    }

    setBatchTotalCost(totalCost);
    setIsBatchChecking(false);
  }, [textEntries, checkStatuses, getSourceInfo]);

  const handleCancelBatch = useCallback(() => {
    batchCancelledRef.current = true;
  }, []);

  const handleDownloadTexts = useCallback(() => {
    const payload = {
      book: { id: book.id, title: book.title },
      exported_at: new Date().toISOString(),
      count: textEntries.length,
      texts: textEntries.map(e => ({
        id: e.id,
        type: e.source === 'section_photo' ? 'photo_caption' : 'text_slot',
        chapter: e.chapter?.title ?? null,
        section: e.sectionTitle,
        pages: e.pageRefs.map(r => r.pageNumber),
        slot: e.source === 'text_slot' && e.slotIndex != null ? e.slotIndex + 1 : null,
        text: e.text,
      })),
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    const slug = book.title.trim().toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '') || 'book';
    a.download = `${slug}-texts.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }, [book.id, book.title, textEntries]);

  const handleConsistencyCheck = useCallback(async () => {
    if (textEntries.length < 2) return;
    setIsConsistencyChecking(true);
    setConsistencyResult(null);
    try {
      const texts = textEntries.map(e => ({
        id: e.id,
        source: e.source === 'section_photo'
          ? `${e.sectionTitle || '?'} — fotka`
          : `Stránka ${e.pageNumber}, slot ${(e.slotIndex ?? 0) + 1}`,
        content: e.text,
      }));
      const result = await checkTextConsistency(texts);
      setConsistencyResult(result);
    } catch (err) {
      setErrors(prev => ({ ...prev, __consistency: err instanceof Error ? err.message : 'Consistency check failed' }));
    } finally {
      setIsConsistencyChecking(false);
    }
  }, [textEntries]);

  const getStatusIndicator = (entryId: string) => {
    const status = checkStatuses[entryId] || 'unchecked';
    switch (status) {
      case 'clean':
        return <span className="inline-flex items-center gap-1 text-xs text-emerald-400"><span className="w-2 h-2 rounded-full bg-emerald-400" />{t('books.editor.clean')}</span>;
      case 'has_errors':
        return <span className="inline-flex items-center gap-1 text-xs text-red-400"><span className="w-2 h-2 rounded-full bg-red-400" />{t('books.editor.hasErrors')}</span>;
      case 'stale':
        return <span className="inline-flex items-center gap-1 text-xs text-amber-400"><span className="w-2 h-2 rounded-full bg-amber-400" />{t('books.editor.staleCheck')}</span>;
      default:
        return <span className="inline-flex items-center gap-1 text-xs text-slate-500"><span className="w-2 h-2 rounded-full bg-slate-500" />{t('books.editor.unchecked')}</span>;
    }
  };

  const getSourceLabel = (entry: TextEntry): ReactNode => {
    const isPhoto = entry.source === 'section_photo';
    const typeLabel = isPhoto ? t('books.editor.textTypePhoto') : t('books.editor.textTypeSlot');
    const TypeIcon = isPhoto ? ImageIcon : Type;
    const pageLinks = entry.pageRefs.length === 0 ? (
      <span className="text-slate-400">{t('books.editor.textLocationUnplaced')}</span>
    ) : (
      <span className="inline-flex items-center flex-wrap gap-x-1">
        <span className="text-slate-400">{t('books.editor.pageLabel')}</span>
        {entry.pageRefs.map((ref, i) => (
          <span key={ref.pageId} className="inline-flex items-center">
            {i > 0 && <span className="text-slate-500 mr-1">,</span>}
            <button
              type="button"
              onClick={e => { e.stopPropagation(); onNavigateToPage(ref.pageId); }}
              className="text-indigo-400 hover:text-indigo-300 hover:underline transition-colors"
              title={t('books.editor.goToPage')}
            >
              {ref.pageNumber}
            </button>
          </span>
        ))}
        {!isPhoto && entry.slotIndex != null && (
          <span className="text-slate-400">, {t('books.editor.slotLabel', { slot: entry.slotIndex + 1 })}</span>
        )}
      </span>
    );
    return (
      <span className="inline-flex items-center flex-wrap gap-x-1.5 gap-y-0.5">
        <span className="inline-flex items-center gap-1 text-slate-400">
          <TypeIcon className="h-3 w-3" />
          {typeLabel}
        </span>
        <span className="text-slate-600">·</span>
        {entry.chapter && (
          <>
            <span className="inline-flex items-center gap-1">
              {entry.chapter.color && (
                <span
                  className="inline-block w-2 h-2 rounded-full flex-shrink-0"
                  style={{ backgroundColor: entry.chapter.color }}
                />
              )}
              <span className="text-slate-400">{entry.chapter.title}</span>
            </span>
            <span className="text-slate-600">›</span>
          </>
        )}
        <span className="text-slate-400">{entry.sectionTitle}</span>
        <span className="text-slate-600">·</span>
        {pageLinks}
      </span>
    );
  };

  const lengthOptions: { value: TargetLength; labelKey: string }[] = [
    { value: 'much_shorter', labelKey: 'books.editor.muchShorter' },
    { value: 'shorter', labelKey: 'books.editor.shorter' },
    { value: 'longer', labelKey: 'books.editor.longer' },
    { value: 'much_longer', labelKey: 'books.editor.muchLonger' },
  ];

  const allChecked = textEntries.length > 0 && textEntries.every(e => {
    const s = checkStatuses[e.id];
    return s === 'clean' || s === 'has_errors';
  });
  const isAnyLoading = checking !== null || rewriting !== null || isConsistencyChecking;

  return (
    <div className="space-y-6">
      {/* Stats */}
      <StatsGrid
        columns={6}
        items={[
          { value: totalTexts, label: t('books.editor.totalTexts'), color: 'white' },
          { value: checkedCount, label: t('books.editor.checkedTexts'), color: 'green' },
          { value: errorCount, label: t('books.editor.textsWithErrors'), color: 'red' },
          { value: staleCount, label: t('books.editor.staleCheck'), color: staleCount > 0 ? 'yellow' : 'white' },
          { value: majorSuggestionCount, label: t('books.editor.majorSuggestions'), color: majorSuggestionCount > 0 ? 'red' : 'white' },
          { value: `~${totalReadingTime} min`, label: t('books.editor.totalReadingTime'), color: 'white' },
        ]}
      />

      {/* Batch check */}
      {textEntries.length > 0 && (
        <div className="flex items-center gap-3 flex-wrap">
          {isBatchChecking ? (
            <>
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <Loader2 className="h-4 w-4 animate-spin text-indigo-400 flex-shrink-0" />
                <span className="text-sm text-slate-300">
                  {t('books.editor.checkingProgress', { current: batchProgress, total: batchTotal })}
                </span>
                <div className="flex-1 h-1.5 bg-slate-700 rounded-full overflow-hidden">
                  <div
                    className="h-full bg-indigo-500 rounded-full transition-all duration-300"
                    style={{ width: `${(batchProgress / batchTotal) * 100}%` }}
                  />
                </div>
              </div>
              <button
                onClick={handleCancelBatch}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-sm font-medium rounded bg-slate-700 hover:bg-slate-600 text-slate-300 transition-colors"
              >
                <X className="h-3.5 w-3.5" />
                {t('books.editor.cancelCheck')}
              </button>
            </>
          ) : (
            <>
              <button
                onClick={() => void handleBatchCheck()}
                disabled={allChecked || isAnyLoading || isConsistencyChecking}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
              >
                <SpellCheck className="h-4 w-4" />
                <DollarSign className="h-3.5 w-3.5 -ml-1 opacity-60" />
                {t('books.editor.checkAll')}
              </button>
              <button
                onClick={() => void handleConsistencyCheck()}
                disabled={textEntries.length < 2 || isAnyLoading || isBatchChecking || isConsistencyChecking}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded bg-violet-600 hover:bg-violet-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
              >
                {isConsistencyChecking ? <Loader2 className="h-4 w-4 animate-spin" /> : <BarChart3 className="h-4 w-4" />}
                <DollarSign className="h-3.5 w-3.5 -ml-1 opacity-60" />
                {t('books.editor.consistencyCheck')}
              </button>
              <button
                onClick={handleDownloadTexts}
                disabled={textEntries.length === 0}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded bg-slate-700 hover:bg-slate-600 disabled:opacity-40 disabled:cursor-not-allowed text-slate-200 transition-colors"
                title={t('books.editor.downloadTextsTitle')}
              >
                <Download className="h-4 w-4" />
                {t('books.editor.downloadTexts')}
              </button>
              {batchTotalCost !== null && (
                <span className="text-sm text-slate-400">
                  {t('books.editor.batchComplete')} — {t('books.editor.totalCost', {
                    amount: batchTotalCost.toLocaleString('cs-CZ', { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
                  })}
                </span>
              )}
            </>
          )}
        </div>
      )}

      {/* Consistency result panel */}
      {consistencyResult && (
        <div className="rounded-lg border border-violet-500/30 bg-violet-950/20 p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-slate-300">{t('books.editor.consistencyScore')}:</span>
                <span className={`text-lg font-bold ${
                  consistencyResult.consistency_score > 80 ? 'text-emerald-400' :
                  consistencyResult.consistency_score >= 50 ? 'text-yellow-400' :
                  'text-red-400'
                }`}>
                  {consistencyResult.consistency_score}%
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-slate-300">{t('books.editor.tone')}:</span>
                <span className="text-sm text-slate-200">{consistencyResult.tone}</span>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-slate-500">
                {t('books.editor.cost', { amount: consistencyResult.cost_czk.toLocaleString('cs-CZ', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) })}
                {consistencyResult.cached && <span className="ml-1 text-emerald-500">({t('books.editor.cachedResult')})</span>}
              </span>
              <button
                onClick={() => setConsistencyResult(null)}
                className="text-slate-500 hover:text-white transition-colors"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>

          {consistencyResult.issues.length === 0 ? (
            <div className="flex items-center gap-2 text-sm text-emerald-400">
              <Check className="h-4 w-4" />
              {t('books.editor.noIssues')}
            </div>
          ) : (
            <div className="space-y-2">
              <p className="text-sm font-medium text-slate-300">{t('books.editor.issues')} ({consistencyResult.issues.length})</p>
              {consistencyResult.issues.map((issue, i) => {
                const entry = textEntries.find(e => e.id === issue.text_id);
                return (
                  <div key={i} className="bg-slate-800/60 rounded p-3 space-y-1.5">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <p className="text-xs text-slate-400 truncate">
                          {entry ? (entry.text.length > 80 ? entry.text.slice(0, 80) + '...' : entry.text) : issue.text_id}
                        </p>
                        {entry && (
                          <div className="text-xs text-slate-500 mt-0.5">{getSourceLabel(entry)}</div>
                        )}
                      </div>
                      {entry && (
                        <button
                          onClick={() => {
                            if (entry.source === 'text_slot' && entry.pageId) onNavigateToPage(entry.pageId);
                            else if (entry.source === 'section_photo' && entry.sectionId) onNavigateToSection(entry.sectionId);
                          }}
                          className="text-xs text-violet-400 hover:text-violet-300 flex-shrink-0 transition-colors"
                        >
                          {t('books.editor.goToText')}
                        </button>
                      )}
                    </div>
                    <p className="text-xs text-red-300">{issue.problem}</p>
                    <p className="text-xs text-slate-300">
                      <span className="text-slate-500">{t('books.editor.suggestion')}:</span> {issue.suggestion}
                    </p>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* Consistency error */}
      {errors.__consistency && (
        <p className="text-xs text-red-400">{errors.__consistency}</p>
      )}

      {/* Search */}
      {textEntries.length > 0 && (
        <div className="space-y-1">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
            <input
              type="text"
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder={t('books.editor.searchTexts')}
              className="w-full pl-9 pr-9 py-2 text-sm bg-slate-800 border border-slate-700 rounded-lg text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-white transition-colors"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
          {searchQuery.trim() && (
            <p className="text-xs text-slate-500">
              {t('books.editor.searchResults', { count: filteredEntries.length, total: textEntries.length })}
            </p>
          )}
        </div>
      )}

      {/* Text list */}
      {textEntries.length === 0 ? (
        <p className="text-sm text-slate-500 text-center py-8">{t('books.editor.totalTexts')}: 0</p>
      ) : (
        <div className="space-y-2">
          {filteredEntries.map(entry => {
            const isExpanded = expandedId === entry.id;
            const checkResult = checkResults[entry.id];
            const rewriteResult = rewriteResults[entry.id];
            const error = errors[entry.id];
            const isChecking = checking === entry.id;
            const isRewriting = rewriting === entry.id;
            const targetLength = targetLengths[entry.id] || 'shorter';

            return (
              <div key={entry.id} className="bg-slate-800 rounded-lg overflow-hidden">
                {/* Row */}
                <div
                  className="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-slate-750"
                  onClick={() => setExpandedId(isExpanded ? null : entry.id)}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-slate-200 truncate">
                      {highlightMatch(entry.text, 100)}
                    </p>
                    <div className="text-xs text-slate-500 mt-0.5 flex items-center flex-wrap gap-x-1.5">
                      {getSourceLabel(entry)}
                      {(() => {
                        const wc = entry.text.trim().split(/\s+/).filter(Boolean).length;
                        if (wc === 0) return null;
                        const rt = wc < 10 ? null : Math.ceil((wc / 200) * 2) / 2;
                        return (
                          <>
                            <span className="text-slate-600">·</span>
                            <span>{rt === null ? t('books.editor.readingTimeShort') : t('books.editor.readingTime', { time: rt })}</span>
                          </>
                        );
                      })()}
                    </div>
                  </div>
                  <div className="flex items-center gap-3 flex-shrink-0">
                    {getStatusIndicator(entry.id)}
                    {(() => {
                      const suggestions = checkResult?.suggestions ?? [];
                      if (suggestions.length === 0) return null;
                      const hasMajor = suggestions.some(s => s.severity === 'major');
                      return (
                        <span
                          className={`inline-flex items-center gap-0.5 text-xs ${hasMajor ? 'text-red-400' : 'text-amber-400'}`}
                          title={t('books.editor.readabilitySuggestionsCount', { count: suggestions.length })}
                        >
                          <AlertTriangle className="h-3.5 w-3.5" />
                          {suggestions.length}
                        </span>
                      );
                    })()}

                    {/* Actions */}
                    <button
                      onClick={e => { e.stopPropagation(); void handleCheck(entry); }}
                      disabled={isAnyLoading}
                      className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
                      title={t('books.editor.textCheck')}
                    >
                      {isChecking ? <Loader2 className="h-3 w-3 animate-spin" /> : <><SpellCheck className="h-3 w-3" /><DollarSign className="h-2.5 w-2.5 -ml-0.5 opacity-60" /></>}
                    </button>

                    <div className="inline-flex items-center gap-1" onClick={e => e.stopPropagation()}>
                      <select
                        value={targetLength}
                        onChange={e => setTargetLengths(prev => ({ ...prev, [entry.id]: e.target.value as TargetLength }))}
                        disabled={isAnyLoading}
                        className="px-1.5 py-1 text-xs bg-slate-900 border border-slate-600 rounded text-white focus:outline-none"
                      >
                        {lengthOptions.map(opt => (
                          <option key={opt.value} value={opt.value}>{t(opt.labelKey)}</option>
                        ))}
                      </select>
                      <button
                        onClick={() => void handleRewrite(entry)}
                        disabled={isAnyLoading}
                        className="inline-flex items-center gap-1 px-2 py-1 text-xs font-medium rounded bg-amber-600 hover:bg-amber-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
                        title={t('books.editor.adjustLength')}
                      >
                        {isRewriting ? <Loader2 className="h-3 w-3 animate-spin" /> : <><ArrowLeftRight className="h-3 w-3" /><DollarSign className="h-2.5 w-2.5 -ml-0.5 opacity-60" /></>}
                      </button>
                    </div>

                    {/* Navigate */}
                    {entry.source === 'text_slot' && entry.pageId && (
                      <button
                        onClick={e => { e.stopPropagation(); onNavigateToPage(entry.pageId!); }}
                        className="inline-flex items-center gap-1 px-2 py-1 text-xs text-slate-400 hover:text-white transition-colors"
                        title={t('books.editor.goToPage')}
                      >
                        <ExternalLink className="h-3 w-3" />
                      </button>
                    )}
                    {entry.source === 'section_photo' && entry.sectionId && (
                      <button
                        onClick={e => { e.stopPropagation(); onNavigateToSection(entry.sectionId); }}
                        className="inline-flex items-center gap-1 px-2 py-1 text-xs text-slate-400 hover:text-white transition-colors"
                        title={t('books.editor.goToSection')}
                      >
                        <ExternalLink className="h-3 w-3" />
                      </button>
                    )}

                    {isExpanded ? <ChevronUp className="h-4 w-4 text-slate-500" /> : <ChevronDown className="h-4 w-4 text-slate-500" />}
                  </div>
                </div>

                {/* Expanded panel */}
                {isExpanded && (
                  <div className="px-4 pb-4 space-y-3">
                    {/* Full text */}
                    <div className="bg-slate-900/50 rounded p-3">
                      <p className="text-sm text-slate-300 whitespace-pre-wrap">{entry.text}</p>
                    </div>

                    {/* Check result */}
                    {checkResult && (
                      <div className="rounded border border-indigo-500/30 bg-indigo-950/30 p-3 space-y-2">
                        {checkResult.changes.length === 0 ? (
                          <div className="flex items-center gap-2 text-sm text-emerald-400">
                            <Check className="h-4 w-4" />
                            {t('books.editor.textOk')}
                          </div>
                        ) : (
                          <>
                            <div className="flex items-center justify-between">
                              <span className="text-xs font-medium text-indigo-300">
                                {t('books.editor.readability')}: {checkResult.readability_score}%
                              </span>
                            </div>
                            <div>
                              <p className="text-xs font-medium text-slate-400 mb-1">{t('books.editor.changes')}:</p>
                              <ul className="text-xs text-slate-300 space-y-0.5 list-disc list-inside">
                                {checkResult.changes.map((change, i) => (
                                  <li key={i}>{change}</li>
                                ))}
                              </ul>
                            </div>
                            <div>
                              <p className="text-xs font-medium text-slate-400 mb-1">{t('books.editor.correctedText')}:</p>
                              <p className="text-xs text-slate-300 bg-slate-900/50 p-2 rounded whitespace-pre-wrap">{checkResult.corrected_text}</p>
                            </div>
                          </>
                        )}
                        <CheckSuggestionsList suggestions={checkResult.suggestions} />
                        <div className="flex items-center gap-2 pt-1">
                          {checkResult.changes.length > 0 && (
                            <button
                              onClick={() => void acceptCheck(entry)}
                              className="px-2.5 py-1 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 text-white transition-colors"
                            >
                              {t('books.editor.accept')}
                            </button>
                          )}
                          <button
                            onClick={() => setCheckResults(prev => omit(prev, entry.id))}
                            className="px-2.5 py-1 text-xs text-slate-400 hover:text-white transition-colors"
                          >
                            {t('books.editor.dismiss')}
                          </button>
                          <span className="ml-auto text-xs text-slate-500">
                            {t('books.editor.cost', { amount: checkResult.cost_czk.toLocaleString('cs-CZ', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) })}
                            {checkResult.cached && <span className="ml-1 text-emerald-500">({t('books.editor.cachedResult')})</span>}
                          </span>
                        </div>
                      </div>
                    )}

                    {/* Rewrite result */}
                    {rewriteResult && (
                      <div className="rounded border border-amber-500/30 bg-amber-950/30 p-3 space-y-2">
                        <p className="text-xs font-medium text-slate-400 mb-1">{t('books.editor.rewrittenText')}:</p>
                        <p className="text-xs text-slate-300 bg-slate-900/50 p-2 rounded whitespace-pre-wrap">{rewriteResult.rewritten_text}</p>
                        <div className="flex items-center gap-2 pt-1">
                          <button
                            onClick={() => void acceptRewrite(entry)}
                            className="px-2.5 py-1 text-xs font-medium rounded bg-amber-600 hover:bg-amber-700 text-white transition-colors"
                          >
                            {t('books.editor.accept')}
                          </button>
                          <button
                            onClick={() => setRewriteResults(prev => omit(prev, entry.id))}
                            className="px-2.5 py-1 text-xs text-slate-400 hover:text-white transition-colors"
                          >
                            {t('books.editor.dismiss')}
                          </button>
                          <span className="ml-auto text-xs text-slate-500">
                            {t('books.editor.cost', { amount: rewriteResult.cost_czk.toLocaleString('cs-CZ', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) })}
                            {rewriteResult.cached && <span className="ml-1 text-emerald-500">({t('books.editor.cachedResult')})</span>}
                          </span>
                        </div>
                      </div>
                    )}

                    {/* Error */}
                    {error && <p className="text-xs text-red-400">{error}</p>}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

    </div>
  );
}
