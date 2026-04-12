import { useCallback, useEffect, useRef, useState } from 'react';
import {
  startBookExportJob,
  cancelBookExportJob,
  getBookExportJobEventsUrl,
  getBookExportJobDownloadUrl,
} from '../../../api/client';
import { useSSE } from '../../../hooks/useSSE';

export type ExportPhase =
  | 'idle'
  | 'starting'
  | 'fetching_metadata'
  | 'downloading_photos'
  | 'compiling_pass1'
  | 'compiling_pass2'
  | 'downloading_file'
  | 'done'
  | 'error'
  | 'cancelled';

interface ExportProgress {
  phase: ExportPhase;
  current: number;
  total: number;
}

interface DownloadProgress {
  bytesLoaded: number;
  bytesTotal: number | null;
}

export interface BookExportJobState {
  jobId: string | null;
  phase: ExportPhase;
  exportProgress: ExportProgress | null;
  downloadProgress: DownloadProgress | null;
  elapsedMs: number;
  filename: string | null;
  fileSize: number | null;
  error: string | null;
}

const INITIAL_STATE: BookExportJobState = {
  jobId: null,
  phase: 'idle',
  exportProgress: null,
  downloadProgress: null,
  elapsedMs: 0,
  filename: null,
  fileSize: null,
  error: null,
};

const KNOWN_PHASES: readonly ExportPhase[] = [
  'fetching_metadata',
  'downloading_photos',
  'compiling_pass1',
  'compiling_pass2',
];

function isKnownPhase(p: string): p is ExportPhase {
  return (KNOWN_PHASES as readonly string[]).includes(p);
}

