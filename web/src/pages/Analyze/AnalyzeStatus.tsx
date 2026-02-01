import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Sparkles, CheckCircle, XCircle, AlertCircle, Square, FolderOpen } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { USD_TO_CZK } from '../../constants';
import type { SortJob } from '../../types';

interface AnalyzeStatusProps {
  currentJob: SortJob | null;
  selectedAlbum: string;
}

export function AnalyzeStatus({ currentJob, selectedAlbum }: AnalyzeStatusProps) {
  const { t } = useTranslation(['pages', 'common']);

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('pages:analyze.status')}</h2>
      </CardHeader>
      <CardContent>
        {!currentJob ? (
          <div className="text-center py-8 text-slate-400">
            <Sparkles className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>{t('pages:analyze.selectAlbumToAnalyze')}</p>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Job info */}
            <div className="flex items-center space-x-3">
              <FolderOpen className="h-5 w-5 text-slate-400" />
              <span className="text-white font-medium">{currentJob.album_title}</span>
            </div>

            {/* Status badge */}
            <div className="flex items-center space-x-2">
              {currentJob.status === 'pending' && (
                <span className="flex items-center text-yellow-400">
                  <AlertCircle className="h-4 w-4 mr-1" />
                  {t('common:status.pending')}
                </span>
              )}
              {currentJob.status === 'running' && (
                <span className="flex items-center text-blue-400">
                  <Sparkles className="h-4 w-4 mr-1 animate-spin" />
                  {t('common:status.running')}
                </span>
              )}
              {currentJob.status === 'completed' && (
                <span className="flex items-center text-green-400">
                  <CheckCircle className="h-4 w-4 mr-1" />
                  {t('common:status.completed')}
                </span>
              )}
              {currentJob.status === 'failed' && (
                <span className="flex items-center text-red-400">
                  <XCircle className="h-4 w-4 mr-1" />
                  {t('common:status.failed')}
                </span>
              )}
              {currentJob.status === 'cancelled' && (
                <span className="flex items-center text-slate-400">
                  <Square className="h-4 w-4 mr-1" />
                  {t('common:status.cancelled')}
                </span>
              )}
            </div>

            {/* Progress bar */}
            {(currentJob.status === 'running' || currentJob.status === 'pending') && currentJob.total_photos > 0 && (
              <div>
                <div className="flex justify-between text-sm text-slate-400 mb-1">
                  <span>{t('pages:analyze.progress')}</span>
                  <span>{currentJob.processed_photos} / {currentJob.total_photos}</span>
                </div>
                <div className="w-full bg-slate-700 rounded-full h-2">
                  <div
                    className="bg-blue-500 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${(currentJob.processed_photos / currentJob.total_photos) * 100}%` }}
                  />
                </div>
              </div>
            )}

            {/* Error message */}
            {currentJob.error && (
              <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
                {currentJob.error}
              </div>
            )}

            {/* Results */}
            {currentJob.result && (
              <div className="space-y-3 pt-4 border-t border-slate-700">
                <h3 className="font-medium text-white">{t('pages:analyze.results')}</h3>
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div>
                    <span className="text-slate-400">{t('pages:analyze.processedCount')}:</span>
                    <span className="text-white ml-2">{currentJob.result.processed_count}</span>
                  </div>
                  <div>
                    <span className="text-slate-400">{t('pages:analyze.updatedCount')}:</span>
                    <span className="text-white ml-2">{currentJob.result.sorted_count}</span>
                  </div>
                </div>

                {currentJob.result.album_date && (
                  <div className="text-sm">
                    <span className="text-slate-400">{t('pages:analyze.albumDate')}:</span>
                    <span className="text-white ml-2">{currentJob.result.album_date}</span>
                  </div>
                )}

                {currentJob.result.usage && (
                  <div className="text-sm space-y-1">
                    <div>
                      <span className="text-slate-400">{t('pages:analyze.tokens')}:</span>
                      <span className="text-white ml-2">
                        {currentJob.result.usage.input_tokens.toLocaleString()} in / {currentJob.result.usage.output_tokens.toLocaleString()} out
                      </span>
                    </div>
                    <div>
                      <span className="text-slate-400">{t('pages:analyze.cost')}:</span>
                      <span className="text-white ml-2">
                        ${currentJob.result.usage.total_cost.toFixed(4)} ({(currentJob.result.usage.total_cost * USD_TO_CZK).toFixed(2)} Kc)
                      </span>
                    </div>
                  </div>
                )}

                {currentJob.result.errors && currentJob.result.errors.length > 0 && (
                  <div className="mt-3">
                    <p className="text-red-400 text-sm mb-2">
                      {t('common:units.error', { count: currentJob.result.errors.length })}:
                    </p>
                    <ul className="text-xs text-slate-400 space-y-1 max-h-32 overflow-y-auto">
                      {currentJob.result.errors.slice(0, 10).map((err, i) => (
                        <li key={i} className="truncate">{err}</li>
                      ))}
                      {currentJob.result.errors.length > 10 && (
                        <li>...and {currentJob.result.errors.length - 10} more</li>
                      )}
                    </ul>
                  </div>
                )}

                {selectedAlbum && (
                  <div className="pt-3">
                    <Link to={`/albums/${selectedAlbum}`}>
                      <Button variant="secondary" size="sm">
                        {t('common:buttons.viewAlbum')}
                      </Button>
                    </Link>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
