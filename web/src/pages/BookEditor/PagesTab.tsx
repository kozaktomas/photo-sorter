import { useState, useEffect, useCallback, useMemo, useRef, type Dispatch, type SetStateAction, type PointerEvent as ReactPointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { DndContext, DragOverlay, KeyboardSensor, PointerSensor, pointerWithin, useSensor, useSensors, type DragEndEvent, type DragStartEvent, type Modifier } from '@dnd-kit/core';
import { arrayMove, sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import { Type, Heading1, Heading2, Bold, Italic, List, ListOrdered, LayoutGrid, Wand2, Loader2, SpellCheck, ArrowLeftRight, Check, DollarSign, History, Maximize2, Minimize2, Eye, Printer } from 'lucide-react';
import { assignSlot, assignTextSlot, clearSlot, swapSlots, updatePage, updateSlotCrop, reorderPages, getThumbnailUrl, autoLayoutSection, checkTextAndSave, rewriteText, listTextVersions, restoreTextVersion } from '../../api/client';
import { MarkdownContent, contrastTextColor, renderMarkdown } from '../../utils/markdown';
import { handleMarkdownPaste } from '../../utils/paste';
import { useBookKeyboardNav } from '../../hooks/useBookKeyboardNav';
import { useUndoRedo, type SlotContent, type UndoEntry } from './hooks/useUndoRedo';
import { PageSidebar } from './PageSidebar';
import { PageMinimap } from './PageMinimap';
import { PageTemplate } from './PageTemplate';
import { UnassignedPool } from './UnassignedPool';
import { PhotoDescriptionDialog } from './PhotoDescriptionDialog';
import type { BookDetail, SectionPhoto, PageFormat, PageStyle, PageSlot, TextVersion } from '../../types';
import { pageFormatSlotCount } from '../../types';
import { getSlotAspectRatio, getSlotRects } from '../../utils/pageFormats';
import { PageLayoutPreview } from '../../components/PageLayoutPreview';
import { BOOK_TYPOGRAPHY } from '../../constants/bookTypography';

// Snap the DragOverlay center to the cursor so large source elements don't cause offset
const snapCenterToCursor: Modifier = ({ activatorEvent, activeNodeRect, draggingNodeRect, transform }) => {
  if (activatorEvent instanceof PointerEvent && activeNodeRect && draggingNodeRect) {
    const grabX = activatorEvent.clientX - activeNodeRect.left;
    const grabY = activatorEvent.clientY - activeNodeRect.top;
    return {
      ...transform,
      x: transform.x + grabX - draggingNodeRect.width / 2,
      y: transform.y + grabY - draggingNodeRect.height / 2,
    };
  }
  return transform;
};

interface Props {
  book: BookDetail;
  setBook: Dispatch<SetStateAction<BookDetail | null>>;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
  initialPageId?: string | null;
  onPageSelect?: (pageId: string | null) => void;
}

// Insert markdown syntax at cursor position (or wrap selection)
function insertMarkdown(textarea: HTMLTextAreaElement, prefix: string, suffix: string, setValue: (v: string) => void, block?: boolean) {
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  const val = textarea.value;
  const selected = val.substring(start, end);

  let newText: string;
  let cursorPos: number;

  if (block) {
    // Block-level: insert at line start
    const lineStart = val.lastIndexOf('\n', start - 1) + 1;
    const before = val.substring(0, lineStart);
    const after = val.substring(lineStart);
    newText = before + prefix + after;
    cursorPos = start + prefix.length;
  } else if (selected) {
    newText = val.substring(0, start) + prefix + selected + suffix + val.substring(end);
    cursorPos = start + prefix.length + selected.length + suffix.length;
  } else {
    newText = val.substring(0, start) + prefix + suffix + val.substring(end);
    cursorPos = start + prefix.length;
  }

  setValue(newText);
  // Restore cursor position after React re-render
  requestAnimationFrame(() => {
    textarea.focus();
    textarea.setSelectionRange(cursorPos, cursorPos);
  });
}

// Inline text slot editing dialog with markdown toolbar and preview
type TargetLength = 'much_shorter' | 'shorter' | 'longer' | 'much_longer';

function TextSlotDialog({ text, pageId, slotIndex, pageFormat, pageSlots, splitPosition, chapterColor, onSave, onClose }: {
  text: string; pageId: string; slotIndex: number;
  pageFormat: PageFormat; pageSlots: PageSlot[];
  splitPosition: number | null;
  chapterColor?: string;
  onSave: (text: string) => void; onClose: () => void;
}) {
  const { t } = useTranslation('pages');
  const [value, setValue] = useState(text);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [previewMode, setPreviewMode] = useState<'print' | 'editor'>('print');
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const wysiwygContentRef = useRef<HTMLDivElement>(null);
  const wysiwygContainerRef = useRef<HTMLDivElement>(null);
  const [fillPercent, setFillPercent] = useState<number | null>(null);

  // AI text check state
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<{ corrected_text: string; readability_score: number; changes: string[]; cost_czk: number; cached: boolean } | null>(null);
  const [checkError, setCheckError] = useState('');

  // AI text rewrite state
  const [rewriting, setRewriting] = useState(false);
  const [rewriteResult, setRewriteResult] = useState<{ rewritten_text: string; cost_czk: number; cached: boolean } | null>(null);
  const [rewriteError, setRewriteError] = useState('');
  const [targetLength, setTargetLength] = useState<TargetLength>('shorter');

  // History state
  const [showHistory, setShowHistory] = useState(false);
  const [history, setHistory] = useState<TextVersion[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);

  const sourceId = `${pageId}:${slotIndex}`;

  // Compute slot dimensions in mm for WYSIWYG preview
  const slotRect = useMemo(() => {
    const rects = getSlotRects(pageFormat, splitPosition);
    return rects[slotIndex] ?? rects[0];
  }, [pageFormat, splitPosition, slotIndex]);

  // Measure fill percentage from rendered WYSIWYG content
  useEffect(() => {
    if (previewMode !== 'print') return;
    const container = wysiwygContainerRef.current;
    const content = wysiwygContentRef.current;
    if (!container || !content) { setFillPercent(null); return; }
    const containerH = container.clientHeight;
    if (containerH <= 0) { setFillPercent(null); return; }
    const contentH = content.scrollHeight;
    setFillPercent(Math.round((contentH / containerH) * 100));
  }, [value, previewMode, pageFormat, splitPosition, slotIndex]);

  const loadHistory = useCallback(async () => {
    setHistoryLoading(true);
    try {
      const versions = await listTextVersions('page_slot', sourceId, 'text_content');
      setHistory(versions);
    } catch { /* silent */ }
    setHistoryLoading(false);
  }, [sourceId]);

  useEffect(() => {
    if (showHistory) void loadHistory();
  }, [showHistory, loadHistory]);

  const handleHistoryRestore = async (id: number) => {
    try {
      const result = await restoreTextVersion(id);
      setValue(result.content);
      void loadHistory();
    } catch { /* silent */ }
  };

  const valueEmpty = value.trim() === '';
  const aiLoading = checking || rewriting;

  const handleCheck = async () => {
    setChecking(true);
    setCheckResult(null);
    setCheckError('');
    setRewriteResult(null);
    try {
      const result = await checkTextAndSave('page_slot', sourceId, 'text_content', value);
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
      const result = await rewriteText(value, targetLength);
      setRewriteResult(result);
    } catch (err) {
      setRewriteError(err instanceof Error ? err.message : 'Rewrite failed');
    } finally {
      setRewriting(false);
    }
  };

  const acceptCheck = () => {
    if (checkResult) {
      setValue(checkResult.corrected_text);
      setCheckResult(null);
    }
  };

  const acceptRewrite = () => {
    if (rewriteResult) {
      setValue(rewriteResult.rewritten_text);
      setRewriteResult(null);
    }
  };

  // Keyboard shortcuts: Ctrl+Enter to save, Ctrl+Shift+C to check, F11/Ctrl+Shift+F fullscreen, Escape to exit
  const textDialogRef = useRef({ value, valueEmpty, aiLoading, isFullscreen, onSave, onClose, handleCheck, setIsFullscreen });
  textDialogRef.current = { value, valueEmpty, aiLoading, isFullscreen, onSave, onClose, handleCheck, setIsFullscreen };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const { value, valueEmpty, aiLoading, isFullscreen, onSave, onClose, handleCheck, setIsFullscreen } = textDialogRef.current;
      if (e.key === 'Escape') {
        e.preventDefault();
        if (isFullscreen) {
          setIsFullscreen(false);
        } else {
          onClose();
        }
        return;
      }
      if (e.key === 'F11' || (e.key === 'F' && (e.ctrlKey || e.metaKey) && e.shiftKey)) {
        e.preventDefault();
        setIsFullscreen((v: boolean) => !v);
        return;
      }
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        if (value.trim()) onSave(value);
        return;
      }
      if (e.key === 'c' && (e.ctrlKey || e.metaKey) && e.shiftKey) {
        e.preventDefault();
        if (!valueEmpty && !aiLoading) void handleCheck();
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

  const toolbarButtons = [
    { icon: Heading1, title: 'H1', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '# ', '', setValue, true) },
    { icon: Heading2, title: 'H2', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '## ', '', setValue, true) },
    { icon: Bold, title: 'Bold', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '**', '**', setValue) },
    { icon: Italic, title: 'Italic', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '*', '*', setValue) },
    { icon: List, title: 'UL', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '- ', '', setValue, true) },
    { icon: ListOrdered, title: 'OL', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '1. ', '', setValue, true) },
  ];

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className={`bg-slate-800 p-6 flex flex-col transition-all duration-200 ${
          isFullscreen
            ? 'fixed inset-0 w-full h-full rounded-none max-w-none max-h-none'
            : 'rounded-lg w-full max-w-4xl max-h-[90vh]'
        }`}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-white">{t('books.editor.textSlotTitle')}</h3>
          <button
            onClick={() => setIsFullscreen(f => !f)}
            className="p-1.5 rounded hover:bg-slate-700 text-slate-400 hover:text-white transition-colors"
            title={isFullscreen ? t('books.editor.minimize') : t('books.editor.maximize')}
          >
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
        </div>
        <div className="flex items-center gap-1 mb-2">
          {toolbarButtons.map(btn => (
            <button
              key={btn.title}
              onClick={btn.action}
              onPointerDown={e => e.preventDefault()}
              className="p-1.5 rounded hover:bg-slate-700 text-slate-400 hover:text-white transition-colors"
              title={btn.title}
            >
              <btn.icon className="h-4 w-4" />
            </button>
          ))}
          <div className="flex-1" />
          <button
            onClick={() => setShowHistory(!showHistory)}
            className="inline-flex items-center gap-1 px-2 py-1 text-xs text-slate-500 hover:text-slate-300 transition-colors"
          >
            <History className="h-3 w-3" />
            {t('books.editor.history')}
          </button>
        </div>
        {showHistory && (
          <TextSlotHistoryPanel
            versions={history}
            loading={historyLoading}
            onRestore={handleHistoryRestore}
            t={t}
          />
        )}
        <div className="overflow-y-auto flex-1 min-h-0 space-y-3">
          <div className="flex gap-4">
            <div className="flex-1 min-w-0 grid grid-cols-2 gap-4">
              <textarea
                ref={textareaRef}
                value={value}
                onChange={e => setValue(e.target.value)}
                onPaste={e => handleMarkdownPaste(e, setValue)}
                className={`w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white font-mono focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500 resize-none ${isFullscreen ? 'h-[70vh]' : 'h-80'}`}
                placeholder={t('books.editor.textPlaceholder')}
                autoFocus
              />
              <div className={`flex flex-col bg-slate-900 border border-slate-600 rounded ${isFullscreen ? 'h-[70vh]' : 'h-80'}`}>
                {/* Preview mode toggle */}
                <div className="flex items-center gap-1 px-3 pt-2 pb-1 shrink-0">
                  <button
                    onClick={() => setPreviewMode('print')}
                    className={`inline-flex items-center gap-1 px-2 py-1 text-xs rounded transition-colors ${
                      previewMode === 'print'
                        ? 'bg-rose-600/20 text-rose-300 font-medium'
                        : 'text-slate-500 hover:text-slate-300'
                    }`}
                  >
                    <Printer className="h-3 w-3" />
                    {t('books.editor.printPreview')}
                  </button>
                  <button
                    onClick={() => setPreviewMode('editor')}
                    className={`inline-flex items-center gap-1 px-2 py-1 text-xs rounded transition-colors ${
                      previewMode === 'editor'
                        ? 'bg-rose-600/20 text-rose-300 font-medium'
                        : 'text-slate-500 hover:text-slate-300'
                    }`}
                  >
                    <Eye className="h-3 w-3" />
                    {t('books.editor.editorPreview')}
                  </button>
                  {previewMode === 'print' && fillPercent !== null && value.trim() && (
                    <span className={`ml-auto text-xs font-medium ${
                      fillPercent > 100 ? 'text-red-400' : fillPercent > 85 ? 'text-amber-400' : 'text-emerald-400'
                    }`}>
                      ~{fillPercent}%
                    </span>
                  )}
                </div>
                {/* Preview content */}
                <div className="flex-1 min-h-0 overflow-auto p-3 pt-1">
                  {previewMode === 'editor' ? (
                    // Editor preview (existing dark-themed markdown)
                    value.trim() ? (
                      <MarkdownContent content={value} />
                    ) : (
                      <p className="text-slate-600 text-sm italic">{t('books.editor.textPlaceholder')}</p>
                    )
                  ) : (
                    // WYSIWYG print preview with PDF-accurate dimensions
                    <div className="flex justify-center">
                      <div
                        ref={wysiwygContainerRef}
                        className="relative bg-white border border-slate-300 shadow-sm"
                        style={{
                          width: `${slotRect.w}mm`,
                          height: `${slotRect.h}mm`,
                        }}
                      >
                        {value.trim() ? (
                          <div
                            ref={wysiwygContentRef}
                            className="wysiwyg-preview"
                            style={{
                              fontFamily: BOOK_TYPOGRAPHY.textSlot.fontFamily,
                              fontSize: BOOK_TYPOGRAPHY.textSlot.fontSize,
                              lineHeight: BOOK_TYPOGRAPHY.textSlot.lineHeight,
                              ...(chapterColor ? { '--chapter-color': chapterColor, '--chapter-text-color': contrastTextColor(chapterColor) } as React.CSSProperties : {}),
                            }}
                            dangerouslySetInnerHTML={{ __html: renderMarkdown(value) }}
                          />
                        ) : (
                          <div className="w-full h-full flex items-center justify-center">
                            <p className="text-slate-300 text-sm italic">{t('books.editor.emptySlot')}</p>
                          </div>
                        )}
                        {/* Overflow region — reduced opacity overlay beyond slot boundary */}
                        {fillPercent !== null && fillPercent > 100 && (
                          <>
                            <div
                              className="absolute left-0 right-0 bg-red-50/70 pointer-events-none"
                              style={{ top: `${slotRect.h}mm`, bottom: 0 }}
                            />
                            <div
                              className="absolute left-0 right-0 border-t-2 border-dashed border-red-400 pointer-events-none"
                              style={{ top: `${slotRect.h}mm` }}
                            />
                          </>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
            <div className="shrink-0">
              <PageLayoutPreview
                format={pageFormat}
                activeSlotIndex={slotIndex}
                slots={pageSlots}
                splitPosition={splitPosition}
                liveText={value}
                previewWidth={200}
              />
            </div>
          </div>
          <div className="flex items-center justify-between">
            <p className="text-xs text-slate-500">{t('books.editor.markdownHelp')}</p>
            <p className="text-xs text-slate-500">
              {(() => {
                const wc = value.trim().split(/\s+/).filter(Boolean).length;
                const rt = wc < 10 ? null : Math.ceil((wc / 200) * 2) / 2;
                return (
                  <>
                    {t('books.editor.charCount', { count: value.length })} · {t('books.editor.wordCount', { count: wc })}
                    {wc > 0 && <> · {rt === null ? t('books.editor.readingTimeShort') : t('books.editor.readingTime', { time: rt })}</>}
                  </>
                );
              })()}
            </p>
          </div>

          {/* AI buttons */}
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleCheck}
              disabled={valueEmpty || aiLoading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded bg-indigo-600 hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed text-white transition-colors"
            >
              {checking ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <><SpellCheck className="h-3.5 w-3.5" /><DollarSign className="h-3 w-3 -ml-1 opacity-60" /></>}
              {checking ? t('books.editor.checking') : t('books.editor.textCheck')}
            </button>

            <div className="inline-flex items-center gap-1.5">
              <select
                value={targetLength}
                onChange={e => setTargetLength(e.target.value as TargetLength)}
                disabled={aiLoading}
                className="px-2 py-1.5 text-xs bg-slate-900 border border-slate-600 rounded text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-amber-500"
              >
                {lengthOptions.map(opt => (
                  <option key={opt.value} value={opt.value}>{t(opt.labelKey)}</option>
                ))}
              </select>
              <button
                onClick={handleRewrite}
                disabled={valueEmpty || aiLoading}
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
          {checkError && <p className="text-xs text-red-400">{checkError}</p>}

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
          {rewriteError && <p className="text-xs text-red-400">{rewriteError}</p>}
        </div>

        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-slate-300 hover:text-white transition-colors"
          >
            {t('books.editor.closeModal')}
          </button>
          <button
            onClick={() => onSave(value)}
            disabled={!value.trim()}
            className="px-4 py-2 text-sm bg-rose-600 hover:bg-rose-700 text-white rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {t('books.editor.saveButton')}
          </button>
        </div>
      </div>
    </div>
  );
}

// Crop adjustment dialog with visual crop box overlay
function CropDialog({ photoUid, cropX, cropY, cropScale: initialScale, format, slotIndex, splitPosition, onSave, onClose }: {
  photoUid: string; cropX: number; cropY: number; cropScale: number;
  format: PageFormat; slotIndex: number; splitPosition?: number | null;
  onSave: (x: number, y: number, scale: number) => void; onClose: () => void;
}) {
  const { t } = useTranslation('pages');
  const [x, setX] = useState(cropX);
  const [y, setY] = useState(cropY);
  const [scale, setScale] = useState(initialScale);
  const [naturalSize, setNaturalSize] = useState<{ w: number; h: number } | null>(null);
  const [containerSize, setContainerSize] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
  const containerRef = useRef<HTMLDivElement>(null);
  const draggingRef = useRef<{
    mode: 'move' | 'resize';
    startX: number; startY: number;
    startCropX: number; startCropY: number;
    startScale: number;
    anchorTopLeftX: number; anchorTopY: number;
  } | null>(null);

  const slotAR = getSlotAspectRatio(format, slotIndex, splitPosition);

  // Measure container with ResizeObserver
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const rect = entries[0].contentRect;
      setContainerSize({ w: rect.width, h: rect.height });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Compute image display rect within the container (object-contain positioning)
  const layout = useMemo(() => {
    if (!naturalSize || containerSize.w === 0 || containerSize.h === 0) return null;
    const photoAR = naturalSize.w / naturalSize.h;
    const containerAR = containerSize.w / containerSize.h;

    let displayW: number, displayH: number, offsetX: number, offsetY: number;
    if (photoAR > containerAR) {
      displayW = containerSize.w;
      displayH = containerSize.w / photoAR;
      offsetX = 0;
      offsetY = (containerSize.h - displayH) / 2;
    } else {
      displayH = containerSize.h;
      displayW = containerSize.h * photoAR;
      offsetX = (containerSize.w - displayW) / 2;
      offsetY = 0;
    }

    // Maximum crop box (scale=1.0): fills one axis completely
    let maxBoxW: number, maxBoxH: number;
    if (photoAR > slotAR) {
      maxBoxH = displayH;
      maxBoxW = displayH * slotAR;
    } else {
      maxBoxW = displayW;
      maxBoxH = displayW / slotAR;
    }

    return { displayW, displayH, offsetX, offsetY, maxBoxW, maxBoxH, photoAR };
  }, [naturalSize, containerSize, slotAR]);

  // Compute crop box position from x,y,scale values
  const cropBoxStyle = useMemo(() => {
    if (!layout) return null;
    const { offsetX, offsetY, displayW, displayH, maxBoxW, maxBoxH } = layout;
    const boxW = maxBoxW * scale;
    const boxH = maxBoxH * scale;
    const overflowX = displayW - boxW;
    const overflowY = displayH - boxH;
    const left = offsetX + x * overflowX;
    const top = offsetY + y * overflowY;
    return { left, top, width: boxW, height: boxH, overflowX, overflowY };
  }, [layout, x, y, scale]);

  const handlePointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    draggingRef.current = { mode: 'move', startX: e.clientX, startY: e.clientY, startCropX: x, startCropY: y, startScale: scale, anchorTopLeftX: 0, anchorTopY: 0 };
  }, [x, y, scale]);

  const handleResizePointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    if (!cropBoxStyle) return;
    const { left, top } = cropBoxStyle;
    draggingRef.current = {
      mode: 'resize',
      startX: e.clientX, startY: e.clientY,
      startCropX: x, startCropY: y, startScale: scale,
      anchorTopLeftX: left, anchorTopY: top,
    };
  }, [x, y, scale, cropBoxStyle]);

  const handlePointerMove = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current || !cropBoxStyle || !layout) return;
    const drag = draggingRef.current;
    const dx = e.clientX - drag.startX;
    const dy = e.clientY - drag.startY;

    if (drag.mode === 'move') {
      const { overflowX, overflowY } = cropBoxStyle;
      const sensX = overflowX > 1 ? overflowX : layout.displayW;
      const sensY = overflowY > 1 ? overflowY : layout.displayH;
      setX(Math.max(0, Math.min(1, drag.startCropX + dx / sensX)));
      setY(Math.max(0, Math.min(1, drag.startCropY + dy / sensY)));
    } else {
      // Resize mode: project mouse delta onto bottom-right diagonal (right+down = bigger)
      const delta = (dx + dy) / 2;
      const sensitivity = Math.max(layout.maxBoxW, layout.maxBoxH);
      const newScale = Math.max(0.1, Math.min(1, drag.startScale + delta / sensitivity));

      const newBoxW = layout.maxBoxW * newScale;
      const newBoxH = layout.maxBoxH * newScale;
      const newOverflowX = layout.displayW - newBoxW;
      const newOverflowY = layout.displayH - newBoxH;

      // Keep top-left corner pinned
      let newX = drag.startCropX;
      let newY = drag.startCropY;
      if (newOverflowX > 1) {
        newX = (drag.anchorTopLeftX - layout.offsetX) / newOverflowX;
        newX = Math.max(0, Math.min(1, newX));
      }
      if (newOverflowY > 1) {
        newY = (drag.anchorTopY - layout.offsetY) / newOverflowY;
        newY = Math.max(0, Math.min(1, newY));
      }

      setScale(newScale);
      setX(newX);
      setY(newY);
    }
  }, [cropBoxStyle, layout]);

  const handlePointerUp = useCallback(() => {
    draggingRef.current = null;
  }, []);

  // Mouse wheel to zoom
  const handleWheel = useCallback((e: React.WheelEvent) => {
    e.preventDefault();
    setScale(prev => Math.max(0.1, Math.min(1, prev + (e.deltaY > 0 ? 0.05 : -0.05))));
  }, []);

  // Dimming overlay divs around the crop box
  const dimOverlays = useMemo(() => {
    if (!cropBoxStyle) return null;
    const { left, top, width, height } = cropBoxStyle;
    const cw = containerSize.w;
    const ch = containerSize.h;
    return (
      <>
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top: 0, width: cw, height: top }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top: top + height, width: cw, height: ch - top - height }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top, width: left, height }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: left + width, top, width: cw - left - width, height }} />
      </>
    );
  }, [cropBoxStyle, containerSize]);

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-slate-800 rounded-lg p-6 w-full max-w-2xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">{t('books.editor.cropTitle')}</h3>
        <div
          ref={containerRef}
          className="relative w-full rounded overflow-hidden mb-4 bg-slate-900 select-none"
          style={{ aspectRatio: '16 / 10' }}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onWheel={handleWheel}
        >
          <img
            src={getThumbnailUrl(photoUid, 'fit_1920')}
            alt=""
            className="w-full h-full object-contain"
            draggable={false}
            onLoad={e => {
              const img = e.currentTarget;
              setNaturalSize({ w: img.naturalWidth, h: img.naturalHeight });
            }}
          />
          {dimOverlays}
          {cropBoxStyle && (
            <div
              className="absolute border-2 border-white/90 cursor-move"
              style={{
                left: cropBoxStyle.left,
                top: cropBoxStyle.top,
                width: cropBoxStyle.width,
                height: cropBoxStyle.height,
              }}
              onPointerDown={handlePointerDown}
            >
              <div
                className="absolute bottom-0 right-0 w-4 h-4 cursor-nwse-resize z-10"
                style={{ transform: 'translate(50%, 50%)' }}
                onPointerDown={handleResizePointerDown}
              >
                <div className="w-2.5 h-2.5 bg-white/90 border border-white rounded-sm" />
              </div>
            </div>
          )}
        </div>
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropHorizontal')}</label>
            <input
              type="range"
              min={0}
              max={100}
              value={Math.round(x * 100)}
              onChange={(e) => setX(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(x * 100)}%</span>
          </div>
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropVertical')}</label>
            <input
              type="range"
              min={0}
              max={100}
              value={Math.round(y * 100)}
              onChange={(e) => setY(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(y * 100)}%</span>
          </div>
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropZoom')}</label>
            <input
              type="range"
              min={10}
              max={100}
              value={Math.round(scale * 100)}
              onChange={(e) => setScale(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(scale * 100)}%</span>
          </div>
        </div>
        <div className="flex items-center justify-between mt-2">
          <p className="text-xs text-slate-500">{t('books.editor.cropDragHint')}</p>
          {layout && naturalSize && (
            <span className="text-xs text-slate-500 tabular-nums whitespace-nowrap ml-3">
              {Math.round(naturalSize.w * cropBoxStyle!.width / layout.displayW)} x {Math.round(naturalSize.h * cropBoxStyle!.height / layout.displayH)} px
            </span>
          )}
        </div>
        <div className="flex justify-between mt-4">
          <button
            onClick={() => { setX(0.5); setY(0.5); setScale(1); }}
            className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors"
          >
            {t('books.editor.resetCrop')}
          </button>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm text-slate-300 hover:text-white transition-colors"
            >
              {t('books.editor.closeModal')}
            </button>
            <button
              onClick={() => onSave(x, y, scale)}
              className="px-4 py-2 text-sm bg-rose-600 hover:bg-rose-700 text-white rounded transition-colors"
            >
              {t('books.editor.saveButton')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function TextSlotHistoryPanel({
  versions, loading, onRestore, t,
}: {
  versions: TextVersion[];
  loading: boolean;
  onRestore: (id: number) => void;
  t: (key: string) => string;
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

function getSlotContent(book: BookDetail, pageId: string, slotIndex: number): SlotContent {
  const page = book.pages.find(p => p.id === pageId);
  const slot = page?.slots.find(s => s.slot_index === slotIndex);
  return { photoUid: slot?.photo_uid || '', textContent: slot?.text_content || '' };
}

export function PagesTab({ book, setBook, sectionPhotos, loadSectionPhotos, onRefresh, initialPageId, onPageSelect }: Props) {
  const { t } = useTranslation('pages');
  const defaultPageId = initialPageId && book.pages.find(p => p.id === initialPageId)
    ? initialPageId
    : (book.pages.length > 0 ? book.pages[0].id : null);
  const [selectedId, setSelectedId] = useState<string | null>(defaultPageId);
  const [activePhotoUid, setActivePhotoUid] = useState<string | null>(null);
  const [activeTextContent, setActiveTextContent] = useState<string | null>(null);
  const [isPhotoDrag, setIsPhotoDrag] = useState(false);
  const [editingPhoto, setEditingPhoto] = useState<{ sectionId: string; photoUid: string } | null>(null);
  const [editingTextSlot, setEditingTextSlot] = useState<{ slotIndex: number; text: string; pageId: string } | null>(null);
  const [editingCrop, setEditingCrop] = useState<{ slotIndex: number; photoUid: string; cropX: number; cropY: number; cropScale: number; format: PageFormat; splitPosition?: number | null } | null>(null);
  const [minimapOpen, setMinimapOpen] = useState(() => {
    try { return localStorage.getItem(`book-minimap-${book.id}`) === 'true'; } catch { return false; }
  });
  const [autoLayoutLoading, setAutoLayoutLoading] = useState(false);
  const [autoLayoutMessage, setAutoLayoutMessage] = useState<string | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const { push: pushUndo } = useUndoRedo(onRefresh, !editingPhoto && !editingTextSlot && !editingCrop);

  const selectedPage = book.pages.find(p => p.id === selectedId);

  // Resolve chapter color for the selected page
  const selectedChapterColor = useMemo(() => {
    if (!selectedPage) return undefined;
    const section = book.sections.find(s => s.id === selectedPage.section_id);
    if (!section?.chapter_id) return undefined;
    const chapter = book.chapters.find(c => c.id === section.chapter_id);
    return chapter?.color || undefined;
  }, [selectedPage, book.sections, book.chapters]);

  // Persist minimap state
  useEffect(() => {
    try { localStorage.setItem(`book-minimap-${book.id}`, String(minimapOpen)); } catch { /* silent */ }
  }, [minimapOpen, book.id]);

  const toggleMinimap = useCallback(() => setMinimapOpen(v => !v), []);

  const handleAutoLayout = useCallback(async () => {
    if (!selectedPage?.section_id || autoLayoutLoading) return;
    setAutoLayoutLoading(true);
    setAutoLayoutMessage(null);
    try {
      const result = await autoLayoutSection(book.id, selectedPage.section_id);
      if (result.pages_created > 0) {
        setAutoLayoutMessage(t('books.editor.autoLayoutSuccess', { pages: result.pages_created, photos: result.photos_placed }));
        onRefresh();
      } else {
        setAutoLayoutMessage(t('books.editor.autoLayoutEmpty'));
      }
      setTimeout(() => setAutoLayoutMessage(null), 3000);
    } catch {
      setAutoLayoutMessage(t('books.editor.autoLayoutEmpty'));
      setTimeout(() => setAutoLayoutMessage(null), 3000);
    } finally {
      setAutoLayoutLoading(false);
    }
  }, [selectedPage, autoLayoutLoading, book.id, onRefresh, t]);

  // Enter: open edit dialog for the first filled slot on the selected page
  const handleEnterKey = useCallback(() => {
    if (!selectedPage) return;
    for (const slot of selectedPage.slots) {
      if (slot.text_content) {
        setEditingTextSlot({ slotIndex: slot.slot_index, text: slot.text_content, pageId: selectedPage.id });
        return;
      }
      if (slot.photo_uid && selectedPage.section_id) {
        setEditingPhoto({ sectionId: selectedPage.section_id, photoUid: slot.photo_uid });
        return;
      }
    }
  }, [selectedPage]);

  // Escape: deselect current page
  const handleEscapeKey = useCallback(() => {
    setSelectedId(null);
  }, []);

  // Keyboard navigation: W/S = prev/next page, E/D = prev/next section, Enter/Escape
  useBookKeyboardNav({
    items: book.pages,
    selectedId,
    onSelect: setSelectedId,
    getId: page => page.id,
    getChapterId: page => page.section_id || '',
    chapters: book.sections.map(s => ({ id: s.id })),
    enabled: !editingPhoto && !editingTextSlot && !editingCrop,
    onEnter: handleEnterKey,
    onEscape: handleEscapeKey,
  });

  // Load section photos for the selected page's section
  useEffect(() => {
    if (selectedPage?.section_id && !sectionPhotos[selectedPage.section_id]) {
      loadSectionPhotos(selectedPage.section_id);
    }
  }, [selectedPage, sectionPhotos, loadSectionPhotos]);

  // Also load all section photos on mount
  useEffect(() => {
    book.sections.forEach(s => {
      if (!sectionPhotos[s.id]) loadSectionPhotos(s.id);
    });
  }, [book.sections, sectionPhotos, loadSectionPhotos]);

  // Update selection if pages change
  useEffect(() => {
    if (selectedId && !book.pages.find(p => p.id === selectedId)) {
      setSelectedId(book.pages.length > 0 ? book.pages[0].id : null);
    }
  }, [book.pages, selectedId]);

  // Notify parent of page selection changes
  useEffect(() => {
    onPageSelect?.(selectedId);
  }, [selectedId, onPageSelect]);

  // Current section photos for selected page
  const currentSectionPhotos = useMemo(() => {
    if (!selectedPage?.section_id) return [];
    return sectionPhotos[selectedPage.section_id] || [];
  }, [selectedPage, sectionPhotos]);

  // Compute unassigned photos for the selected page's section
  const unassignedPhotos = useMemo(() => {
    if (!selectedPage?.section_id) return [];
    const photos = sectionPhotos[selectedPage.section_id] || [];
    // Gather all photo uids assigned to any page in this section
    const assignedUids = new Set<string>();
    book.pages.forEach(page => {
      if (page.section_id === selectedPage.section_id) {
        page.slots.forEach(s => {
          if (s.photo_uid) assignedUids.add(s.photo_uid);
        });
      }
    });
    return photos.filter(p => !assignedUids.has(p.photo_uid)).map(p => p.photo_uid);
  }, [selectedPage, sectionPhotos, book.pages]);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const data = event.active.data.current as Record<string, unknown> | undefined;
    if (data?.photoUid) {
      setActivePhotoUid(data.photoUid as string);
      setActiveTextContent(null);
      setIsPhotoDrag(true);
    } else if (data?.textContent) {
      setActiveTextContent(data.textContent as string);
      setActivePhotoUid(null);
      setIsPhotoDrag(true);
    } else {
      setIsPhotoDrag(false);
    }
  }, []);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    setActivePhotoUid(null);
    setActiveTextContent(null);
    setIsPhotoDrag(false);
    const { active, over } = event;
    if (!over) return;

    const activeData = active.data.current as Record<string, unknown> | undefined;
    const overData = over.data.current as Record<string, unknown> | undefined;
    if (!activeData || !overData) return;

    // Case 1: Page reorder (both active and over are page-reorder items)
    if (activeData.type === 'page-reorder' && overData.type === 'page-reorder') {
      if (active.id === over.id) return;
      const activePage = book.pages.find(p => p.id === activeData.pageId);
      const overPage = book.pages.find(p => p.id === overData.pageId);
      if (!activePage || !overPage) return;
      // Block cross-section drag
      if (activePage.section_id !== overPage.section_id) return;
      const oldIndex = book.pages.findIndex(p => p.id === activeData.pageId);
      const newIndex = book.pages.findIndex(p => p.id === overData.pageId);
      const reordered = arrayMove(book.pages, oldIndex, newIndex);
      try {
        await reorderPages(book.id, reordered.map(p => p.id));
        onRefresh();
      } catch { /* silent */ }
      return;
    }

    // Case 2 & 3: Photo/text drag operations
    const photoUid = activeData.photoUid as string | undefined;
    const dragTextContent = activeData.textContent as string | undefined;
    if (!photoUid && !dragTextContent) return;

    // Case 2: Photo dropped on a sidebar page
    if (overData.type === 'page-reorder') {
      if (!photoUid) return; // Only photo drags to sidebar pages
      const targetPageId = overData.pageId as string;
      const targetPage = book.pages.find(p => p.id === targetPageId);
      if (!targetPage) return;

      const totalSlots = pageFormatSlotCount(targetPage.format);
      // Check if photo already on target page
      if (targetPage.slots.some(s => s.photo_uid === photoUid)) return;
      // Find first empty slot
      const filledIndices = new Set(targetPage.slots.filter(s => s.photo_uid || s.text_content).map(s => s.slot_index));
      let emptySlotIndex = -1;
      for (let i = 0; i < totalSlots; i++) {
        if (!filledIndices.has(i)) { emptySlotIndex = i; break; }
      }
      if (emptySlotIndex === -1) return; // Page full

      const sourcePageId = activeData.sourcePageId as string | undefined;
      const sourceSlotIndex = activeData.sourceSlotIndex as number | undefined;
      const isFromSlot = sourcePageId !== undefined && sourceSlotIndex !== undefined;

      // Optimistic update
      setBook(prev => {
        if (!prev) return prev;
        const pages = prev.pages.map(p => ({ ...p, slots: p.slots.map(s => ({ ...s })) }));
        if (isFromSlot) {
          const srcPage = pages.find(p => p.id === sourcePageId);
          const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
          if (srcSlot) { srcSlot.photo_uid = ''; srcSlot.text_content = ''; }
        }
        const tgtPage = pages.find(p => p.id === targetPageId);
        if (tgtPage) {
          const tgtSlot = tgtPage.slots.find(s => s.slot_index === emptySlotIndex);
          if (tgtSlot) {
            tgtSlot.photo_uid = photoUid;
            tgtSlot.text_content = '';
          } else {
            tgtPage.slots.push({ slot_index: emptySlotIndex, photo_uid: photoUid, text_content: '', crop_x: 0.5, crop_y: 0.5, crop_scale: 1.0, title: '', file_name: '' });
          }
        }
        return { ...prev, pages };
      });

      try {
        const undoEntry: UndoEntry = [];
        if (isFromSlot) {
          undoEntry.push({ type: 'clear', pageId: sourcePageId, slotIndex: sourceSlotIndex, prev: getSlotContent(book, sourcePageId, sourceSlotIndex) });
          await clearSlot(sourcePageId, sourceSlotIndex);
        }
        undoEntry.push({ type: 'assign', pageId: targetPageId, slotIndex: emptySlotIndex, prev: { photoUid: '', textContent: '' }, next: { photoUid, textContent: '' } });
        await assignSlot(targetPageId, emptySlotIndex, photoUid);
        pushUndo(undoEntry);
        onRefresh();
      } catch {
        onRefresh();
      }
      return;
    }

    // Case 3: Photo/text dropped on a slot (existing logic)
    if (!selectedPage) return;

    const targetPageId = overData.pageId as string;
    const targetSlotIndex = overData.slotIndex as number;
    const targetPhotoUid = overData.photoUid as string | undefined;
    const targetTextContent = overData.textContent as string | undefined;
    const targetHasContent = !!targetPhotoUid || !!targetTextContent;

    // Check if dragging from a slot (has source slot info)
    const sourcePageId = activeData.sourcePageId as string | undefined;
    const sourceSlotIndex = activeData.sourceSlotIndex as number | undefined;
    const isFromSlot = sourcePageId !== undefined && sourceSlotIndex !== undefined;

    // Don't drop on the same slot
    if (isFromSlot && sourcePageId === targetPageId && sourceSlotIndex === targetSlotIndex) return;

    // Optimistic update: swap/move photos/text in local state immediately
    setBook(prev => {
      if (!prev) return prev;
      const pages = prev.pages.map(p => ({ ...p, slots: p.slots.map(s => ({ ...s })) }));
      if (isFromSlot && targetHasContent && sourcePageId === targetPageId) {
        // Same-page swap
        const page = pages.find(p => p.id === sourcePageId);
        if (page) {
          const srcSlot = page.slots.find(s => s.slot_index === sourceSlotIndex);
          const tgtSlot = page.slots.find(s => s.slot_index === targetSlotIndex);
          if (srcSlot && tgtSlot) {
            [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
            [srcSlot.text_content, tgtSlot.text_content] = [tgtSlot.text_content, srcSlot.text_content];
          }
        }
      } else if (isFromSlot && targetHasContent) {
        // Cross-page swap
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
          [srcSlot.text_content, tgtSlot.text_content] = [tgtSlot.text_content, srcSlot.text_content];
        }
      } else if (isFromSlot) {
        // Move to empty slot
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          tgtSlot.photo_uid = srcSlot.photo_uid;
          tgtSlot.text_content = srcSlot.text_content;
          srcSlot.photo_uid = '';
          srcSlot.text_content = '';
        }
      } else {
        // From unassigned pool (always photo)
        const tgtPage = pages.find(p => p.id === targetPageId);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (tgtSlot && photoUid) tgtSlot.photo_uid = photoUid;
      }
      return { ...prev, pages };
    });

    // Capture prev content before API calls for undo tracking
    const prevSource = isFromSlot ? getSlotContent(book, sourcePageId, sourceSlotIndex) : { photoUid: '', textContent: '' };
    const prevTarget = getSlotContent(book, targetPageId, targetSlotIndex);

    try {
      let undoEntry: UndoEntry;

      if (isFromSlot && targetHasContent && sourcePageId === targetPageId) {
        // Swap: both slots on the same page — atomic swap
        await swapSlots(sourcePageId, sourceSlotIndex, targetSlotIndex);
        undoEntry = [{ type: 'swap', pageId: sourcePageId, slotIndexA: sourceSlotIndex, slotIndexB: targetSlotIndex }];
      } else if (isFromSlot && targetHasContent) {
        // Swap across pages — assign each to the other's slot
        const assignments: Promise<void>[] = [];
        if (photoUid) {
          assignments.push(assignSlot(targetPageId, targetSlotIndex, photoUid));
        } else if (dragTextContent) {
          assignments.push(assignTextSlot(targetPageId, targetSlotIndex, dragTextContent));
        }
        if (targetPhotoUid) {
          assignments.push(assignSlot(sourcePageId, sourceSlotIndex, targetPhotoUid));
        } else if (targetTextContent) {
          assignments.push(assignTextSlot(sourcePageId, sourceSlotIndex, targetTextContent));
        }
        await Promise.all(assignments);
        undoEntry = [
          { type: 'assign', pageId: targetPageId, slotIndex: targetSlotIndex, prev: prevTarget, next: prevSource },
          { type: 'assign', pageId: sourcePageId, slotIndex: sourceSlotIndex, prev: prevSource, next: prevTarget },
        ];
      } else if (isFromSlot) {
        // Move: source slot has content, target is empty — clear old first to avoid unique constraint
        await clearSlot(sourcePageId, sourceSlotIndex);
        if (photoUid) {
          await assignSlot(targetPageId, targetSlotIndex, photoUid);
        } else if (dragTextContent) {
          await assignTextSlot(targetPageId, targetSlotIndex, dragTextContent);
        }
        undoEntry = [
          { type: 'clear', pageId: sourcePageId, slotIndex: sourceSlotIndex, prev: prevSource },
          { type: 'assign', pageId: targetPageId, slotIndex: targetSlotIndex, prev: { photoUid: '', textContent: '' }, next: prevSource },
        ];
      } else {
        // From unassigned pool — just assign
        if (photoUid) {
          await assignSlot(targetPageId, targetSlotIndex, photoUid);
        }
        undoEntry = [
          { type: 'assign', pageId: targetPageId, slotIndex: targetSlotIndex, prev: prevTarget, next: { photoUid: photoUid || '', textContent: '' } },
        ];
      }
      pushUndo(undoEntry);
      onRefresh();
    } catch {
      // Revert on error
      onRefresh();
    }
  }, [book, selectedPage, setBook, onRefresh, pushUndo]);

  const handleClearSlot = useCallback(async (slotIndex: number) => {
    if (!selectedPage) return;
    const prev = getSlotContent(book, selectedPage.id, slotIndex);
    try {
      await clearSlot(selectedPage.id, slotIndex);
      pushUndo([{ type: 'clear', pageId: selectedPage.id, slotIndex, prev }]);
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, book, pushUndo, onRefresh]);

  const handleEditDescription = useCallback((photoUid: string) => {
    if (!selectedPage?.section_id) return;
    setEditingPhoto({ sectionId: selectedPage.section_id, photoUid });
  }, [selectedPage]);

  const handleUpdatePageDescription = useCallback(async (desc: string) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { description: desc });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleChangeFormat = useCallback(async (format: PageFormat) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { format });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleChangeStyle = useCallback(async (style: PageStyle) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { style });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleDescSaved = useCallback(() => {
    if (editingPhoto) {
      loadSectionPhotos(editingPhoto.sectionId);
    }
    setEditingPhoto(null);
  }, [editingPhoto, loadSectionPhotos]);

  const handleAddText = useCallback((slotIndex: number) => {
    if (!selectedPage) return;
    setEditingTextSlot({ slotIndex, text: '', pageId: selectedPage.id });
  }, [selectedPage]);

  const handleEditText = useCallback((slotIndex: number) => {
    if (!selectedPage) return;
    const slot = selectedPage.slots.find(s => s.slot_index === slotIndex);
    setEditingTextSlot({ slotIndex, text: slot?.text_content || '', pageId: selectedPage.id });
  }, [selectedPage]);

  const handleSaveText = useCallback(async (text: string) => {
    if (!selectedPage || editingTextSlot === null) return;
    const prev = getSlotContent(book, selectedPage.id, editingTextSlot.slotIndex);
    try {
      await assignTextSlot(selectedPage.id, editingTextSlot.slotIndex, text);
      pushUndo([{ type: 'assign', pageId: selectedPage.id, slotIndex: editingTextSlot.slotIndex, prev, next: { photoUid: '', textContent: text } }]);
      onRefresh();
    } catch { /* silent */ }
    setEditingTextSlot(null);
  }, [selectedPage, editingTextSlot, book, pushUndo, onRefresh]);

  const handleEditCrop = useCallback((slotIndex: number) => {
    if (!selectedPage) return;
    const slot = selectedPage.slots.find(s => s.slot_index === slotIndex);
    if (!slot?.photo_uid) return;
    setEditingCrop({ slotIndex, photoUid: slot.photo_uid, cropX: slot.crop_x ?? 0.5, cropY: slot.crop_y ?? 0.5, cropScale: slot.crop_scale ?? 1.0, format: selectedPage.format, splitPosition: selectedPage.split_position });
  }, [selectedPage]);

  const handleSaveCrop = useCallback(async (cropX: number, cropY: number, cropScale: number) => {
    if (!selectedPage || !editingCrop) return;
    try {
      await updateSlotCrop(selectedPage.id, editingCrop.slotIndex, cropX, cropY, cropScale);
      onRefresh();
    } catch { /* silent */ }
    setEditingCrop(null);
  }, [selectedPage, editingCrop, onRefresh]);

  const handleChangeSplitPosition = useCallback(async (split: number | null) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { split_position: split });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleChangeHidePageNumber = useCallback(async (hide: boolean) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { hide_page_number: hide });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  if (book.pages.length === 0 && !selectedId) {
    return (
      <div className="flex gap-4">
        <PageSidebar
          bookId={book.id}
          pages={book.pages}
          chapters={book.chapters}
          sections={book.sections}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
        />
        <div className="flex-1 text-center text-slate-500 py-12">
          {t('books.editor.noPages')}
        </div>
      </div>
    );
  }

  // Find current editing photo data
  const editingPhotoData = editingPhoto
    ? currentSectionPhotos.find(sp => sp.photo_uid === editingPhoto.photoUid)
    : null;

  return (
    <DndContext sensors={sensors} collisionDetection={pointerWithin} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
      <div className="mb-2 flex items-center gap-2">
        <button
          onClick={toggleMinimap}
          className={`p-1.5 rounded transition-colors ${minimapOpen ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white hover:bg-slate-800'}`}
          title={t('books.editor.minimapToggle')}
        >
          <LayoutGrid className="h-4 w-4" />
        </button>
        {selectedPage?.section_id && unassignedPhotos.length > 0 && (
          <button
            onClick={handleAutoLayout}
            disabled={autoLayoutLoading}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded bg-slate-700 hover:bg-slate-600 text-slate-200 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {autoLayoutLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Wand2 className="h-4 w-4" />
            )}
            {t('books.editor.autoLayout')}
          </button>
        )}
        {autoLayoutMessage && (
          <span className="text-sm text-emerald-400 animate-pulse">{autoLayoutMessage}</span>
        )}
      </div>
      {minimapOpen && book.pages.length > 0 && (
        <div className="mb-3">
          <PageMinimap book={book} selectedId={selectedId} onSelect={setSelectedId} />
        </div>
      )}
      <div className="flex gap-4">
        <PageSidebar
          bookId={book.id}
          pages={book.pages}
          chapters={book.chapters}
          sections={book.sections}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
          isPhotoDragActive={isPhotoDrag}
        />
        <div className="flex-1 space-y-4">
          {selectedPage && (
            <>
              <PageTemplate
                page={selectedPage}
                onClearSlot={handleClearSlot}
                sectionPhotos={currentSectionPhotos}
                onEditDescription={handleEditDescription}
                onUpdatePageDescription={handleUpdatePageDescription}
                onChangeFormat={handleChangeFormat}
                onChangeStyle={handleChangeStyle}
                onEditText={handleEditText}
                onAddText={handleAddText}
                onEditCrop={handleEditCrop}
                onChangeSplitPosition={handleChangeSplitPosition}
                onChangeHidePageNumber={handleChangeHidePageNumber}
                chapterColor={selectedChapterColor}
              />
              <UnassignedPool
                photoUids={unassignedPhotos}
                sectionPhotos={currentSectionPhotos}
                onEditDescription={handleEditDescription}
              />
            </>
          )}
          <DragOverlay modifiers={[snapCenterToCursor]} dropAnimation={null}>
            {activePhotoUid && (
              <div className="w-16 h-16 rounded shadow-lg overflow-hidden opacity-80">
                <img
                  src={getThumbnailUrl(activePhotoUid, 'tile_100')}
                  alt=""
                  className="w-full h-full object-cover"
                />
              </div>
            )}
            {activeTextContent && (
              <div className="w-16 h-16 rounded shadow-lg overflow-hidden opacity-80 bg-slate-700 flex items-center justify-center">
                <Type className="h-6 w-6 text-slate-300" />
              </div>
            )}
          </DragOverlay>
        </div>

        {editingPhoto && editingPhotoData && (
          <PhotoDescriptionDialog
            sectionId={editingPhoto.sectionId}
            photoUid={editingPhoto.photoUid}
            description={editingPhotoData.description}
            note={editingPhotoData.note}
            onSaved={handleDescSaved}
            onClose={() => setEditingPhoto(null)}
          />
        )}

        {editingTextSlot !== null && (() => {
          const editPage = book.pages.find(p => p.id === editingTextSlot.pageId);
          const editSection = editPage ? book.sections.find(s => s.id === editPage.section_id) : undefined;
          const editChapter = editSection?.chapter_id ? book.chapters.find(c => c.id === editSection.chapter_id) : undefined;
          return (
            <TextSlotDialog
              text={editingTextSlot.text}
              pageId={editingTextSlot.pageId}
              slotIndex={editingTextSlot.slotIndex}
              pageFormat={editPage?.format ?? '1_fullscreen'}
              pageSlots={editPage?.slots ?? []}
              splitPosition={editPage?.split_position ?? null}
              chapterColor={editChapter?.color || undefined}
              onSave={handleSaveText}
              onClose={() => setEditingTextSlot(null)}
            />
          );
        })()}

        {editingCrop && (
          <CropDialog
            photoUid={editingCrop.photoUid}
            cropX={editingCrop.cropX}
            cropY={editingCrop.cropY}
            cropScale={editingCrop.cropScale}
            format={editingCrop.format}
            slotIndex={editingCrop.slotIndex}
            splitPosition={editingCrop.splitPosition}
            onSave={handleSaveCrop}
            onClose={() => setEditingCrop(null)}
          />
        )}
      </div>
    </DndContext>
  );
}
