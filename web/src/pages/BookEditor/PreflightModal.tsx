import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Info, XCircle, ChevronDown, ChevronRight, Loader2, X } from 'lucide-react';
import type { PreflightResponse, PreflightIssue } from '../../types';
import type { PhotoQuality } from '../../api/client';

interface PreflightModalProps {
  data: PreflightResponse;
  loading: boolean;
  onExport: (photoQuality: PhotoQuality) => void;
  onClose: () => void;
  onGoToPage: (pageNumber: number) => void;
  photoQuality: PhotoQuality;
  onPhotoQualityChange: (q: PhotoQuality) => void;
}

function IssueIcon({ level }: { level: 'error' | 'warning' | 'info' }) {
  if (level === 'error') return <XCircle className="h-4 w-4 text-red-400 shrink-0" />;
  if (level === 'warning') return <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0" />;
  return <Info className="h-4 w-4 text-blue-400 shrink-0" />;
}

function IssueRow({ issue, level, onGoToPage, t }: {
  issue: PreflightIssue;
  level: 'error' | 'warning' | 'info';
  onGoToPage: (pageNumber: number) => void;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  const description = formatIssue(issue, t);

  return (
    <div className="flex items-center gap-2 py-1.5 px-2 text-sm text-slate-300">
      <IssueIcon level={level} />
      <span className="flex-1">{description}</span>
      {issue.page_number != null && issue.page_number > 0 && (
        <button
          onClick={() => onGoToPage(issue.page_number ?? 0)}
          className="text-xs text-rose-400 hover:text-rose-300 whitespace-nowrap"
        >
          {t('books.editor.preflight.goToPage')}
        </button>
      )}
    </div>
  );
}

function formatIssue(issue: PreflightIssue, t: (key: string, opts?: Record<string, unknown>) => string): string {
  switch (issue.type) {
    case 'empty_slot':
      return t('books.editor.preflight.emptySlot', {
        page: issue.page_number,
        slot: (issue.slot_index ?? 0) + 1,
      });
    case 'low_dpi':
      return t('books.editor.preflight.lowDpi', {
        page: issue.page_number,
        slot: (issue.slot_index ?? 0) + 1,
        photo: issue.photo_uid,
        dpi: issue.dpi,
      });
    case 'empty_section':
      return t('books.editor.preflight.emptySection', { section: issue.section });
    case 'unplaced_photos':
      return t('books.editor.preflight.unplacedPhotos', {
        count: issue.count,
        section: issue.section,
      });
    case 'missing_captions':
      return t('books.editor.preflight.missingCaptions', { count: issue.count });
    case 'original_downgrade':
      return t('books.editor.preflight.originalDowngrade', {
        photo: issue.photo_uid,
        longest: issue.longest_px,
      });
    default:
      return issue.type;
  }
}

function CollapsibleSection({ title, count, level, children }: {
  title: string;
  count: number;
  level: 'error' | 'warning' | 'info';
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(true);
  if (count === 0) return null;

  const colorMap = {
    error: 'text-red-400',
    warning: 'text-amber-400',
    info: 'text-blue-400',
  };

  return (
    <div className="mb-3">
      <button
        onClick={() => setOpen(!open)}
        className={`flex items-center gap-2 w-full text-left font-medium text-sm ${colorMap[level]}`}
      >
        {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        {title} ({count})
      </button>
      {open && <div className="ml-2 mt-1">{children}</div>}
    </div>
  );
}

export function PreflightModal({
  data,
  loading,
  onExport,
  onClose,
  onGoToPage,
  photoQuality,
  onPhotoQualityChange,
}: PreflightModalProps) {
  const { t } = useTranslation('pages');

  if (loading) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
        <div className="bg-slate-900 border border-slate-700 rounded-lg p-8 flex flex-col items-center gap-3">
          <Loader2 className="h-6 w-6 animate-spin text-rose-400" />
          <span className="text-slate-300 text-sm">{t('books.editor.preflight.checking')}</span>
        </div>
      </div>
    );
  }

  const { summary, errors, warnings, info } = data;
  const qualityHelpKey =
    photoQuality === 'low'
      ? 'books.editor.preflight.qualityHelpLow'
      : photoQuality === 'original'
        ? 'books.editor.preflight.qualityHelpOriginal'
        : 'books.editor.preflight.qualityHelpMedium';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="bg-slate-900 border border-slate-700 rounded-lg w-full max-w-lg max-h-[80vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-700">
          <h2 className="text-lg font-semibold text-white">{t('books.editor.preflight.title')}</h2>
          <button onClick={onClose} className="text-slate-400 hover:text-white">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Summary bar */}
        <div className="bg-slate-800 rounded-lg p-3 mx-4 mt-4 flex items-center gap-4 text-sm text-slate-300">
          <span>{t('books.editor.preflight.summaryPages', { count: summary.total_pages })}</span>
          <span className="text-slate-500">|</span>
          <span>{t('books.editor.preflight.summaryPhotos', { count: summary.total_photos })}</span>
          <span className="text-slate-500">|</span>
          <span>{t('books.editor.preflight.summarySlots', { filled: summary.filled_slots, total: summary.total_slots })}</span>
        </div>

        {/* Issues */}
        <div className="flex-1 overflow-y-auto px-4 py-3">
          <CollapsibleSection
            title={t('books.editor.preflight.errors')}
            count={errors.length}
            level="error"
          >
            {errors.map((issue, i) => (
              <IssueRow key={i} issue={issue} level="error" onGoToPage={onGoToPage} t={t} />
            ))}
          </CollapsibleSection>

          <CollapsibleSection
            title={t('books.editor.preflight.warnings')}
            count={warnings.length}
            level="warning"
          >
            {warnings.map((issue, i) => (
              <IssueRow key={i} issue={issue} level="warning" onGoToPage={onGoToPage} t={t} />
            ))}
          </CollapsibleSection>

          <CollapsibleSection
            title={t('books.editor.preflight.info')}
            count={info.length}
            level="info"
          >
            {info.map((issue, i) => (
              <IssueRow key={i} issue={issue} level="info" onGoToPage={onGoToPage} t={t} />
            ))}
          </CollapsibleSection>
        </div>

        {/* Footer: quality picker + buttons */}
        <div className="flex flex-col gap-3 px-4 py-3 border-t border-slate-700">
          <div className="flex flex-col gap-1">
            <label className="text-xs font-medium text-slate-400" htmlFor="preflight-quality">
              {t('books.editor.preflight.qualityLabel')}
            </label>
            <select
              id="preflight-quality"
              value={photoQuality}
              onChange={(e) => onPhotoQualityChange(e.target.value as PhotoQuality)}
              className="px-2 py-1.5 text-sm bg-slate-800 border border-slate-600 rounded text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
            >
              <option value="low">{t('books.editor.preflight.qualityLow')}</option>
              <option value="medium">{t('books.editor.preflight.qualityMedium')}</option>
              <option value="original">{t('books.editor.preflight.qualityOriginal')}</option>
            </select>
            <span className="text-xs text-slate-500">{t(qualityHelpKey)}</span>
          </div>
          <div className="flex items-center justify-end gap-3">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-slate-300 bg-slate-700 hover:bg-slate-600 rounded-lg transition-colors"
            >
              {t('books.editor.preflight.cancel')}
            </button>
            <button
              onClick={() => onExport(photoQuality)}
              className="px-4 py-2 text-sm font-medium text-white bg-rose-600 hover:bg-rose-500 rounded-lg transition-colors"
            >
              {t('books.editor.preflight.exportAnyway')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
