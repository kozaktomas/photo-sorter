import { useState, useEffect, useCallback, useRef } from 'react';

interface SSEOptions {
  onMessage?: (event: { type: string; data: unknown }) => void;
  onError?: (error: Event) => void;
  onOpen?: () => void;
}

export function useSSE(url: string | null, options: SSEOptions = {}) {
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<Event | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  // Use refs for callbacks to avoid re-triggering effects
  const optionsRef = useRef(options);
  optionsRef.current = options;

  const disconnect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
      setIsConnected(false);
    }
  }, []);

  useEffect(() => {
    if (!url) {
      disconnect();
      return;
    }

    // Close existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const eventSource = new EventSource(url);
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      setIsConnected(true);
      setError(null);
      optionsRef.current.onOpen?.();
    };

    eventSource.onerror = (event) => {
      setError(event);
      setIsConnected(false);
      optionsRef.current.onError?.(event);
    };

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        optionsRef.current.onMessage?.({ type: 'message', data });
      } catch {
        optionsRef.current.onMessage?.({ type: 'message', data: event.data });
      }
    };

    // Add listeners for specific event types
    // Note: 'error' is excluded because it conflicts with native EventSource error event
    ['status', 'started', 'progress', 'photos_counted', 'filtering_done', 'completed', 'job_error', 'cancelled'].forEach((type) => {
      eventSource.addEventListener(type, (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data);
          optionsRef.current.onMessage?.({ type, data });
        } catch {
          optionsRef.current.onMessage?.({ type, data: (event as MessageEvent).data });
        }
      });
    });

    return () => disconnect();
  }, [url, disconnect]);

  return { isConnected, error, disconnect };
}
