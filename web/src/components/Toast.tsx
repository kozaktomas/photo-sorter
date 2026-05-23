import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import { CheckCircle, XCircle, Info, Loader2, X } from 'lucide-react';

// A small dependency-free toast stack.
//
// - `toast.success/error/info/promise` push items onto a queue.
// - At most `MAX_VISIBLE` items render at the bottom-right corner.
// - Success/info auto-dismiss after 4s; errors after 6s; promise toasts stay
//   in the "loading" state until the user-supplied promise settles, then
//   transition to success/error with the standard timer.
// - Toasts have a close button. The focused toast can also be dismissed with
//   the Esc key.
// - A subtle progress bar runs out across the auto-dismiss window.

type ToastKind = 'success' | 'error' | 'info' | 'loading';

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
  // When set, the toast auto-dismisses after this many ms. Omitted for
  // "loading" toasts which stay until manually transitioned.
  duration?: number;
  // Wall-clock ms when the toast was last (re)started. Drives the progress
  // bar so swapping kind/message on a promise resolution restarts cleanly.
  startedAt: number;
}

interface PromiseMessages<T> {
  loading: string;
  success: string | ((value: T) => string);
  error: string | ((err: unknown) => string);
}

interface ToastApi {
  success: (message: string) => number;
  error: (message: string) => number;
  info: (message: string) => number;
  // promise resolves/rejects update the same toast in place.
  promise: <T>(p: Promise<T>, messages: PromiseMessages<T>) => Promise<T>;
  dismiss: (id: number) => void;
}

const DEFAULT_DURATIONS: Record<Exclude<ToastKind, 'loading'>, number> = {
  success: 4000,
  info: 4000,
  error: 6000,
};

const MAX_VISIBLE = 3;

const ToastContext = createContext<ToastApi | null>(null);

