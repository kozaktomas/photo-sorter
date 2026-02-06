import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { getThumbnailUrl } from '../../../api/client';
import type { Photo } from '../../../types';

interface SlideshowState {
  currentIndex: number;
  isPlaying: boolean;
  interval: number;
  isFullscreen: boolean;
  showInfo: boolean;
  goToNext: () => void;
  goToPrev: () => void;
  togglePlayPause: () => void;
  setInterval: (ms: number) => void;
  toggleFullscreen: () => void;
  toggleInfo: () => void;
  exit: () => void;
}

export function useSlideshow(photos: Photo[]): SlideshowState {
  const navigate = useNavigate();
  const [currentIndex, setCurrentIndex] = useState(0);
  const [isPlaying, setIsPlaying] = useState(true);
  const [interval, setIntervalValue] = useState(5000);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showInfo, setShowInfo] = useState(true);
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
      document.exitFullscreen();
    }
    navigate(-1);
  }, [navigate]);

  const setInterval = useCallback((ms: number) => {
    setIntervalValue(ms);
  }, []);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else {
      document.documentElement.requestFullscreen();
    }
  }, []);

  const toggleInfo = useCallback(() => {
    setShowInfo((prev) => !prev);
  }, []);

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

  // Preload next image
  useEffect(() => {
    if (currentIndex < total - 1) {
      const nextPhoto = photos[currentIndex + 1];
      if (nextPhoto) {
        const img = new Image();
        img.src = getThumbnailUrl(nextPhoto.uid, 'fit_1920');
      }
    }
  }, [currentIndex, photos, total]);

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
            document.exitFullscreen();
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
      }
    }

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [goToNext, goToPrev, togglePlayPause, exit, toggleFullscreen, toggleInfo]);

  return {
    currentIndex,
    isPlaying,
    interval,
    isFullscreen,
    showInfo,
    goToNext,
    goToPrev,
    togglePlayPause,
    setInterval,
    toggleFullscreen,
    toggleInfo,
    exit,
  };
}
