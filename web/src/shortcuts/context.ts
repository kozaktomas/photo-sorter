import { createContext, useContext, type RefObject } from 'react';

type Handler = () => void;

export interface ShortcutsContextValue {
  register: (id: string, handler: Handler) => void;
  unregister: (id: string) => void;
  // Handlers live in a ref so registering does not trigger a re-render of
  // the tree below the provider. The global keydown listener reads through
  // this.
  handlersRef: RefObject<Map<string, Handler>>;
}

export const ShortcutsContext = createContext<ShortcutsContextValue | null>(null);

export function useShortcutsContext(): ShortcutsContextValue {
  const ctx = useContext(ShortcutsContext);
  if (!ctx) {
    throw new Error('useShortcutsContext must be used inside <ShortcutsProvider>');
  }
  return ctx;
}
