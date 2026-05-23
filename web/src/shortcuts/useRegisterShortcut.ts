import { useEffect, useRef } from 'react';
import { useShortcutsContext } from './context';

type Handler = () => void;

// Registers a handler for `shortcutId` while the calling component is mounted
// AND `enabled` is true. Pass `enabled=false` to temporarily suspend the
// shortcut without unmounting the component (e.g. a modal stealing focus, or
// a viewer-role user who can't take the underlying action).
export function useRegisterShortcut(
  shortcutId: string,
  handler: Handler | undefined,
  enabled = true,
) {
  const { register, unregister } = useShortcutsContext();
  // Stash the latest handler in a ref so callers don't have to memoize it for
  // the registration to stay correct.
  const handlerRef = useRef<Handler | undefined>(handler);
  handlerRef.current = handler;

  useEffect(() => {
    if (!enabled || !handlerRef.current) return;
    register(shortcutId, () => handlerRef.current?.());
    return () => unregister(shortcutId);
  }, [shortcutId, enabled, register, unregister]);
}

export function useShortcutHandlersRef() {
  return useShortcutsContext().handlersRef;
}
