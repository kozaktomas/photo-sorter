import type { SortJob, SortJobResult, ProcessJob, ProcessJobResult } from './index';

// Sort job SSE events
export type SortJobEvent =
  | { type: 'status'; data: SortJob }
  | { type: 'started'; data: null }
  | { type: 'photos_counted'; data: { total: number } }
  | { type: 'progress'; data: { processed_photos: number; total_photos: number } }
  | { type: 'completed'; data: SortJobResult }
  | { type: 'job_error'; message: string }
  | { type: 'cancelled'; data: null };

// Process job SSE events
export type ProcessJobEvent =
  | { type: 'status'; data: ProcessJob }
  | { type: 'started'; data: null }
  | { type: 'photos_counted'; data: { total: number } }
  | { type: 'filtering_done'; data: { to_process: number; skipped: number } }
  | { type: 'progress'; data: { processed: number; total: number } }
  | { type: 'completed'; data: ProcessJobResult }
  | { type: 'job_error'; message: string }
  | { type: 'cancelled'; data: null };

// Raw SSE message from useSSE hook
export interface RawSSEMessage {
  type: string;
  data: unknown;
}

// Type guard to check if raw SSE message is a SortJobEvent
export function isSortJobEvent(event: RawSSEMessage): event is { type: SortJobEvent['type']; data: unknown } {
  return ['status', 'started', 'photos_counted', 'progress', 'completed', 'job_error', 'cancelled'].includes(event.type);
}

// Type guard to check if raw SSE message is a ProcessJobEvent
export function isProcessJobEvent(event: RawSSEMessage): event is { type: ProcessJobEvent['type']; data: unknown } {
  return ['status', 'started', 'photos_counted', 'filtering_done', 'progress', 'completed', 'job_error', 'cancelled'].includes(event.type);
}

// Helper to safely parse sort job events
export function parseSortJobEvent(event: RawSSEMessage): SortJobEvent | null {
  const { type, data } = event;
  const eventData = data as Record<string, unknown> | null;

  switch (type) {
    case 'status':
      return eventData ? { type: 'status', data: eventData as unknown as SortJob } : null;
    case 'started':
      return { type: 'started', data: null };
    case 'photos_counted': {
      const total = (eventData?.data as { total?: number })?.total ?? 0;
      return { type: 'photos_counted', data: { total } };
    }
    case 'progress': {
      const progressData = eventData?.data as { processed_photos?: number; total_photos?: number } | undefined;
      if (progressData) {
        return {
          type: 'progress',
          data: {
            processed_photos: progressData.processed_photos ?? 0,
            total_photos: progressData.total_photos ?? 0,
          },
        };
      }
      return null;
    }
    case 'completed': {
      const result = eventData?.data as SortJobResult | undefined;
      return result ? { type: 'completed', data: result } : null;
    }
    case 'job_error': {
      const message = (eventData?.message as string) || 'Unknown error';
      return { type: 'job_error', message };
    }
    case 'cancelled':
      return { type: 'cancelled', data: null };
    default:
      return null;
  }
}

// Helper to safely parse process job events
export function parseProcessJobEvent(event: RawSSEMessage): ProcessJobEvent | null {
  const { type, data } = event;
  const eventData = data as Record<string, unknown> | null;

  switch (type) {
    case 'status':
      return eventData ? { type: 'status', data: eventData as unknown as ProcessJob } : null;
    case 'started':
      return { type: 'started', data: null };
    case 'photos_counted': {
      const total = (eventData?.data as { total?: number })?.total ?? 0;
      return { type: 'photos_counted', data: { total } };
    }
    case 'filtering_done': {
      const filterData = eventData?.data as { to_process?: number; skipped?: number } | undefined;
      if (filterData) {
        return {
          type: 'filtering_done',
          data: {
            to_process: filterData.to_process ?? 0,
            skipped: filterData.skipped ?? 0,
          },
        };
      }
      return null;
    }
    case 'progress': {
      const progressData = eventData?.data as { processed?: number; total?: number } | undefined;
      if (progressData) {
        return {
          type: 'progress',
          data: {
            processed: progressData.processed ?? 0,
            total: progressData.total ?? 0,
          },
        };
      }
      return null;
    }
    case 'completed': {
      const result = eventData?.data as ProcessJobResult | undefined;
      return result ? { type: 'completed', data: result } : null;
    }
    case 'job_error': {
      const message = (eventData?.message as string) || 'Unknown error';
      return { type: 'job_error', message };
    }
    case 'cancelled':
      return { type: 'cancelled', data: null };
    default:
      return null;
  }
}
