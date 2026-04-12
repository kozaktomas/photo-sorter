import { useTranslation } from 'react-i18next';
import { X } from 'lucide-react';
import type { BookExportJobState, ExportPhase } from './hooks/useBookExportJob';

interface Props {
  state: BookExportJobState;
  onCancel: () => void;
  onDismiss: () => void;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function formatElapsed(ms: number): string {
  const total = Math.floor(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${s.toString().padStart(2, '0')}`;
}

export function ExportProgressModal({ state, onCancel, onDismiss }: Props) {
  const { t } = useTranslation('pages');
  if (state.phase === 'idle') return null;

  const isTerminal =
    state.phase === 'done' || state.phase === 'error' || state.phase === 'cancelled';

  const generationPhases: ExportPhase[] = [
    'starting',
    'fetching_metadata',
    'downloading_photos',
    'compiling_pass1',
    'compiling_pass2',
  ];
  const inGeneration = generationPhases.includes(state.phase);

  const exportBarValue = (() => {
    if (state.phase === 'downloading_photos' && state.exportProgress && state.exportProgress.total > 0) {
      return (state.exportProgress.current / state.exportProgress.total) * 100;
    }
    if (state.phase === 'compiling_pass1') return 50;
    if (state.phase === 'compiling_pass2') return 75;
    if (state.phase === 'starting' || state.phase === 'fetching_metadata') return 5;
    if (!inGeneration) return 100;
    return 0;
  })();

  const exportIndeterminate =
    state.phase === 'compiling_pass1' || state.phase === 'compiling_pass2';

  const downloadBarValue = (() => {
    const dp = state.downloadProgress;
    if (!dp?.bytesTotal) return 0;
    return (dp.bytesLoaded / dp.bytesTotal) * 100;
  })();

  const phaseLabel = (() => {
    switch (state.phase) {
      case 'starting':
        return t('books.editor.exportModal.starting');
      case 'fetching_metadata':
        return t('books.editor.exportModal.fetchingMetadata');
      case 'downloading_photos':
        return t('books.editor.exportModal.downloadingPhotos', {
          current: state.exportProgress?.current ?? 0,
          total: state.exportProgress?.total ?? 0,
        });
      case 'compiling_pass1':
        return t('books.editor.exportModal.compilingPass1');
      case 'compiling_pass2':
        return t('books.editor.exportModal.compilingPass2');
      case 'downloading_file': {
        const dp = state.downloadProgress;
        if (!dp) return t('books.editor.exportModal.waiting');
        if (dp.bytesTotal !== null) {
          return t('books.editor.exportModal.downloadingFile', {
            loaded: formatBytes(dp.bytesLoaded),
            total: formatBytes(dp.bytesTotal),
          });
        }
        return t('books.editor.exportModal.downloadingFileUnknown', {
          loaded: formatBytes(dp.bytesLoaded),
        });
      }
      case 'done':
        return t('books.editor.exportModal.done');
      case 'error':
        return t('books.editor.exportModal.error');
      case 'cancelled':
        return t('books.editor.exportModal.cancelled');
      default:
        return '';
    }
  })();

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-md bg-slate-900 border border-slate-700 rounded-lg shadow-2xl p-6">
        <div className="flex items-start justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">
            {t('books.editor.exportModal.title')}
          </h2>
          {isTerminal && (
            <button
              onClick={onDismiss}
              className="text-slate-400 hover:text-white transition-colors"
              aria-label={t('books.editor.exportModal.close')}
            >
              <X className="h-5 w-5" />
            </button>
          )}
        </div>

        <p className="text-sm text-slate-300 mb-4 min-h-[1.25rem]">{phaseLabel}</p>

        {state.phase !== 'error' && state.phase !== 'cancelled' && (
          <>
            <div className="mb-3">
              <div className="w-full bg-slate-700 rounded-full h-2 overflow-hidden">
                {exportIndeterminate ? (
                  <div className="h-2 w-full bg-gradient-to-r from-emerald-600 via-emerald-400 to-emerald-600 animate-pulse" />
                ) : (
                  <div
                    className="bg-emerald-500 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${exportBarValue}%` }}
                  />
                )}
              </div>
            </div>

            {(state.phase === 'downloading_file' || state.phase === 'done') && (
              <div className="mb-3">
                <div className="w-full bg-slate-700 rounded-full h-2 overflow-hidden">
                  <div
                    className="bg-sky-500 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${downloadBarValue}%` }}
                  />
                </div>
              </div>
            )}
          </>
        )}

        {state.error && (
          <div className="mt-3 text-sm text-red-400 bg-red-900/20 border border-red-900/40 rounded p-2">
            {state.error}
          </div>
        )}

        <div className="mt-4 flex items-center justify-between text-xs text-slate-500">
          <span>{t('books.editor.exportModal.elapsed', { time: formatElapsed(state.elapsedMs) })}</span>
          {!isTerminal && (
            <button
              onClick={onCancel}
              className="px-3 py-1 bg-slate-700 hover:bg-slate-600 text-white rounded transition-colors"
            >
              {t('books.editor.exportModal.cancel')}
            </button>
          )}
          {isTerminal && (
            <button
              onClick={onDismiss}
              className="px-3 py-1 bg-slate-700 hover:bg-slate-600 text-white rounded transition-colors"
            >
              {t('books.editor.exportModal.close')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
