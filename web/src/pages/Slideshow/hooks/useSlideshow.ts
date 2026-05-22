import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import type { Photo } from '../../../types';

export type SlideshowEffect = 'none' | 'reflections' | 'dissolve' | 'push' | 'origami';

const EFFECT_ORDER: SlideshowEffect[] = ['none', 'reflections', 'dissolve', 'push', 'origami'];

export const SPEED_PRESETS = [5000, 10000, 20000, 30000] as const;
const DEFAULT_INTERVAL = SPEED_PRESETS[0];

const LS_INTERVAL = 'slideshow.interval';
const LS_KEN_BURNS = 'slideshow.kenBurns';
const LS_CAPTIONS = 'slideshow.captions';

function readPersistedNumber(key: string, fallback: number, allowed: readonly number[]): number {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return fallback;
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) return fallback;
    if (!allowed.includes(parsed)) return fallback;
    return parsed;
  } catch {
    return fallback;
  }
}

function readPersistedBool(key: string, fallback: boolean): boolean {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw == null) return fallback;
    return raw === 'true';
  } catch {
    return fallback;
  }
}

function writePersisted(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // localStorage may be unavailable (private mode, quota); ignore.
  }
}

interface UseSlideshowOptions {
  // Wired by the page to share keyboard handling with useTVMode.
  tvMode?: {
    isActive: boolean;
    toggle: () => void;
    exit: () => void;
  };
}

interface SlideshowState {
  currentIndex: number;
  isPlaying: boolean;
  interval: number;
  isFullscreen: boolean;
  showInfo: boolean;
  activeEffect: SlideshowEffect;
  kenBurnsEnabled: boolean;
  captionsEnabled: boolean;
  kenBurnsVariant: number;
  goToNext: () => void;
  goToPrev: () => void;
  togglePlayPause: () => void;
  setInterval: (ms: number) => void;
  toggleFullscreen: () => void;
  toggleInfo: () => void;
  toggleEffect: () => void;
  toggleKenBurns: () => void;
  toggleCaptions: () => void;
  exit: () => void;
}

export function useSlideshow(photos: Photo[], options: UseSlideshowOptions = {}): SlideshowState {
  const navigate = useNavigate();
  const [currentIndex, setCurrentIndex] = useState(0);
  const [isPlaying, setIsPlaying] = useState(true);
  const [interval, setIntervalValue] = useState(() =>
    readPersistedNumber(LS_INTERVAL, DEFAULT_INTERVAL, SPEED_PRESETS),
  );
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showInfo, setShowInfo] = useState(true);
  const [activeEffect, setActiveEffect] = useState<SlideshowEffect>('none');
  const [kenBurnsEnabled, setKenBurnsEnabled] = useState(() => readPersistedBool(LS_KEN_BURNS, false));
  const [captionsEnabled, setCaptionsEnabled] = useState(() => readPersistedBool(LS_CAPTIONS, false));
  const [kenBurnsVariant, setKenBurnsVariant] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const total = photos.length;

  const goToNext = useCallback(() => {
    setCurrentIndex((prev) => {
      if (prev >= total - 1) {
        setIsPlaying(false);
        return prev;
      }
      return prev + 1;
    });
  }, [total]);

  const goToPrev = useCallback(() => {
    setCurrentIndex((prev) => Math.max(0, prev - 1));
  }, []);

  const togglePlayPause = useCallback(() => {
    setIsPlaying((prev) => {
      if (!prev && currentIndex >= total - 1) {
        // Restart from beginning if at the end
        setCurrentIndex(0);
      }
      return !prev;
    });
  }, [currentIndex, total]);

  const exit = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    }
    void navigate(-1);
  }, [navigate]);

  const setInterval = useCallback((ms: number) => {
    setIntervalValue(ms);
    writePersisted(LS_INTERVAL, String(ms));
  }, []);

  const cycleSpeed = useCallback((direction: 1 | -1) => {
    setIntervalValue((prev) => {
      const idx = SPEED_PRESETS.indexOf(prev as (typeof SPEED_PRESETS)[number]);
      const base = idx === -1 ? 0 : idx;
      const next = (base + direction + SPEED_PRESETS.length) % SPEED_PRESETS.length;
      const value = SPEED_PRESETS[next];
      writePersisted(LS_INTERVAL, String(value));
      return value;
    });
  }, []);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void document.documentElement.requestFullscreen();
    }
  }, []);

  const toggleInfo = useCallback(() => {
    setShowInfo((prev) => !prev);
  }, []);

  const toggleEffect = useCallback(() => {
    setActiveEffect((prev) => {
      const idx = EFFECT_ORDER.indexOf(prev);
      return EFFECT_ORDER[(idx + 1) % EFFECT_ORDER.length];
    });
  }, []);

  const toggleKenBurns = useCallback(() => {
    setKenBurnsEnabled((prev) => {
      const next = !prev;
      writePersisted(LS_KEN_BURNS, String(next));
      return next;
    });
  }, []);

  const toggleCaptions = useCallback(() => {
    setCaptionsEnabled((prev) => {
      const next = !prev;
      writePersisted(LS_CAPTIONS, String(next));
      return next;
    });
  }, []);

  // Alternate Ken Burns direction per photo so the loop doesn't feel repetitive.
  const KB_VARIANT_COUNT = 6;
  useEffect(() => {
    setKenBurnsVariant(Math.floor(Math.random() * KB_VARIANT_COUNT));
  }, [currentIndex]);

  // Sync fullscreen state with browser
  useEffect(() => {
    function handleFullscreenChange() {
      setIsFullscreen(!!document.fullscreenElement);
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

  // Auto-play timer
  useEffect(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    if (isPlaying && total > 0 && currentIndex < total - 1) {
      timerRef.current = setTimeout(goToNext, interval);
    }

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [isPlaying, currentIndex, interval, total, goToNext]);

  // Keyboard controls
  const tvMode = options.tvMode;
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      switch (e.key) {
        case 'ArrowRight':
          goToNext();
          break;
        case 'ArrowLeft':
          goToPrev();
          break;
        case 'ArrowUp':
          // slower (longer dwell) — next preset up
          e.preventDefault();
          cycleSpeed(1);
          break;
        case 'ArrowDown':
          // faster (shorter dwell) — previous preset
          e.preventDefault();
          cycleSpeed(-1);
          break;
        case ' ':
          e.preventDefault();
          togglePlayPause();
          break;
        case 'Escape':
          if (tvMode?.isActive) {
            tvMode.exit();
          } else if (document.fullscreenElement) {
            void document.exitFullscreen();
          } else {
            exit();
          }
          break;
        case 'f':
        case 'F':
          if (tvMode) {
            tvMode.toggle();
          } else {
            toggleFullscreen();
          }
          break;
        case 'i':
        case 'I':
          toggleInfo();
          break;
        case 'k':
        case 'K':
          toggleKenBurns();
          break;
        case 'c':
        case 'C':
          toggleCaptions();
          break;
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [
    goToNext,
    goToPrev,
    togglePlayPause,
    cycleSpeed,
    exit,
    toggleFullscreen,
    toggleInfo,
    toggleKenBurns,
    toggleCaptions,
    tvMode,
  ]);

  return {
    currentIndex,
    isPlaying,
    interval,
    isFullscreen,
    showInfo,
    activeEffect,
    kenBurnsEnabled,
    captionsEnabled,
    kenBurnsVariant,
    goToNext,
    goToPrev,
    togglePlayPause,
    setInterval,
    toggleFullscreen,
    toggleInfo,
    toggleEffect,
    toggleKenBurns,
    toggleCaptions,
    exit,
  };
}
