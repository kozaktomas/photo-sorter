import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import type { Photo } from '../../../types';

export type SlideshowEffect = 'none' | 'kenBurns' | 'reflections' | 'dissolve' | 'push' | 'origami';

const EFFECT_ORDER: SlideshowEffect[] = ['none', 'kenBurns', 'reflections', 'dissolve', 'push', 'origami'];

interface SlideshowState {
  currentIndex: number;
  isPlaying: boolean;
  interval: number;
  isFullscreen: boolean;
  showInfo: boolean;
  activeEffect: SlideshowEffect;
  kenBurnsVariant: number;
  goToNext: () => void;
  goToPrev: () => void;
  togglePlayPause: () => void;
  setInterval: (ms: number) => void;
  toggleFullscreen: () => void;
  toggleInfo: () => void;
  toggleEffect: () => void;
  exit: () => void;
}

export function useSlideshow(photos: Photo[]): SlideshowState {
  const navigate = useNavigate();
  const [currentIndex, setCurrentIndex] = useState(0);
  const [isPlaying, setIsPlaying] = useState(true);
  const [interval, setIntervalValue] = useState(5000);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showInfo, setShowInfo] = useState(true);
  const [activeEffect, setActiveEffect] = useState<SlideshowEffect>('none');
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

  // Randomize Ken Burns variant on each photo change
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
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      switch (e.key) {
        case 'ArrowRight':
          goToNext();
          break;
        case 'ArrowLeft':
          goToPrev();
          break;
        case ' ':
          e.preventDefault();
          togglePlayPause();
          break;
        case 'Escape':
          if (document.fullscreenElement) {
            void document.exitFullscreen();
          } else {
            exit();
          }
          break;
        case 'f':
        case 'F':
          toggleFullscreen();
          break;
        case 'i':
        case 'I':
          toggleInfo();
          break;
        case 'k':
        case 'K':
          toggleEffect();
          break;
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [goToNext, goToPrev, togglePlayPause, exit, toggleFullscreen, toggleInfo, toggleEffect]);

  return {
    currentIndex,
    isPlaying,
    interval,
    isFullscreen,
    showInfo,
    activeEffect,
    kenBurnsVariant,
    goToNext,
    goToPrev,
    togglePlayPause,
    setInterval,
    toggleFullscreen,
    toggleInfo,
    toggleEffect,
    exit,
  };
}
