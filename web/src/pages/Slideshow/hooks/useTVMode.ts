import { useCallback, useEffect, useRef, useState } from 'react';

interface UseTVModeOptions {
  onFullscreenDenied: () => void;
}

interface UseTVModeResult {
  isTVMode: boolean;
  // True when TV mode is active but the browser refused the Fullscreen API
  // request; the UI falls back to an in-page maximized black overlay.
  fallbackMode: boolean;
  controlsVisible: boolean;
  cursorVisible: boolean;
  enter: () => Promise<void>;
  exit: () => Promise<void>;
  toggle: () => Promise<void>;
  onMouseMove: () => void;
}

const INACTIVITY_MS = 3000;

// Wake Lock API isn't in our lib.dom yet; structural typing keeps it optional.
interface WakeLockLike {
  release: () => Promise<void>;
}
interface WakeLockNavigator {
  wakeLock?: { request: (type: 'screen') => Promise<WakeLockLike> };
}

export function useTVMode({ onFullscreenDenied }: UseTVModeOptions): UseTVModeResult {
  const [isTVMode, setIsTVMode] = useState(false);
  const [fallbackMode, setFallbackMode] = useState(false);
  const [visible, setVisible] = useState(true);
  const wakeLockRef = useRef<WakeLockLike | null>(null);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isTVModeRef = useRef(false);
  const fallbackModeRef = useRef(false);

  isTVModeRef.current = isTVMode;
  fallbackModeRef.current = fallbackMode;

  const releaseWakeLock = useCallback(() => {
    const wl = wakeLockRef.current;
    if (wl) {
      wakeLockRef.current = null;
      void wl.release().catch(() => undefined);
    }
  }, []);

  const acquireWakeLock = useCallback(async () => {
    const wl = (navigator as unknown as WakeLockNavigator).wakeLock;
    if (!wl?.request) return;
    try {
      wakeLockRef.current = await wl.request('screen');
    } catch {
      // unsupported or denied — silent no-op per spec
    }
  }, []);

  const enter = useCallback(async () => {
    let fullscreenOk = true;
    if (!document.fullscreenElement) {
      try {
        await document.documentElement.requestFullscreen();
      } catch {
        fullscreenOk = false;
        onFullscreenDenied();
      }
    }
    setFallbackMode(!fullscreenOk);
    setIsTVMode(true);
    setVisible(true);
    void acquireWakeLock();
  }, [acquireWakeLock, onFullscreenDenied]);

  const exit = useCallback(async () => {
    if (document.fullscreenElement) {
      try {
        await document.exitFullscreen();
      } catch {
        // already exiting or unsupported — ignore
      }
    }
    releaseWakeLock();
    setIsTVMode(false);
    setFallbackMode(false);
    setVisible(true);
  }, [releaseWakeLock]);

  const toggle = useCallback(async () => {
    if (isTVModeRef.current) {
      await exit();
    } else {
      await enter();
    }
  }, [enter, exit]);

  // React to browser-driven fullscreen changes (ESC, F11, etc.). When the
  // browser drops fullscreen and we weren't in the in-page fallback,
  // tear TV mode down so the chrome reappears.
  useEffect(() => {
    function handleFullscreenChange() {
      if (!document.fullscreenElement && isTVModeRef.current && !fallbackModeRef.current) {
        releaseWakeLock();
        setIsTVMode(false);
        setFallbackMode(false);
        setVisible(true);
      }
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, [releaseWakeLock]);

  // Wake lock is auto-released when the tab is hidden; reacquire on focus.
  useEffect(() => {
    if (!isTVMode) return;
    function handleVisibility() {
      if (document.visibilityState === 'visible' && isTVModeRef.current && !wakeLockRef.current) {
        void acquireWakeLock();
      }
    }
    document.addEventListener('visibilitychange', handleVisibility);
    return () => document.removeEventListener('visibilitychange', handleVisibility);
  }, [isTVMode, acquireWakeLock]);

  useEffect(() => {
    return () => releaseWakeLock();
  }, [releaseWakeLock]);

  const resetTimer = useCallback(() => {
    setVisible(true);
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
    if (isTVModeRef.current) {
      hideTimerRef.current = setTimeout(() => setVisible(false), INACTIVITY_MS);
    }
  }, []);

  useEffect(() => {
    if (!isTVMode) {
      setVisible(true);
      if (hideTimerRef.current) {
        clearTimeout(hideTimerRef.current);
        hideTimerRef.current = null;
      }
      return;
    }
    resetTimer();
    return () => {
      if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
    };
  }, [isTVMode, resetTimer]);

  return {
    isTVMode,
    fallbackMode,
    controlsVisible: visible,
    cursorVisible: visible,
    enter,
    exit,
    toggle,
    onMouseMove: resetTimer,
  };
}
