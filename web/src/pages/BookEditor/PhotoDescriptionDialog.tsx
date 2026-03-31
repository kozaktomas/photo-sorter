import { useState, useEffect, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { X, SpellCheck, ArrowLeftRight, Check, Loader2, DollarSign, History } from 'lucide-react';
import { getThumbnailUrl, updateSectionPhoto, checkTextAndSave, rewriteText, listTextVersions, restoreTextVersion } from '../../api/client';
import { handleMarkdownPaste } from '../../utils/paste';
import type { TextVersion } from '../../types';

interface Props {
  sectionId: string;
  photoUid: string;
  description: string;
  note: string;
  onSaved: () => void;
  onClose: () => void;
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

type TargetLength = 'much_shorter' | 'shorter' | 'longer' | 'much_longer';

export function PhotoDescriptionDialog({ sectionId, photoUid, description, note, onSaved, onClose }: Props) {
  const { t } = useTranslation('pages');
  const [desc, setDesc] = useState(description);
  const [noteText, setNoteText] = useState(note);
  const [saving, setSaving] = useState(false);

  // Text check state
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<CheckResult | null>(null);
  const [checkError, setCheckError] = useState('');

  // Text rewrite state
  const [rewriting, setRewriting] = useState(false);
  const [rewriteResult, setRewriteResult] = useState<RewriteResult | null>(null);
  const [rewriteError, setRewriteError] = useState('');
  const [targetLength, setTargetLength] = useState<TargetLength>('shorter');

  // History state
  const [showDescHistory, setShowDescHistory] = useState(false);
  const [showNoteHistory, setShowNoteHistory] = useState(false);
  const [descHistory, setDescHistory] = useState<TextVersion[]>([]);
  const [noteHistory, setNoteHistory] = useState<TextVersion[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);

  const sourceId = `${sectionId}:${photoUid}`;

  const loadDescHistory = useCallback(async () => {
    setHistoryLoading(true);
    try {
      const versions = await listTextVersions('section_photo', sourceId, 'description');
      setDescHistory(versions);
    } catch { /* silent */ }
    setHistoryLoading(false);
  }, [sourceId]);

  const loadNoteHistory = useCallback(async () => {
    setHistoryLoading(true);
    try {
      const versions = await listTextVersions('section_photo', sourceId, 'note');
      setNoteHistory(versions);
    } catch { /* silent */ }
    setHistoryLoading(false);
  }, [sourceId]);

  useEffect(() => {
    if (showDescHistory) void loadDescHistory();
  }, [showDescHistory, loadDescHistory]);

  useEffect(() => {
    if (showNoteHistory) void loadNoteHistory();
  }, [showNoteHistory, loadNoteHistory]);

  const handleRestore = async (id: number, target: 'desc' | 'note') => {
    try {
      const result = await restoreTextVersion(id);
      if (target === 'desc') {
        setDesc(result.content);
        void loadDescHistory();
      } else {
        setNoteText(result.content);
        void loadNoteHistory();
      }
    } catch { /* silent */ }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSectionPhoto(sectionId, photoUid, desc, noteText);
      onSaved();
    } catch {
      /* silent */
    } finally {
      setSaving(false);
    }
  };

  const handleCheck = async () => {
    setChecking(true);
    setCheckResult(null);
    setCheckError('');
    setRewriteResult(null);
    try {
      const sourceId = `${sectionId}:${photoUid}`;
      const result = await checkTextAndSave('section_photo', sourceId, 'description', desc);
      setCheckResult(result);
    } catch (err) {
      setCheckError(err instanceof Error ? err.message : 'Check failed');
    } finally {
      setChecking(false);
    }
  };

  const handleRewrite = async () => {
    setRewriting(true);
    setRewriteResult(null);
    setRewriteError('');
    setCheckResult(null);
    try {
      const result = await rewriteText(desc, targetLength);
      setRewriteResult(result);
    } catch (err) {
      setRewriteError(err instanceof Error ? err.message : 'Rewrite failed');
    } finally {
      setRewriting(false);
    }
  };

  const acceptCheck = () => {
    if (checkResult) {
      setDesc(checkResult.corrected_text);
      setCheckResult(null);
    }
  };

  const acceptRewrite = () => {
    if (rewriteResult) {
      setDesc(rewriteResult.rewritten_text);
      setRewriteResult(null);
    }
  };

  const descEmpty = desc.trim() === '';
  const aiLoading = checking || rewriting;

  // Keyboard shortcuts: Ctrl+Enter to save, Ctrl+Shift+C to check, Escape to close
  const handlersRef = useRef({ handleSave, handleCheck, onClose, descEmpty, aiLoading });
  handlersRef.current = { handleSave, handleCheck, onClose, descEmpty, aiLoading };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const { handleSave, handleCheck, onClose, descEmpty, aiLoading } = handlersRef.current;
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        void handleSave();
        return;
      }
      if (e.key === 'c' && (e.ctrlKey || e.metaKey) && e.shiftKey) {
        e.preventDefault();
        if (!descEmpty && !aiLoading) void handleCheck();
        return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  const lengthOptions: { value: TargetLength; labelKey: string }[] = [
    { value: 'much_shorter', labelKey: 'books.editor.muchShorter' },
    { value: 'shorter', labelKey: 'books.editor.shorter' },
    { value: 'longer', labelKey: 'books.editor.longer' },
    { value: 'much_longer', labelKey: 'books.editor.muchLonger' },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70" onClick={onClose}>
      <div
        className="bg-slate-800 border border-slate-700 rounded-lg w-full max-w-2xl mx-4 overflow-hidden max-h-[90vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700">
          <h3 className="text-sm font-medium text-white">{t('books.editor.photoDetailsTitle')}</h3>
          <button onClick={onClose} className="text-slate-400 hover:text-white">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4 space-y-4 overflow-y-auto">
          <div className="flex justify-center">
            <img
              src={getThumbnailUrl(photoUid, 'fit_720')}
              alt=""
              className="max-h-40 rounded object-contain"
            />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-slate-400">
                {t('books.editor.descriptionLabel')}
              </label>
              <button
                onClick={() => { setShowDescHistory(!showDescHistory); setShowNoteHistory(false); }}
                className="inline-flex items-center gap-1 text-xs text-slate-500 hover:text-slate-300 transition-colors"
              >
                <History className="h-3 w-3" />
                {t('books.editor.history')}
              </button>
            </div>
            {showDescHistory && (
              <VersionHistoryPanel
                versions={descHistory}
                loading={historyLoading}
                onRestore={(id) => handleRestore(id, 'desc')}
                t={t}
              />
            )}
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              onPaste={e => handleMarkdownPaste(e, setDesc)}
              placeholder={t('books.editor.descriptionPlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
              rows={8}
              autoFocus
            />
            <div className="flex items-center justify-between mt-1">
              <p className="text-xs text-slate-500">{t('books.editor.descriptionHelp')}</p>
              <p className="text-xs text-slate-500">
                {(() => {
                  const wc = desc.trim().split(/\s+/).filter(Boolean).length;
                  const rt = wc < 10 ? null : Math.ceil((wc / 200) * 2) / 2;
                  return (
                    <>
                      {t('books.editor.charCount', { count: desc.length })} · {t('books.editor.wordCount', { count: wc })}
                      {wc > 0 && <> · {rt === null ? t('books.editor.readingTimeShort') : t('books.editor.readingTime', { time: rt })}</>}
                    </>
                  );
                })()}
              </p>
            </div>
          </div>

          {/* AI buttons */}
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleCheck}
              disabled={descEmpty || aiLoading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
            >
              {checking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <><SpellCheck className="h-3.5 w-3.5" /><DollarSign className="h-3 w-3 -ml-1 opacity-60" /></>}
              {checking ? t('books.editor.checking') : t('books.editor.textCheck')}
            </button>

            <div className="inline-flex items-center gap-1.5">
              <select
                value={targetLength}
                onChange={(e) => setTargetLength(e.target.value as TargetLength)}
                disabled={aiLoading}
                className="px-2 py-1.5 text-xs bg-slate-900 border border-slate-600 rounded text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-amber-500"
              >
                {lengthOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>{t(opt.labelKey)}</option>
                ))}
              </select>
              <button
                onClick={handleRewrite}
                disabled={descEmpty || aiLoading}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded bg-amber-600 hover:bg-amber-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
              >
                {rewriting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <><ArrowLeftRight className="h-3.5 w-3.5" /><DollarSign className="h-3 w-3 -ml-1 opacity-60" /></>}
                {rewriting ? t('books.editor.rewriting') : t('books.editor.adjustLength')}
              </button>
            </div>
          </div>

          {/* Check result panel */}
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
                    onClick={acceptCheck}
                    className="px-2.5 py-1 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 text-white transition-colors"
                  >
                    {t('books.editor.accept')}
                  </button>
                )}
                <button
                  onClick={() => setCheckResult(null)}
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
          {checkError && (
            <p className="text-xs text-red-400">{checkError}</p>
          )}

          {/* Rewrite result panel */}
          {rewriteResult && (
            <div className="rounded border border-amber-500/30 bg-amber-950/30 p-3 space-y-2">
              <p className="text-xs font-medium text-slate-400 mb-1">{t('books.editor.rewrittenText')}:</p>
              <p className="text-xs text-slate-300 bg-slate-900/50 p-2 rounded whitespace-pre-wrap">{rewriteResult.rewritten_text}</p>
              <div className="flex items-center gap-2 pt-1">
                <button
                  onClick={acceptRewrite}
                  className="px-2.5 py-1 text-xs font-medium rounded bg-amber-600 hover:bg-amber-700 text-white transition-colors"
                >
                  {t('books.editor.accept')}
                </button>
                <button
                  onClick={() => setRewriteResult(null)}
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
          {rewriteError && (
            <p className="text-xs text-red-400">{rewriteError}</p>
          )}

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-slate-400">
                {t('books.editor.noteLabel')}
              </label>
              <button
                onClick={() => { setShowNoteHistory(!showNoteHistory); setShowDescHistory(false); }}
                className="inline-flex items-center gap-1 text-xs text-slate-500 hover:text-slate-300 transition-colors"
              >
                <History className="h-3 w-3" />
                {t('books.editor.history')}
              </button>
            </div>
            {showNoteHistory && (
              <VersionHistoryPanel
                versions={noteHistory}
                loading={historyLoading}
                onRestore={(id) => handleRestore(id, 'note')}
                t={t}
              />
            )}
            <textarea
              value={noteText}
              onChange={(e) => setNoteText(e.target.value)}
              onPaste={e => handleMarkdownPaste(e, setNoteText)}
              placeholder={t('books.editor.notePlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus-visible:ring-1 focus-visible:ring-amber-500"
              rows={2}
            />
            <div className="flex items-center justify-between mt-1">
              <p className="text-xs text-slate-500">{t('books.editor.noteHelp')}</p>
              <p className="text-xs text-slate-500">{t('books.editor.charCount', { count: noteText.length })}</p>
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-2 px-4 py-3 border-t border-slate-700">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm text-slate-400 hover:text-white transition-colors"
          >
            {t('books.editor.closeModal')}
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-1.5 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white text-sm rounded transition-colors"
          >
            {saving ? '...' : t('books.editor.saveButton')}
          </button>
        </div>
      </div>
    </div>
  );
}

function VersionHistoryPanel({
  versions, loading, onRestore, t,
}: {
  versions: TextVersion[];
  loading: boolean;
  onRestore: (id: number) => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  if (loading) {
    return (
      <div className="mb-2 p-2 bg-slate-900/50 border border-slate-700 rounded text-xs text-slate-500">
        <Loader2 className="h-3 w-3 animate-spin inline mr-1" />
        ...
      </div>
    );
  }

  if (versions.length === 0) {
    return (
      <div className="mb-2 p-2 bg-slate-900/50 border border-slate-700 rounded text-xs text-slate-500">
        {t('books.editor.noHistory')}
      </div>
    );
  }

  return (
    <div className="mb-2 max-h-32 overflow-y-auto bg-slate-900/50 border border-slate-700 rounded divide-y divide-slate-700/50">
      {versions.map((v) => (
        <div key={v.id} className="flex items-center gap-2 px-2 py-1.5 text-xs">
          <span className="flex-1 text-slate-300 truncate" title={v.content}>
            {v.content.length > 50 ? v.content.slice(0, 50) + '...' : v.content}
          </span>
          <span className={`shrink-0 px-1 rounded text-[10px] ${v.changed_by === 'ai' ? 'bg-indigo-900/50 text-indigo-300' : 'bg-slate-700 text-slate-400'}`}>
            {v.changed_by === 'ai' ? t('books.editor.changedByAi') : t('books.editor.changedByUser')}
          </span>
          <span className="shrink-0 text-slate-500">
            {new Date(v.created_at).toLocaleString()}
          </span>
          <button
            onClick={() => onRestore(v.id)}
            className="shrink-0 px-1.5 py-0.5 text-[10px] font-medium rounded bg-rose-600/80 hover:bg-rose-600 text-white transition-colors"
          >
            {t('books.editor.restore')}
          </button>
        </div>
      ))}
    </div>
  );
}
