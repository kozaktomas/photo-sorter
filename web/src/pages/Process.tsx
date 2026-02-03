import { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Cpu, Play, Square, CheckCircle, XCircle, AlertCircle, Database, RefreshCw, RefreshCcw } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../components/Card';
import { Button } from '../components/Button';
import { FormInput } from '../components/FormInput';
import { FormCheckbox } from '../components/FormCheckbox';
import { getConfig, startProcess, cancelProcessJob, rebuildIndex, syncCache } from '../api/client';
import { useSSE } from '../hooks/useSSE';
import type { Config, ProcessJob, ProcessJobResult, RebuildIndexResponse, SyncCacheResponse } from '../types';

export function ProcessPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Form state
  const [concurrency, setConcurrency] = useState(5);
  const [limit, setLimit] = useState(0);
  const [noFaces, setNoFaces] = useState(false);
  const [noEmbeddings, setNoEmbeddings] = useState(false);

  // Job state
  const [currentJob, setCurrentJob] = useState<ProcessJob | null>(null);
  const [isStarting, setIsStarting] = useState(false);

  // Rebuild index state
  const [isRebuilding, setIsRebuilding] = useState(false);
  const [rebuildResult, setRebuildResult] = useState<RebuildIndexResponse | null>(null);
  const [rebuildError, setRebuildError] = useState<string | null>(null);

  // Sync cache state
  const [isSyncing, setIsSyncing] = useState(false);
  const [syncResult, setSyncResult] = useState<SyncCacheResponse | null>(null);
  const [syncError, setSyncError] = useState<string | null>(null);

  // SSE connection
  const sseUrl = currentJob?.id ? `/api/v1/process/${currentJob.id}/events` : null;

  const handleSSEMessage = useCallback((event: { type: string; data: unknown }) => {
    const eventData = event.data as Record<string, unknown> | null;

    if (event.type === 'status') {
      if (eventData) setCurrentJob(eventData as unknown as ProcessJob);
    } else if (event.type === 'started') {
      setCurrentJob((prev) => prev ? { ...prev, status: 'running' } : null);
    } else if (event.type === 'photos_counted') {
      const total = (eventData?.data as { total?: number })?.total ?? 0;
      setCurrentJob((prev) => prev ? { ...prev, total_photos: total } : null);
    } else if (event.type === 'filtering_done') {
      const data = eventData?.data as { to_process?: number; skipped?: number } | undefined;
      if (data) {
        setCurrentJob((prev) => prev ? {
          ...prev,
          total_photos: data.to_process ?? prev.total_photos,
          skipped_photos: data.skipped ?? prev.skipped_photos,
        } : null);
      }
    } else if (event.type === 'progress') {
      const progressData = eventData?.data as { processed?: number; total?: number } | undefined;
      if (progressData) {
        setCurrentJob((prev) => prev ? {
          ...prev,
          processed_photos: progressData.processed ?? prev.processed_photos,
          total_photos: progressData.total ?? prev.total_photos,
          status: 'running'
        } : null);
      }
    } else if (event.type === 'completed') {
      const result = eventData?.data as ProcessJobResult | undefined;
      setCurrentJob((prev) => prev ? { ...prev, status: 'completed', result } : null);
    } else if (event.type === 'job_error') {
      const message = (eventData?.message as string) || 'Unknown error';
      setCurrentJob((prev) => prev ? { ...prev, status: 'failed', error: message } : null);
    } else if (event.type === 'cancelled') {
      setCurrentJob((prev) => prev ? { ...prev, status: 'cancelled' } : null);
    }
  }, []);

  useSSE(sseUrl, {
    onMessage: handleSSEMessage,
  });

  useEffect(() => {
    async function loadData() {
      try {
        const configData = await getConfig();
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load config:', err);
      } finally {
        setIsLoading(false);
      }
    }
    loadData();
  }, []);

  const handleStart = async () => {
    setIsStarting(true);
    try {
      const response = await startProcess({
        concurrency,
        limit: limit || undefined,
        no_faces: noFaces || undefined,
        no_embeddings: noEmbeddings || undefined,
      });

      setCurrentJob({
        id: response.job_id,
        status: 'pending',
        total_photos: 0,
        processed_photos: 0,
        skipped_photos: 0,
        started_at: new Date().toISOString(),
        options: { concurrency, limit, no_faces: noFaces, no_embeddings: noEmbeddings },
      });
    } catch (err) {
      console.error('Failed to start processing:', err);
      alert(err instanceof Error ? err.message : 'Failed to start processing');
    } finally {
      setIsStarting(false);
    }
  };

  const handleCancel = async () => {
    if (!currentJob) return;
    try {
      await cancelProcessJob(currentJob.id);
    } catch (err) {
      console.error('Failed to cancel job:', err);
    }
  };

  const handleReset = () => {
    setCurrentJob(null);
  };

  const handleRebuildIndex = async () => {
    setIsRebuilding(true);
    setRebuildResult(null);
    setRebuildError(null);
    try {
      const result = await rebuildIndex();
      setRebuildResult(result);
    } catch (err) {
      console.error('Failed to rebuild index:', err);
      setRebuildError(err instanceof Error ? err.message : 'Failed to rebuild index');
    } finally {
      setIsRebuilding(false);
    }
  };

  const handleSyncCache = async () => {
    setIsSyncing(true);
    setSyncResult(null);
    setSyncError(null);
    try {
      const result = await syncCache();
      setSyncResult(result);
    } catch (err) {
      console.error('Failed to sync cache:', err);
      setSyncError(err instanceof Error ? err.message : 'Failed to sync cache');
    } finally {
      setIsSyncing(false);
    }
  };

  const isJobRunning = currentJob?.status === 'pending' || currentJob?.status === 'running';
  const isJobDone = currentJob?.status === 'completed' || currentJob?.status === 'failed' || currentJob?.status === 'cancelled';
  const isWritable = config?.embeddings_writable ?? false;

  if (isLoading) {
    return <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">{t('pages:process.title')}</h1>
        <p className="text-slate-400 mt-1">{t('pages:process.subtitle')}</p>
      </div>

      {!isWritable && (
        <div className="p-4 bg-yellow-500/10 border border-yellow-500/20 rounded-lg text-yellow-400">
          {t('pages:process.unavailableMessage')}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Configuration */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:process.configuration')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Options */}
            <div className="space-y-3">
              <FormCheckbox
                label={t('pages:process.skipFaceDetection')}
                checked={noFaces}
                onChange={(e) => setNoFaces(e.target.checked)}
                disabled={isJobRunning || !isWritable || noEmbeddings}
              />
              <FormCheckbox
                label={t('pages:process.skipImageEmbeddings')}
                checked={noEmbeddings}
                onChange={(e) => setNoEmbeddings(e.target.checked)}
                disabled={isJobRunning || !isWritable || noFaces}
              />
            </div>

            {/* Limit */}
            <FormInput
              label={t('pages:process.limit')}
              type="number"
              value={limit}
              onChange={(e) => setLimit(parseInt(e.target.value) || 0)}
              disabled={isJobRunning || !isWritable}
              min={0}
            />

            {/* Concurrency */}
            <FormInput
              label={t('pages:process.concurrency')}
              type="number"
              value={concurrency}
              onChange={(e) => setConcurrency(parseInt(e.target.value) || 5)}
              disabled={isJobRunning || !isWritable}
              min={1}
              max={20}
            />

            {/* Actions */}
            <div className="flex space-x-3 pt-4">
              {!currentJob && (
                <Button
                  onClick={handleStart}
                  disabled={!isWritable}
                  isLoading={isStarting}
                  className="flex-1"
                >
                  <Play className="h-4 w-4 mr-2" />
                  {t('pages:process.startProcessing')}
                </Button>
              )}

              {isJobRunning && (
                <Button variant="danger" onClick={handleCancel} className="flex-1">
                  <Square className="h-4 w-4 mr-2" />
                  {t('common:buttons.cancel')}
                </Button>
              )}

              {isJobDone && (
                <Button variant="secondary" onClick={handleReset} className="flex-1">
                  {t('common:buttons.startNew')}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Status */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:process.status')}</h2>
          </CardHeader>
          <CardContent>
            {!currentJob ? (
              <div className="text-center py-8 text-slate-400">
                <Cpu className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:process.configureAndStart')}</p>
              </div>
            ) : (
              <div className="space-y-4">
                {/* Status badge */}
                <div className="flex items-center space-x-2">
                  {currentJob.status === 'pending' && (
                    <span className="flex items-center text-yellow-400">
                      <AlertCircle className="h-4 w-4 mr-1" />
                      {t('common:status.fetchingPhotos')}
                    </span>
                  )}
                  {currentJob.status === 'running' && (
                    <span className="flex items-center text-blue-400">
                      <Cpu className="h-4 w-4 mr-1 animate-spin" />
                      {t('common:status.processing')}
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

                {/* Skipped count */}
                {currentJob.skipped_photos > 0 && (
                  <div className="text-sm text-slate-400">
                    {t('pages:process.skippedPhotos', { count: currentJob.skipped_photos })}
                  </div>
                )}

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
                    <h3 className="font-medium text-white">{t('pages:process.results')}</h3>

                    {!currentJob.options.no_embeddings && (
                      <div className="text-sm space-y-1">
                        <div className="text-slate-400 font-medium">{t('pages:process.embeddings')}</div>
                        <div className="grid grid-cols-2 gap-2">
                          <div>
                            <span className="text-green-400">{currentJob.result.embed_success}</span>
                            <span className="text-slate-500 ml-1">{t('pages:process.success')}</span>
                          </div>
                          {currentJob.result.embed_error > 0 && (
                            <div>
                              <span className="text-red-400">{currentJob.result.embed_error}</span>
                              <span className="text-slate-500 ml-1">{t('pages:process.errors')}</span>
                            </div>
                          )}
                        </div>
                        <div className="text-slate-500">
                          {t('pages:process.totalInDb')}: {currentJob.result.total_embeddings}
                        </div>
                      </div>
                    )}

                    {!currentJob.options.no_faces && (
                      <div className="text-sm space-y-1">
                        <div className="text-slate-400 font-medium">{t('pages:process.faces')}</div>
                        <div className="grid grid-cols-2 gap-2">
                          <div>
                            <span className="text-green-400">{currentJob.result.face_success}</span>
                            <span className="text-slate-500 ml-1">{t('common:units.photo', { count: currentJob.result.face_success })}</span>
                          </div>
                          {currentJob.result.face_error > 0 && (
                            <div>
                              <span className="text-red-400">{currentJob.result.face_error}</span>
                              <span className="text-slate-500 ml-1">{t('pages:process.errors')}</span>
                            </div>
                          )}
                        </div>
                        <div className="text-slate-500">
                          {t('pages:process.newFaces')}: {currentJob.result.total_new_faces}
                        </div>
                        <div className="text-slate-500">
                          {t('pages:process.totalInDb')}: {t('pages:process.photosWithFaces', { faces: currentJob.result.total_faces, photos: currentJob.result.total_face_photos })}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Rebuild HNSW Index */}
      <Card>
        <CardHeader>
          <div className="flex items-center space-x-2">
            <Database className="h-5 w-5 text-slate-400" />
            <h2 className="text-lg font-semibold text-white">{t('pages:process.rebuildIndex.title')}</h2>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-slate-400">
            {t('pages:process.rebuildIndex.description')}
          </p>

          <Button
            onClick={handleRebuildIndex}
            disabled={!isWritable || isRebuilding || isJobRunning}
            isLoading={isRebuilding}
            variant="secondary"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${isRebuilding ? 'animate-spin' : ''}`} />
            {t('pages:process.rebuildIndex.button')}
          </Button>

          {/* Success message */}
          {rebuildResult && (
            <div className="p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm space-y-1">
              <div className="flex items-center">
                <CheckCircle className="h-4 w-4 mr-2" />
                {t('pages:process.rebuildIndex.success')}
              </div>
              <div className="text-slate-400 pl-6 space-y-0.5">
                <div>{t('pages:process.rebuildIndex.facesIndexed', { count: rebuildResult.face_count })}</div>
                <div>{t('pages:process.rebuildIndex.embeddingsIndexed', { count: rebuildResult.embedding_count })}</div>
                <div>{t('pages:process.rebuildIndex.duration', { ms: rebuildResult.duration_ms })}</div>
              </div>
            </div>
          )}

          {/* Error message */}
          {rebuildError && (
            <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
              <div className="flex items-center">
                <XCircle className="h-4 w-4 mr-2" />
                {rebuildError}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Sync Cache */}
      <Card>
        <CardHeader>
          <div className="flex items-center space-x-2">
            <RefreshCcw className="h-5 w-5 text-slate-400" />
            <h2 className="text-lg font-semibold text-white">{t('pages:process.syncCache.title')}</h2>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-slate-400">
            {t('pages:process.syncCache.description')}
          </p>

          <Button
            onClick={handleSyncCache}
            disabled={!isWritable || isSyncing || isJobRunning}
            isLoading={isSyncing}
            variant="secondary"
          >
            <RefreshCcw className={`h-4 w-4 mr-2 ${isSyncing ? 'animate-spin' : ''}`} />
            {t('pages:process.syncCache.button')}
          </Button>

          {/* Success message */}
          {syncResult && (
            <div className="p-3 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm space-y-1">
              <div className="flex items-center">
                <CheckCircle className="h-4 w-4 mr-2" />
                {t('pages:process.syncCache.success')}
              </div>
              <div className="text-slate-400 pl-6 space-y-0.5">
                <div>{t('pages:process.syncCache.photosScanned', { count: syncResult.photos_scanned })}</div>
                <div>{t('pages:process.syncCache.facesUpdated', { count: syncResult.faces_updated })}</div>
                {syncResult.photos_deleted > 0 && (
                  <div>{t('pages:process.syncCache.photosDeleted', { count: syncResult.photos_deleted })}</div>
                )}
                <div>{t('pages:process.syncCache.duration', { ms: syncResult.duration_ms })}</div>
              </div>
            </div>
          )}

          {/* Error message */}
          {syncError && (
            <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
              <div className="flex items-center">
                <XCircle className="h-4 w-4 mr-2" />
                {syncError}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
