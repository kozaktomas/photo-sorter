import { useCallback, useMemo, useRef } from 'react';
import { ShortcutsContext, type ShortcutsContextValue } from './context';

type Handler = () => void;

interface ShortcutsProviderProps {
  children: React.ReactNode;
}

export function ShortcutsProvider({ children }: ShortcutsProviderProps) {
  const handlersRef = useRef(new Map<string, Handler>());

  const register = useCallback((id: string, handler: Handler) => {
    handlersRef.current.set(id, handler);
  }, []);

  const unregister = useCallback((id: string) => {
    handlersRef.current.delete(id);
  }, []);

  const value = useMemo<ShortcutsContextValue>(
    () => ({ register, unregister, handlersRef }),
    [register, unregister],
  );

  return <ShortcutsContext.Provider value={value}>{children}</ShortcutsContext.Provider>;
}
