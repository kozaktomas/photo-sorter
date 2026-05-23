import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { startUploadJob, cancelUploadJob } from '../../../api/client';
import { useSSE } from '../../../hooks/useSSE';
import { useToast } from '../../../components/Toast';
import type { NearDuplicatesEvent, UploadJobResult } from '../../../types';

type UploadPhase =
  | 'idle'
  | 'uploading'
  | 'processing'
  | 'detecting'
  | 'labels'
  | 'albums'
  | 'book'
  | 'embeddings'
  | 'completed'
  | 'failed'
  | 'cancelled';

interface UploadProgress {
  current: number;
  total: number;
  filename?: string;
}

interface UploadJobState {
  jobId: string | null;
  phase: UploadPhase;
  progress: UploadProgress | null;
  result: UploadJobResult | null;
  error: string | null;
  isStarting: boolean;
  nearDuplicates: NearDuplicatesEvent[];
}

export function useUploadJob() {
  const { t } = useTranslation('common');
  const toast = useToast();
  const [state, setState] = useState<UploadJobState>({
    jobId: null,
    phase: 'idle',
    progress: null,
    result: null,
    error: null,
    isStarting: false,
    nearDuplicates: [],
  });

  const sseUrl = state.jobId ? `/api/v1/upload/${state.jobId}/events` : null;

  const handleSSEMessage = useCallback((event: { type: string; data: unknown }) => {
    const eventData = event.data as Record<string, unknown> | null;

    switch (event.type) {
      case 'started':
        setState(prev => ({ ...prev, phase: 'uploading' }));
        break;

      case 'upload_progress': {
        const data = eventData?.data as { current?: number; total?: number; filename?: string } | undefined;
        if (data) {
          setState(prev => ({
            ...prev,
            phase: 'uploading',
            progress: {
              current: data.current ?? 0,
              total: data.total ?? 0,
              filename: data.filename,
            },
          }));
        }
        break;
      }

      case 'processing_upload': {
        const data = eventData?.data as { current?: number; total?: number } | undefined;
        setState(prev => ({
          ...prev,
          phase: 'processing',
          progress: data ? {
            current: data.current ?? 0,
            total: data.total ?? 0,
          } : prev.progress,
        }));
        break;
      }

      case 'detecting_photos':
        setState(prev => ({ ...prev, phase: 'detecting', progress: null }));
        break;

      case 'applying_labels': {
        const data = eventData?.data as { current?: number; total?: number } | undefined;
        if (data) {
          setState(prev => ({
            ...prev,
            phase: 'labels',
            progress: { current: data.current ?? 0, total: data.total ?? 0 },
          }));
        }
        break;
      }

      case 'applying_albums':
        setState(prev => ({ ...prev, phase: 'albums', progress: null }));
        break;

      case 'adding_to_book':
        setState(prev => ({ ...prev, phase: 'book', progress: null }));
        break;

      case 'process_progress': {
        const data = eventData?.data as { processed?: number; total?: number } | undefined;
        if (data) {
          setState(prev => ({
            ...prev,
            phase: 'embeddings',
            progress: { current: data.processed ?? 0, total: data.total ?? 0 },
          }));
        }
        break;
      }

      case 'near_duplicates': {
        // Payload shape: { filename, photo_uid, matches: [...] }. The
        // server only emits the event when matches is non-empty, but
        // we double-check defensively before queueing the user prompt.
        const data = eventData?.data as NearDuplicatesEvent | undefined;
        if (data?.photo_uid && data.matches?.length > 0) {
          setState(prev => ({
            ...prev,
            nearDuplicates: [...prev.nearDuplicates, data],
          }));
        }
        break;
      }

      case 'completed': {
        const result = eventData?.data as UploadJobResult | undefined;
        setState(prev => ({
          ...prev,
          phase: 'completed',
          result: result ?? null,
          progress: null,
        }));
        const count = result?.uploaded ?? 0;
        toast.success(t('toasts.jobs.uploadCompleted', { count }));
        break;
      }

      case 'job_error': {
        const message = (eventData?.message as string) || 'Unknown error';
        setState(prev => ({
          ...prev,
          phase: 'failed',
          error: message,
          progress: null,
        }));
        toast.error(t('toasts.jobs.uploadFailed', { message }));
        break;
      }

      case 'cancelled':
        setState(prev => ({ ...prev, phase: 'cancelled', progress: null }));
        toast.info(t('toasts.jobs.uploadCancelled'));
        break;
    }
  }, [toast, t]);

  useSSE(sseUrl, { onMessage: handleSSEMessage });

  const startUpload = useCallback(async (
    files: File[],
    config: {
      album_uids: string[];
      labels?: string[];
      book_section_id?: string;
      auto_process?: boolean;
    },
  ) => {
    setState(prev => ({ ...prev, isStarting: true, error: null, nearDuplicates: [] }));
    try {
      const response = await startUploadJob(files, config);
      setState({
        jobId: response.job_id,
        phase: 'uploading',
        progress: null,
        result: null,
        error: null,
        isStarting: false,
        nearDuplicates: [],
      });
      toast.info(t('toasts.jobs.uploadStarted', { count: files.length }));
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to start upload';
      setState(prev => ({
        ...prev,
        isStarting: false,
        phase: 'failed',
        error: message,
      }));
      toast.error(t('toasts.jobs.uploadFailed', { message }));
    }
  }, [toast, t]);

  const cancelUpload = useCallback(async () => {
    if (state.jobId) {
      try {
        await cancelUploadJob(state.jobId);
      } catch {
        // ignore
      }
    }
  }, [state.jobId]);

  const resetUpload = useCallback(() => {
    setState({
      jobId: null,
      phase: 'idle',
      progress: null,
      result: null,
      error: null,
      isStarting: false,
      nearDuplicates: [],
    });
  }, []);

  // Clears just the near-duplicate queue after the user has resolved it.
  // Leaves the rest of the upload state intact so the user can still see
  // the result summary.
  const clearNearDuplicates = useCallback(() => {
    setState(prev => ({ ...prev, nearDuplicates: [] }));
  }, []);

  const isRunning = ['uploading', 'processing', 'detecting', 'labels', 'albums', 'book', 'embeddings'].includes(state.phase);
  const isDone = ['completed', 'failed', 'cancelled'].includes(state.phase);

  return {
    ...state,
    isRunning,
    isDone,
    startUpload,
    cancelUpload,
    resetUpload,
    clearNearDuplicates,
  };
}
