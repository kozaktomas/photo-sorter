import { useState, useCallback } from 'react';
import { startSort, getSortJobStatus, cancelSortJob } from '../../../api/client';
import { useSSE } from '../../../hooks/useSSE';
import { parseSortJobEvent } from '../../../types/events';
import type { SortJob, SortJobOptions } from '../../../types';

export interface UseSortJobReturn {
  // State
  currentJob: SortJob | null;
  isStarting: boolean;
  // Actions
  startJob: (albumUid: string, options: Partial<SortJobOptions>) => Promise<void>;
  cancelJob: () => Promise<void>;
  resetJob: () => void;
  // Computed
  isJobRunning: boolean;
  isJobDone: boolean;
}

export function useSortJob(): UseSortJobReturn {
  const [currentJob, setCurrentJob] = useState<SortJob | null>(null);
  const [isStarting, setIsStarting] = useState(false);

  // SSE URL - only connect when we have an active job
  const sseUrl = currentJob?.id ? `/api/v1/sort/${currentJob.id}/events` : null;

  // Handle SSE messages
  const handleSSEMessage = useCallback((event: { type: string; data: unknown }) => {
    const parsed = parseSortJobEvent(event);
    if (!parsed) return;

    switch (parsed.type) {
      case 'status':
        setCurrentJob(parsed.data);
        break;
      case 'started':
        setCurrentJob((prev) => prev ? { ...prev, status: 'running' } : null);
        break;
      case 'photos_counted':
        setCurrentJob((prev) => prev ? { ...prev, total_photos: parsed.data.total } : null);
        break;
      case 'progress':
        setCurrentJob((prev) => prev ? {
          ...prev,
          processed_photos: parsed.data.processed_photos,
          total_photos: parsed.data.total_photos,
          status: 'running',
        } : null);
        break;
      case 'completed':
        setCurrentJob((prev) => prev ? { ...prev, status: 'completed', result: parsed.data } : null);
        break;
      case 'job_error':
        setCurrentJob((prev) => prev ? { ...prev, status: 'failed', error: parsed.message } : null);
        break;
      case 'cancelled':
        setCurrentJob((prev) => prev ? { ...prev, status: 'cancelled' } : null);
        break;
    }
  }, []);

  useSSE(sseUrl, { onMessage: handleSSEMessage });

  const startJob = useCallback(async (albumUid: string, options: Partial<SortJobOptions>) => {
    setIsStarting(true);
    try {
      const response = await startSort({
        album_uid: albumUid,
        dry_run: options.dry_run,
        limit: options.limit ?? undefined,
        individual_dates: options.individual_dates,
        batch_mode: options.batch_mode,
        provider: options.provider,
        force_date: options.force_date,
        concurrency: options.concurrency,
      });

      // Get initial job status
      const job = await getSortJobStatus(response.job_id);
      setCurrentJob(job);
    } catch (err) {
      console.error('Failed to start analysis:', err);
      throw err;
    } finally {
      setIsStarting(false);
    }
  }, []);

  const cancelJob = useCallback(async () => {
    if (!currentJob) return;
    try {
      await cancelSortJob(currentJob.id);
    } catch (err) {
      console.error('Failed to cancel job:', err);
    }
  }, [currentJob]);

  const resetJob = useCallback(() => {
    setCurrentJob(null);
  }, []);

  const isJobRunning = currentJob?.status === 'pending' || currentJob?.status === 'running';
  const isJobDone = currentJob?.status === 'completed' || currentJob?.status === 'failed' || currentJob?.status === 'cancelled';

  return {
    currentJob,
    isStarting,
    startJob,
    cancelJob,
    resetJob,
    isJobRunning,
    isJobDone,
  };
}
