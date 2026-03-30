import { useState, useMemo, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { SpellCheck, ArrowLeftRight, Loader2, Check, DollarSign, ChevronDown, ChevronUp, ExternalLink, X } from 'lucide-react';
import { checkText, rewriteText, updateSectionPhoto, assignTextSlot } from '../../api/client';
import { StatsGrid } from '../../components/StatsGrid';
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
  const isAnyLoading = checking !== null || rewriting !== null;

  return (
    <div className="space-y-6">
      {/* Stats */}
      <StatsGrid
        columns={3}
        items={[
          { value: totalTexts, label: t('books.editor.totalTexts'), color: 'white' },
          { value: checkedCount, label: t('books.editor.checkedTexts'), color: 'green' },
          { value: errorCount, label: t('books.editor.textsWithErrors'), color: 'red' },
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
                disabled={allChecked || isAnyLoading}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
              >
                <SpellCheck className="h-4 w-4" />
                <DollarSign className="h-3.5 w-3.5 -ml-1 opacity-60" />
                {t('books.editor.checkAll')}
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

      {/* Text list */}
      {textEntries.length === 0 ? (
        <p className="text-sm text-slate-500 text-center py-8">{t('books.editor.totalTexts')}: 0</p>
      ) : (
        <div className="space-y-2">
          {textEntries.map(entry => {
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
                      {entry.text.length > 100 ? entry.text.slice(0, 100) + '...' : entry.text}
                    </p>
                    <p className="text-xs text-slate-500 mt-0.5">{getSourceLabel(entry)}</p>
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
    </div>
  );
}