// Triggers a browser save dialog for the given blob with the suggested
// filename. Lifted from the existing exportBookPDF flow.
function triggerBlobDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function useBookExportJob() {
  const [state, setState] = useState<BookExportJobState>(INITIAL_STATE);
  const abortRef = useRef<AbortController | null>(null);
  const lastProgressFlushRef = useRef<number>(0);
  const startedAtRef = useRef<number | null>(null);
  const timerRef = useRef<number | null>(null);

  // Keep the elapsed-time counter ticking while the job is active.
  useEffect(() => {
    const active =
      state.phase !== 'idle' &&
      state.phase !== 'done' &&
      state.phase !== 'error' &&
      state.phase !== 'cancelled';
    if (!active) {
      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
      return;
    }
    if (timerRef.current === null && startedAtRef.current !== null) {
      timerRef.current = window.setInterval(() => {
        if (startedAtRef.current !== null) {
          setState(prev => ({ ...prev, elapsedMs: Date.now() - startedAtRef.current! }));
        }
      }, 500);
    }
    return () => {
      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [state.phase]);

  const sseUrl = state.jobId ? getBookExportJobEventsUrl(state.jobId) : null;

  const downloadFile = useCallback(
    async (downloadUrl: string, filename: string) => {
      const controller = new AbortController();
      abortRef.current?.abort();
      abortRef.current = controller;

      try {
        const res = await fetch(downloadUrl, {
          credentials: 'include',
          signal: controller.signal,
        });
        if (!res.ok || !res.body) {
          throw new Error(`Download failed: HTTP ${res.status}`);
        }
        const totalStr = res.headers.get('Content-Length');
        const total = totalStr ? parseInt(totalStr, 10) : null;

        setState(prev => ({
          ...prev,
          phase: 'downloading_file',
          downloadProgress: { bytesLoaded: 0, bytesTotal: total },
        }));

        const reader = res.body.getReader();
        const chunks: Uint8Array[] = [];
        let loaded = 0;
        lastProgressFlushRef.current = 0;

        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          if (value) {
            chunks.push(value);
            loaded += value.length;
            const now = Date.now();
            // Throttle React state updates: at most every 100ms. 700 MB
            // produces thousands of chunks; an un-throttled setState loop
            // tanks the UI thread.
            if (now - lastProgressFlushRef.current >= 100) {
              lastProgressFlushRef.current = now;
              const snapshot = loaded;
              setState(prev => ({
                ...prev,
                downloadProgress: { bytesLoaded: snapshot, bytesTotal: total },
              }));
            }
          }
        }

        // Flush the final count so the bar always lands at 100%.
        setState(prev => ({
          ...prev,
          downloadProgress: { bytesLoaded: loaded, bytesTotal: total ?? loaded },
        }));

        const blob = new Blob(chunks as BlobPart[], { type: 'application/pdf' });
        triggerBlobDownload(blob, filename);

        setState(prev => ({ ...prev, phase: 'done' }));
      } catch (err) {
        if ((err as Error)?.name === 'AbortError') {
          return;
        }
        setState(prev => ({
          ...prev,
          phase: 'error',
          error: err instanceof Error ? err.message : 'Download failed',
        }));
      } finally {
        abortRef.current = null;
      }
    },
    [],
  );

  const handleSSEMessage = useCallback(
    (event: { type: string; data: unknown }) => {
      const eventData = event.data as Record<string, unknown> | null;

      switch (event.type) {
        case 'started':
          setState(prev => ({ ...prev, phase: 'fetching_metadata' }));
          break;

        case 'progress': {
          const data = eventData?.data as
            | { phase?: string; current?: number; total?: number }
            | undefined;
          if (!data) return;
          const phase: ExportPhase = isKnownPhase(data.phase ?? '')
            ? (data.phase as ExportPhase)
            : 'fetching_metadata';
          setState(prev => ({
            ...prev,
            phase,
            exportProgress: {
              phase,
              current: data.current ?? 0,
              total: data.total ?? 0,
            },
          }));
          break;
        }

        case 'completed': {
          const data = eventData?.data as
            | { job_id?: string; filename?: string; file_size?: number; download_url?: string }
            | undefined;
          if (!data?.download_url) return;
          const filename = data.filename ?? 'book.pdf';
          setState(prev => ({
            ...prev,
            filename,
            fileSize: data.file_size ?? null,
          }));
          void downloadFile(data.download_url, filename);
          break;
        }

        case 'job_error': {
          const message = (eventData?.message as string) || 'Export failed';
          setState(prev => ({ ...prev, phase: 'error', error: message }));
          break;
        }

        case 'cancelled':
          setState(prev => ({ ...prev, phase: 'cancelled' }));
          break;
      }
    },
    [downloadFile],
  );

  useSSE(sseUrl, { onMessage: handleSSEMessage });

  const start = useCallback(async (bookId: string) => {
    startedAtRef.current = Date.now();
    setState({ ...INITIAL_STATE, phase: 'starting', elapsedMs: 0 });
    try {
      const { jobId } = await startBookExportJob(bookId);
      setState(prev => ({ ...prev, jobId, phase: 'fetching_metadata' }));
    } catch (err) {
      setState(prev => ({
        ...prev,
        phase: 'error',
        error: err instanceof Error ? err.message : 'Failed to start export',
      }));
    }
  }, []);

  const cancel = useCallback(async () => {
    abortRef.current?.abort();
    abortRef.current = null;
    const jobId = state.jobId;
    if (jobId) {
      try {
        await cancelBookExportJob(jobId);
      } catch {
        // Ignore — the UI will transition via the SSE 'cancelled' event.
      }
    }
    setState(prev => ({ ...prev, phase: 'cancelled' }));
  }, [state.jobId]);

  const reset = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    startedAtRef.current = null;
    setState(INITIAL_STATE);
  }, []);

  // Close the abort controller on unmount so an in-flight chunked download
  // doesn't leak after the user navigates away mid-export.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, []);

  const isActive =
    state.phase !== 'idle' &&
    state.phase !== 'done' &&
    state.phase !== 'error' &&
    state.phase !== 'cancelled';

  return {
    state,
    isActive,
    start,
    cancel,
    reset,
    getDownloadUrl: (jobId: string) => getBookExportJobDownloadUrl(jobId),
  };
}
