import { useState, useMemo, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { SpellCheck, ArrowLeftRight, Loader2, Check, DollarSign, ChevronDown, ChevronUp, ExternalLink, X, BarChart3, Copy, Search } from 'lucide-react';
import { checkText, rewriteText, checkTextConsistency, updateSectionPhoto, assignTextSlot } from '../../api/client';
import { StatsGrid } from '../../components/StatsGrid';
import { findDuplicateTexts } from '../../utils/textSimilarity';
import type { BookDetail, SectionPhoto } from '../../types';

type TargetLength = 'much_shorter' | 'shorter' | 'longer' | 'much_longer';

type CheckStatus = 'unchecked' | 'clean' | 'has_errors';

interface TextEntry {
  id: string;
  text: string;
  source: 'section_photo' | 'text_slot';
  // Section photo fields
  sectionId?: string;
  sectionTitle?: string;
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

  // In-memory check status tracking
  const [checkStatuses, setCheckStatuses] = useState<Record<string, CheckStatus>>({});

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

  // Build text entries from book data
  const textEntries = useMemo<TextEntry[]>(() => {
    const entries: TextEntry[] = [];

    // Photo descriptions from sections
    for (const section of book.sections) {
      const photos = sectionPhotos[section.id] || [];
      for (const photo of photos) {
        if (photo.description?.trim()) {
          entries.push({
            id: `sp-${section.id}-${photo.photo_uid}`,
            text: photo.description,
            source: 'section_photo',
            sectionId: section.id,
            sectionTitle: section.title,
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
          const pageIndex = book.pages
            .filter(p => p.section_id === page.section_id)
            .findIndex(p => p.id === page.id);
          entries.push({
            id: `ts-${page.id}-${slot.slot_index}`,
            text: slot.text_content,
            source: 'text_slot',
            pageId: page.id,
            pageNumber: pageIndex + 1,
            slotIndex: slot.slot_index,
          });
        }
      }
    }

    return entries;
  }, [book, sectionPhotos]);

  // Stats
  const totalTexts = textEntries.length;
  const checkedCount = Object.values(checkStatuses).filter(s => s === 'clean').length;
  const errorCount = Object.values(checkStatuses).filter(s => s === 'has_errors').length;
  const totalReadingTime = useMemo(() => {
    let total = 0;
    for (const e of textEntries) {
      const wc = e.text.trim().split(/\s+/).filter(Boolean).length;
      if (wc >= 10) total += Math.ceil((wc / 200) * 2) / 2;
      else if (wc > 0) total += 0.5;
    }
    return Math.ceil(total * 2) / 2;
  }, [textEntries]);

  // Duplicate detection
  const duplicatePairs = useMemo(
    () => findDuplicateTexts(textEntries.map(e => ({ id: e.id, text: e.text }))),
    [textEntries],
  );

  // Filtered entries based on search
  const filteredEntries = useMemo(() => {
    if (!searchQuery.trim()) return textEntries;
    const q = searchQuery.toLowerCase();
    return textEntries.filter(entry => {
      const source = entry.source === 'section_photo'
        ? (entry.sectionTitle || '').toLowerCase()
        : `page ${entry.pageNumber} slot ${(entry.slotIndex ?? 0) + 1}`.toLowerCase();
      return entry.text.toLowerCase().includes(q) || source.includes(q);
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
      const result = await checkText(entry.text);
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
  }, []);

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
    const unchecked = textEntries.filter(e => !checkStatuses[e.id] || checkStatuses[e.id] === 'unchecked');
    if (unchecked.length === 0) return;

    batchCancelledRef.current = false;
    setIsBatchChecking(true);
    setBatchProgress(0);
    setBatchTotal(unchecked.length);
    setBatchTotalCost(null);

    let totalCost = 0;

    for (let i = 0; i < unchecked.length; i++) {
      if (batchCancelledRef.current) break;

      const entry = unchecked[i];
      setBatchProgress(i + 1);
      setChecking(entry.id);
      setErrors(prev => ({ ...prev, [entry.id]: '' }));

      try {
        const result = await checkText(entry.text);
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
  }, [textEntries, checkStatuses]);

  const handleCancelBatch = useCallback(() => {
    batchCancelledRef.current = true;
  }, []);

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
      default:
        return <span className="inline-flex items-center gap-1 text-xs text-slate-500"><span className="w-2 h-2 rounded-full bg-slate-500" />{t('books.editor.unchecked')}</span>;
    }
  };

  const getSourceLabel = (entry: TextEntry) => {
    if (entry.source === 'section_photo') {
      return t('books.editor.sectionPhotoSource', { section: entry.sectionTitle || '?' });
    }
    return t('books.editor.pageSlotSource', { page: entry.pageNumber, slot: (entry.slotIndex ?? 0) + 1 });
  };

  const lengthOptions: { value: TargetLength; labelKey: string }[] = [
    { value: 'much_shorter', labelKey: 'books.editor.muchShorter' },
    { value: 'shorter', labelKey: 'books.editor.shorter' },
    { value: 'longer', labelKey: 'books.editor.longer' },
    { value: 'much_longer', labelKey: 'books.editor.muchLonger' },
  ];

  const allChecked = textEntries.length > 0 && textEntries.every(e => checkStatuses[e.id] === 'clean' || checkStatuses[e.id] === 'has_errors');
  const isAnyLoading = checking !== null || rewriting !== null || isConsistencyChecking;

  return (
    <div className="space-y-6">
      {/* Stats */}
      <StatsGrid
        columns={5}
        items={[
          { value: totalTexts, label: t('books.editor.totalTexts'), color: 'white' },
          { value: checkedCount, label: t('books.editor.checkedTexts'), color: 'green' },
          { value: errorCount, label: t('books.editor.textsWithErrors'), color: 'red' },
          { value: duplicatePairs.length, label: t('books.editor.duplicateTexts'), color: duplicatePairs.length > 0 ? 'yellow' : 'white' },
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
                          <p className="text-xs text-slate-500 mt-0.5">{getSourceLabel(entry)}</p>
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
                    <p className="text-xs text-slate-500 mt-0.5">
                      {getSourceLabel(entry)}
                      {(() => {
                        const wc = entry.text.trim().split(/\s+/).filter(Boolean).length;
                        if (wc === 0) return null;
                        const rt = wc < 10 ? null : Math.ceil((wc / 200) * 2) / 2;
                        return <> · {rt === null ? t('books.editor.readingTimeShort') : t('books.editor.readingTime', { time: rt })}</>;
                      })()}
                    </p>
                  </div>
                  <div className="flex items-center gap-3 flex-shrink-0">
                    {getStatusIndicator(entry.id)}

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
                        onClick={e => { e.stopPropagation(); onNavigateToSection(entry.sectionId!); }}
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

      {/* Duplicate texts */}
      {textEntries.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <Copy className="h-4 w-4 text-yellow-400" />
            <h3 className="text-sm font-medium text-slate-200">{t('books.editor.duplicateTexts')}</h3>
            {duplicatePairs.length > 0 && (
              <span className="text-xs text-yellow-400">({duplicatePairs.length})</span>
            )}
          </div>
          {duplicatePairs.length === 0 ? (
            <p className="text-sm text-slate-500 text-center py-4">{t('books.editor.noDuplicates')}</p>
          ) : (
            <div className="space-y-2">
              {duplicatePairs.map((pair, idx) => {
                const entryA = textEntries.find(e => e.id === pair.entryA.id);
                const entryB = textEntries.find(e => e.id === pair.entryB.id);
                return (
                  <div key={idx} className="bg-slate-800 rounded-lg p-3 space-y-2">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-medium text-yellow-400">
                        {t('books.editor.similarity', { percent: Math.round(pair.similarity * 100) })}
                      </span>
                    </div>
                    {/* Text A */}
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <p className="text-xs text-slate-300 truncate">
                          {pair.entryA.text.length > 80 ? pair.entryA.text.slice(0, 80) + '...' : pair.entryA.text}
                        </p>
                        {entryA && (
                          <p className="text-xs text-slate-500 mt-0.5">{getSourceLabel(entryA)}</p>
                        )}
                      </div>
                      {entryA && (
                        <button
                          onClick={() => {
                            if (entryA.source === 'text_slot' && entryA.pageId) onNavigateToPage(entryA.pageId);
                            else if (entryA.source === 'section_photo' && entryA.sectionId) onNavigateToSection(entryA.sectionId);
                          }}
                          className="text-xs text-yellow-400 hover:text-yellow-300 flex-shrink-0 transition-colors"
                        >
                          {t('books.editor.goToText')}
                        </button>
                      )}
                    </div>
                    {/* Text B */}
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <p className="text-xs text-slate-300 truncate">
                          {pair.entryB.text.length > 80 ? pair.entryB.text.slice(0, 80) + '...' : pair.entryB.text}
                        </p>
                        {entryB && (
                          <p className="text-xs text-slate-500 mt-0.5">{getSourceLabel(entryB)}</p>
                        )}
                      </div>
                      {entryB && (
                        <button
                          onClick={() => {
                            if (entryB.source === 'text_slot' && entryB.pageId) onNavigateToPage(entryB.pageId);
                            else if (entryB.source === 'section_photo' && entryB.sectionId) onNavigateToSection(entryB.sectionId);
                          }}
                          className="text-xs text-yellow-400 hover:text-yellow-300 flex-shrink-0 transition-colors"
                        >
                          {t('books.editor.goToText')}
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
