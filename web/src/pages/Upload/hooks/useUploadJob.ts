import { useState, useCallback } from 'react';
import { startUploadJob, cancelUploadJob } from '../../../api/client';
import { useSSE } from '../../../hooks/useSSE';
import type { UploadJobResult } from '../../../types';

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
}

export function useUploadJob() {
  const [state, setState] = useState<UploadJobState>({
    jobId: null,
    phase: 'idle',
    progress: null,
    result: null,
    error: null,
    isStarting: false,
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

      case 'processing_upload':
        setState(prev => ({ ...prev, phase: 'processing', progress: null }));
        break;

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

      case 'completed': {
        const result = eventData?.data as UploadJobResult | undefined;
        setState(prev => ({
          ...prev,
          phase: 'completed',
          result: result ?? null,
          progress: null,
        }));
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
        break;
      }

      case 'cancelled':
        setState(prev => ({ ...prev, phase: 'cancelled', progress: null }));
        break;
    }
  }, []);

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
    setState(prev => ({ ...prev, isStarting: true, error: null }));
    try {
      const response = await startUploadJob(files, config);
      setState({
        jobId: response.job_id,
        phase: 'uploading',
        progress: null,
        result: null,
        error: null,
        isStarting: false,
      });
    } catch (err) {
      setState(prev => ({
        ...prev,
        isStarting: false,
        phase: 'failed',
        error: err instanceof Error ? err.message : 'Failed to start upload',
      }));
    }
  }, []);

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
    });
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
  };
}