let nextId = 1;
function genId(): number {
  return nextId++;
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  // Hold the latest items in a ref so callbacks closed over the initial
  // state still see the live queue.
  const itemsRef = useRef(items);
  itemsRef.current = items;

  const dismiss = useCallback((id: number) => {
    setItems((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const push = useCallback(
    (item: Omit<ToastItem, 'id' | 'startedAt'>) => {
      const id = genId();
      const next: ToastItem = {
        ...item,
        id,
        startedAt: Date.now(),
      };
      setItems((prev) => [...prev, next]);
      return id;
    },
    [],
  );

  // Update an existing toast in place (used by toast.promise).
  const update = useCallback(
    (id: number, patch: Partial<Omit<ToastItem, 'id'>>) => {
      setItems((prev) =>
        prev.map((t) =>
          t.id === id
            ? {
                ...t,
                ...patch,
                startedAt: patch.startedAt ?? Date.now(),
              }
            : t,
        ),
      );
    },
    [],
  );

  const success = useCallback(
    (message: string) =>
      push({ kind: 'success', message, duration: DEFAULT_DURATIONS.success }),
    [push],
  );
  const error = useCallback(
    (message: string) =>
      push({ kind: 'error', message, duration: DEFAULT_DURATIONS.error }),
    [push],
  );
  const info = useCallback(
    (message: string) =>
      push({ kind: 'info', message, duration: DEFAULT_DURATIONS.info }),
    [push],
  );

  const promise = useCallback(
    async <T,>(p: Promise<T>, messages: PromiseMessages<T>): Promise<T> => {
      const id = push({ kind: 'loading', message: messages.loading });
      try {
        const value = await p;
        const text =
          typeof messages.success === 'function'
            ? messages.success(value)
            : messages.success;
        update(id, {
          kind: 'success',
          message: text,
          duration: DEFAULT_DURATIONS.success,
        });
        return value;
      } catch (err) {
        const text =
          typeof messages.error === 'function'
            ? messages.error(err)
            : messages.error;
        update(id, {
          kind: 'error',
          message: text,
          duration: DEFAULT_DURATIONS.error,
        });
        throw err;
      }
    },
    [push, update],
  );

  const api = useMemo<ToastApi>(
    () => ({ success, error, info, promise, dismiss }),
    [success, error, info, promise, dismiss],
  );

  // Cap the visible queue. Older items beyond MAX_VISIBLE are hidden; once
  // a visible one auto-dismisses, the next queued item slides in.
  const visible = items.slice(-MAX_VISIBLE);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastViewport items={visible} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    throw new Error('useToast must be used within <ToastProvider>');
  }
  return ctx;
}

interface ToastViewportProps {
  items: ToastItem[];
  onDismiss: (id: number) => void;
}

function ToastViewport({ items, onDismiss }: ToastViewportProps) {
  if (items.length === 0) return null;
  return (
    <div
      className="fixed bottom-4 right-4 z-[9999] flex flex-col gap-2 pointer-events-none"
      aria-live="polite"
      aria-relevant="additions"
    >
      {items.map((toast) => (
        <ToastCard key={toast.id} toast={toast} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

const KIND_STYLES: Record<ToastKind, { ring: string; bar: string; icon: React.ReactNode }> = {
  success: {
    ring: 'border-green-500/40 bg-slate-800/95',
    bar: 'bg-green-500',
    icon: <CheckCircle className="h-4 w-4 text-green-400" />,
  },
  error: {
    ring: 'border-red-500/40 bg-slate-800/95',
    bar: 'bg-red-500',
    icon: <XCircle className="h-4 w-4 text-red-400" />,
  },
  info: {
    ring: 'border-blue-500/40 bg-slate-800/95',
    bar: 'bg-blue-500',
    icon: <Info className="h-4 w-4 text-blue-400" />,
  },
  loading: {
    ring: 'border-slate-500/40 bg-slate-800/95',
    bar: 'bg-slate-500',
    icon: <Loader2 className="h-4 w-4 text-slate-300 animate-spin" />,
  },
};

interface ToastCardProps {
  toast: ToastItem;
  onDismiss: (id: number) => void;
}

function ToastCard({ toast, onDismiss }: ToastCardProps) {
  const { t } = useTranslation('common');
  const cardRef = useRef<HTMLDivElement>(null);
  const [progress, setProgress] = useState(100);

  // Auto-dismiss + progress bar tick. Loading toasts have no duration, so
  // they stay until update()/dismiss() is called from the outside.
  useEffect(() => {
    if (!toast.duration) {
      setProgress(100);
      return;
    }
    const start = toast.startedAt;
    const end = start + toast.duration;
    let raf = 0;
    const tick = () => {
      const now = Date.now();
      const remaining = Math.max(0, end - now);
      const pct = (remaining / toast.duration!) * 100;
      setProgress(pct);
      if (remaining > 0) {
        raf = requestAnimationFrame(tick);
      } else {
        onDismiss(toast.id);
      }
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [toast.id, toast.duration, toast.startedAt, onDismiss]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      onDismiss(toast.id);
    }
  };

  const styles = KIND_STYLES[toast.kind];

  return (
    <div
      ref={cardRef}
      role={toast.kind === 'error' ? 'alert' : 'status'}
      tabIndex={0}
      onKeyDown={handleKeyDown}
      className={`pointer-events-auto w-80 max-w-[90vw] rounded-lg border ${styles.ring} text-slate-100 shadow-xl backdrop-blur overflow-hidden focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-400`}
    >
      <div className="flex items-start gap-2 px-3 py-2">
        <div className="mt-0.5 shrink-0">{styles.icon}</div>
        <div className="flex-1 text-sm leading-snug whitespace-pre-line">
          {toast.message}
        </div>
        <button
          type="button"
          onClick={() => onDismiss(toast.id)}
          className="text-slate-400 hover:text-white shrink-0"
          aria-label={t('buttons.close')}
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      {/* Progress bar — width animates from 100% → 0%. Hidden for indefinite
          (loading) toasts. */}
      {toast.duration && (
        <div className="h-0.5 bg-slate-700/60">
          <div
            className={`h-full ${styles.bar} transition-[width] duration-100 ease-linear`}
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </div>
  );
}
