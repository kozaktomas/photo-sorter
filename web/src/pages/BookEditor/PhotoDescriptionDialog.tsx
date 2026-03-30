import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { X, SpellCheck, ArrowLeftRight, Check, Loader2 } from 'lucide-react';
import { getThumbnailUrl, updateSectionPhoto, checkText, rewriteText } from '../../api/client';

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
}

interface RewriteResult {
  rewritten_text: string;
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
      const result = await checkText(desc);
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
            <label className="block text-xs font-medium text-slate-400 mb-1">
              {t('books.editor.descriptionLabel')}
            </label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder={t('books.editor.descriptionPlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
              rows={8}
              autoFocus
            />
            <p className="text-xs text-slate-500 mt-1">{t('books.editor.descriptionHelp')}</p>
          </div>

          {/* AI buttons */}
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleCheck}
              disabled={descEmpty || aiLoading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
            >
              {checking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <SpellCheck className="h-3.5 w-3.5" />}
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
                {rewriting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ArrowLeftRight className="h-3.5 w-3.5" />}
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
              <div className="flex gap-2 pt-1">
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
              <div className="flex gap-2 pt-1">
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
              </div>
            </div>
          )}
          {rewriteError && (
            <p className="text-xs text-red-400">{rewriteError}</p>
          )}

          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              {t('books.editor.noteLabel')}
            </label>
            <textarea
              value={noteText}
              onChange={(e) => setNoteText(e.target.value)}
              placeholder={t('books.editor.notePlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white resize-none focus:outline-none focus-visible:ring-1 focus-visible:ring-amber-500"
              rows={2}
            />
            <p className="text-xs text-slate-500 mt-1">{t('books.editor.noteHelp')}</p>
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
