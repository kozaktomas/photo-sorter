import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Trash2, X } from 'lucide-react';
import { Button } from '../../components/Button';
import { archivePhotos, getThumbnailUrl } from '../../api/client';
import type { NearDuplicatesEvent } from '../../types';

type Decision = 'keep' | 'delete';

interface NearDuplicatesModalProps {
  events: NearDuplicatesEvent[];
  onClose: () => void;
}

export function NearDuplicatesModal({ events, onClose }: NearDuplicatesModalProps) {
  const { t } = useTranslation(['pages', 'common']);

  // Per just-uploaded photo decision. Default to "keep" (i.e. don't touch
  // the new copy) which matches the server-side default behaviour.
  const [decisions, setDecisions] = useState<Record<string, Decision>>(() =>
    Object.fromEntries(events.map((ev) => [ev.photo_uid, 'keep' as Decision])),
  );
  const [isApplying, setIsApplying] = useState(false);
  const [applyError, setApplyError] = useState<string | null>(null);
  const [doneMessage, setDoneMessage] = useState<string | null>(null);

  if (events.length === 0) return null;

  const toggle = (uid: string) => {
    setDecisions((prev) => ({
      ...prev,
      [uid]: prev[uid] === 'delete' ? 'keep' : 'delete',
    }));
  };

  const handleApply = async () => {
    const toArchive = Object.entries(decisions)
      .filter(([, d]) => d === 'delete')
      .map(([uid]) => uid);

    if (toArchive.length === 0) {
      onClose();
      return;
    }

    setIsApplying(true);
    setApplyError(null);
    try {
      await archivePhotos(toArchive);
      setDoneMessage(t('pages:nearDuplicates.deletedSummary', { count: toArchive.length }));
      // Brief pause so the user sees the confirmation before the modal closes.
      setTimeout(onClose, 1200);
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : 'Failed to archive duplicates');
    } finally {
      setIsApplying(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="near-duplicates-title"
    >
      <div className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl max-w-4xl w-full max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-start justify-between p-5 border-b border-slate-700 shrink-0">
          <div className="flex items-start gap-3">
            <AlertTriangle className="h-6 w-6 text-amber-400 mt-1" />
            <div>
              <h2 id="near-duplicates-title" className="text-lg font-semibold text-white">
                {t('pages:nearDuplicates.title')}
              </h2>
              <p className="text-sm text-slate-400 mt-1">
                {t('pages:nearDuplicates.intro', { count: events.length })}
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-white transition-colors"
            aria-label={t('common:buttons.cancel')}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body — scrollable list of duplicate pairs */}
        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          {events.map((ev) => {
            const decision = decisions[ev.photo_uid] ?? 'keep';
            return (
              <div
                key={ev.photo_uid}
                className={`p-4 rounded-lg border ${
                  decision === 'delete'
                    ? 'bg-red-500/10 border-red-500/30'
                    : 'bg-slate-900 border-slate-700'
                }`}
              >
                <div className="flex gap-4">
                  {/* Just-uploaded photo */}
                  <div className="flex-1 min-w-0">
                    <p className="text-xs text-slate-400 mb-2">
                      {t('pages:nearDuplicates.yourUpload')}
                    </p>
                    <a
                      href={`/photos/${ev.photo_uid}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="block aspect-square w-32 rounded-md overflow-hidden bg-slate-700 hover:ring-2 hover:ring-amber-400 transition-all"
                    >
                      <img
                        src={getThumbnailUrl(ev.photo_uid, 'tile_224')}
                        alt={ev.filename}
                        className="w-full h-full object-cover"
                        loading="lazy"
                      />
                    </a>
                    <p className="text-xs text-slate-300 mt-2 truncate" title={ev.filename}>
                      {ev.filename}
                    </p>
                  </div>

                  {/* Existing matches */}
                  <div className="flex-1 min-w-0">
                    <p className="text-xs text-slate-400 mb-2">
                      {t('pages:nearDuplicates.existing')}
                    </p>
                    <div className="flex flex-wrap gap-2">
                      {ev.matches.slice(0, 4).map((m) => (
                        <a
                          key={m.photo_uid}
                          href={`/photos/${m.photo_uid}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="block aspect-square w-32 rounded-md overflow-hidden bg-slate-700 hover:ring-2 hover:ring-blue-400 transition-all"
                          title={[
                            m.file_name,
                            t('pages:nearDuplicates.scoreHash', { value: m.score_phash }),
                            m.score_embedding > 0
                              ? t('pages:nearDuplicates.scoreEmbedding', { value: m.score_embedding.toFixed(3) })
                              : null,
                          ].filter(Boolean).join('\n')}
                        >
                          <img
                            src={getThumbnailUrl(m.photo_uid, 'tile_224')}
                            alt={m.file_name}
                            className="w-full h-full object-cover"
                            loading="lazy"
                          />
                        </a>
                      ))}
                    </div>
                  </div>
                </div>

                {/* Per-pair decision */}
                <div className="mt-4 flex items-center justify-end gap-2">
                  <Button
                    size="sm"
                    variant={decision === 'keep' ? 'primary' : 'secondary'}
                    onClick={() => setDecisions((prev) => ({ ...prev, [ev.photo_uid]: 'keep' }))}
                    disabled={isApplying}
                  >
                    {t('pages:nearDuplicates.keepBoth')}
                  </Button>
                  <Button
                    size="sm"
                    variant={decision === 'delete' ? 'danger' : 'secondary'}
                    onClick={() => toggle(ev.photo_uid)}
                    disabled={isApplying}
                  >
                    <Trash2 className="h-3.5 w-3.5 mr-1" />
                    {t('pages:nearDuplicates.deleteUploaded')}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>

        {/* Footer */}
        <div className="p-4 border-t border-slate-700 flex items-center justify-between shrink-0">
          <div className="text-sm">
            {doneMessage && <span className="text-emerald-400">{doneMessage}</span>}
            {applyError && <span className="text-red-400">{applyError}</span>}
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={onClose} disabled={isApplying}>
              {t('pages:nearDuplicates.close')}
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleApply}
              disabled={isApplying}
              isLoading={isApplying}
            >
              {t('common:buttons.save')}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
