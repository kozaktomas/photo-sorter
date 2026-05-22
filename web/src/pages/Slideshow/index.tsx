import { useState, useEffect, useLayoutEffect, useRef, useCallback, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { useSlideshowPhotos } from './hooks/useSlideshowPhotos';
import { useSlideshow } from './hooks/useSlideshow';
import { useTVMode } from './hooks/useTVMode';
import { SlideshowControls } from './SlideshowControls';
import { TVControlBar } from './TVControlBar';
import { EFFECT_CONFIGS, KEN_BURNS_CONFIG } from './effectConfigs';
import type { Photo } from '../../types';

function useMouseActivity(isFullscreen: boolean) {
  const [controlsVisible, setControlsVisible] = useState(true);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const resetTimer = useCallback(() => {
    setControlsVisible(true);
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }
    if (isFullscreen) {
      timerRef.current = setTimeout(() => {
        setControlsVisible(false);
      }, 5000);
    }
  }, [isFullscreen]);

  useEffect(() => {
    if (!isFullscreen) {
      setControlsVisible(true);
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      return;
    }

    // Start the hide timer when entering fullscreen
    resetTimer();

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [isFullscreen, resetTimer]);

  return { controlsVisible, onMouseMove: resetTimer };
}

export function SlideshowPage() {
  const { t } = useTranslation('common');
  const { photos, title, isLoading, error, sourceType } = useSlideshowPhotos();
  const [toast, setToast] = useState<string | null>(null);
  const toastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showToast = useCallback((message: string) => {
    setToast(message);
    if (toastTimerRef.current) clearTimeout(toastTimerRef.current);
    toastTimerRef.current = setTimeout(() => setToast(null), 4000);
  }, []);

  useEffect(() => () => {
    if (toastTimerRef.current) clearTimeout(toastTimerRef.current);
  }, []);

  const tv = useTVMode({
    onFullscreenDenied: useCallback(() => showToast(t('slideshow.fullscreenUnavailable')), [showToast, t]),
  });

  // The React Compiler auto-memoizes this object based on its inputs; the
  // inner functions are stable useCallbacks from useTVMode.
  const slideshow = useSlideshow(photos, {
    tvMode: {
      isActive: tv.isTVMode,
      toggle: () => void tv.toggle(),
      exit: () => void tv.exit(),
    },
  });

  const [imageLoaded, setImageLoaded] = useState(false);
  const [prevPhoto, setPrevPhoto] = useState<Photo | null>(null);
  const [isTransitioning, setIsTransitioning] = useState(false);
  const [outgoingEndStyle, setOutgoingEndStyle] = useState<CSSProperties | null>(null);
  const prevPhotoRef = useRef<Photo | null>(null);
  const preloadedRef = useRef<Set<string>>(new Set());
  const { controlsVisible: nonTVControlsVisible, onMouseMove: nonTVOnMouseMove } = useMouseActivity(
    slideshow.isFullscreen && !tv.isTVMode,
  );

  const currentPhoto = photos[slideshow.currentIndex];
  const transitionConfig = slideshow.activeEffect !== 'none'
    ? EFFECT_CONFIGS[slideshow.activeEffect]
    : null;
  // Whether Ken Burns motion is currently driving the "during" animation. If
  // KB is enabled it always wins; otherwise fall back to the transition
  // effect's own during anim (e.g. reflections breathe).
  const kbDuring = slideshow.kenBurnsEnabled;
  const effectiveOverflowHidden = transitionConfig?.overflowHidden !== false || kbDuring;
  // Total wall-clock time for the cross-fade/transition between photos.
  const transitionDuration = transitionConfig?.transitionDuration ?? 300;

  // Preload upcoming photos and track readiness
  useEffect(() => {
    for (let offset = 1; offset <= 2; offset++) {
      const idx = slideshow.currentIndex + offset;
      if (idx < photos.length) {
        const photo = photos[idx];
        if (photo && !preloadedRef.current.has(photo.uid)) {
          const img = new Image();
          img.onload = () => preloadedRef.current.add(photo.uid);
          img.src = getThumbnailUrl(photo.uid, 'fit_1920');
        }
      }
    }
  }, [slideshow.currentIndex, photos]);

  // Set up transition state before browser paints to avoid blink
  useLayoutEffect(() => {
    const isPreloaded = currentPhoto && preloadedRef.current.has(currentPhoto.uid);
    if (!isPreloaded) {
      setImageLoaded(false);
    } else {
      setImageLoaded(true);
    }
    if (prevPhotoRef.current && prevPhotoRef.current.uid !== currentPhoto?.uid) {
      // Freeze the outgoing photo at its during-animation end state via static CSS
      // kenBurnsVariant still holds the OLD value here (effect hasn't updated it yet)
      if (kbDuring) {
        setOutgoingEndStyle(KEN_BURNS_CONFIG.duringEndStyle(slideshow.kenBurnsVariant));
      } else if (transitionConfig?.duringEndStyle) {
        setOutgoingEndStyle(transitionConfig.duringEndStyle(slideshow.kenBurnsVariant));
      } else {
        setOutgoingEndStyle(null);
      }
      if (transitionConfig || kbDuring) {
        setPrevPhoto(prevPhotoRef.current);
        setIsTransitioning(true);
        const timer = setTimeout(() => {
          setIsTransitioning(false);
          setPrevPhoto(null);
          setOutgoingEndStyle(null);
        }, transitionDuration);
        return () => clearTimeout(timer);
      }
    }
  }, [currentPhoto?.uid]); // eslint-disable-line react-hooks/exhaustive-deps

  // Track current photo for crossfade
  useEffect(() => {
    if (currentPhoto) {
      prevPhotoRef.current = currentPhoto;
    }
  }, [currentPhoto]);
  const hasPrev = slideshow.currentIndex > 0;
  const hasNext = slideshow.currentIndex < photos.length - 1;

  // Format date for display (top info overlay)
  const photoDate = currentPhoto?.taken_at
    ? new Date(currentPhoto.taken_at).toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
      })
    : null;

  // Format date for TV-mode captions ("June 2024" — human-friendly, no day)
  const captionDate = currentPhoto?.taken_at && currentPhoto.year > 1
    ? new Date(currentPhoto.taken_at).toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'long',
      })
    : null;

  // Unified mouse-move handler: feeds both the non-TV controls timer and the TV mode timer.
  const onMouseMove = useCallback(() => {
    if (tv.isTVMode) {
      tv.onMouseMove();
    } else {
      nonTVOnMouseMove();
    }
  }, [tv, nonTVOnMouseMove]);

  if (isLoading) {
    return (
      <div className="fixed inset-0 bg-black flex items-center justify-center z-50">
        <div className="text-white/60 text-lg">Loading...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="fixed inset-0 bg-black flex items-center justify-center z-50">
        <div className="text-center space-y-4">
          <div className="text-red-400 text-lg">{error}</div>
          <button
            onClick={slideshow.exit}
            className="px-4 py-2 rounded bg-white/15 hover:bg-white/25 text-white transition-colors"
          >
            Go Back
          </button>
        </div>
      </div>
    );
  }

  if (photos.length === 0) {
    return (
      <div className="fixed inset-0 bg-black flex items-center justify-center z-50">
        <div className="text-center space-y-4">
          <div className="text-white/60 text-lg">No photos to display</div>
          <button
            onClick={slideshow.exit}
            className="px-4 py-2 rounded bg-white/15 hover:bg-white/25 text-white transition-colors"
          >
            Go Back
          </button>
        </div>
      </div>
    );
  }

  // Overlay visibility for the chrome (top info bar, bottom controls bar, side arrows).
  // In TV mode, all chrome is hidden — replaced by the floating pill bar + captions strip.
  // In non-TV fullscreen, chrome auto-hides on inactivity. Otherwise it's group-hover driven.
  const overlayClass = tv.isTVMode
    ? 'opacity-0 pointer-events-none'
    : slideshow.isFullscreen
    ? `transition-opacity duration-300 ${nonTVControlsVisible ? 'opacity-100' : 'opacity-0 pointer-events-none'}`
    : 'opacity-0 group-hover/slideshow:opacity-100 transition-opacity duration-300';

  // Cursor hides in TV mode after 3s inactivity, and in non-TV fullscreen after 5s.
  const cursorHidden = (tv.isTVMode && !tv.cursorVisible) || (slideshow.isFullscreen && !tv.isTVMode && !nonTVControlsVisible);

  // Pause = freeze Ken Burns + transition animations on the current frame.
  const animationPlayState: CSSProperties['animationPlayState'] = slideshow.isPlaying ? 'running' : 'paused';

  return (
    <div
      className={`fixed inset-0 bg-black z-50 ${!slideshow.isFullscreen && !tv.isTVMode ? 'group/slideshow' : ''} ${
        cursorHidden ? 'cursor-none' : ''
      }`}
      onMouseMove={onMouseMove}
    >
      {/* Top info overlay */}
      {slideshow.showInfo && (
        <div className={`absolute top-0 left-0 right-0 z-20 bg-gradient-to-b from-black/70 to-transparent pt-4 pb-12 px-6 ${overlayClass}`}>
          <div className="max-w-4xl mx-auto">
            <div className="text-white/50 text-sm">
              {sourceType === 'album' ? 'Album' : 'Label'}: {title}
            </div>
            {currentPhoto && (
              <div className="mt-1">
                <span className="text-white font-medium">
                  {currentPhoto.title || currentPhoto.file_name}
                </span>
                {photoDate && currentPhoto.year > 1 && (
                  <span className="text-white/50 ml-3 text-sm">{photoDate}</span>
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Main photo */}
      <div className={`absolute inset-0 flex items-center justify-center ${effectiveOverflowHidden ? 'overflow-hidden' : ''}`}>
        {/* Current photo (underneath) */}
        {currentPhoto && (
          <img
            key={currentPhoto.uid}
            src={getThumbnailUrl(currentPhoto.uid, 'fit_1920')}
            alt={currentPhoto.title || currentPhoto.file_name}
            className={`h-full w-full object-contain transition-opacity duration-300 ${
              imageLoaded ? 'opacity-100' : 'opacity-0'
            }`}
            style={(() => {
              if (!imageLoaded) return undefined;
              const anims: string[] = [];
              if (kbDuring) {
                anims.push(KEN_BURNS_CONFIG.during(slideshow.kenBurnsVariant, slideshow.interval));
              } else if (transitionConfig?.during) {
                anims.push(transitionConfig.during(slideshow.kenBurnsVariant, slideshow.interval));
              }
              if (isTransitioning && transitionConfig?.incoming) {
                anims.push(transitionConfig.incoming);
              }
              if (anims.length === 0) return undefined;
              return {
                animation: anims.join(', '),
                animationPlayState,
                ...transitionConfig?.incomingStyle,
              };
            })()}
            onLoad={() => setImageLoaded(true)}
            onError={() => setImageLoaded(true)}
          />
        )}
        {/* Outgoing photo (on top, fading/animating out) */}
        {isTransitioning && prevPhoto && (
          <img
            key={`prev-${prevPhoto.uid}`}
            src={getThumbnailUrl(prevPhoto.uid, 'fit_1920')}
            alt=""
            className="absolute inset-0 h-full w-full object-contain"
            style={{
              animation: transitionConfig?.outgoing ?? undefined,
              animationPlayState,
              ...transitionConfig?.outgoingStyle,
              ...outgoingEndStyle,
            }}
          />
        )}
      </div>

      {/* TV-mode captions strip (bottom-left). Large font, semi-transparent dark BG. */}
      {tv.isTVMode && slideshow.captionsEnabled && currentPhoto && (currentPhoto.title || currentPhoto.description || captionDate) && (
        <div className="absolute bottom-10 left-10 z-20 max-w-[60vw] rounded-md px-6 py-4 backdrop-blur-sm" style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}>
          {(currentPhoto.title || currentPhoto.file_name) && (
            <div className="text-white font-semibold leading-tight" style={{ fontSize: 'clamp(1.5rem, 2.2vw, 2.5rem)' }}>
              {currentPhoto.title || currentPhoto.file_name}
            </div>
          )}
          {currentPhoto.description && (
            <div className="text-white/85 mt-2 leading-snug" style={{ fontSize: 'clamp(1rem, 1.4vw, 1.5rem)' }}>
              {currentPhoto.description}
            </div>
          )}
          {captionDate && (
            <div className="text-white/70 mt-2" style={{ fontSize: 'clamp(0.95rem, 1.2vw, 1.25rem)' }}>
              {captionDate}
            </div>
          )}
        </div>
      )}

      {/* Left arrow */}
      <button
        onClick={slideshow.goToPrev}
        disabled={!hasPrev}
        className={`absolute left-4 top-1/2 -translate-y-1/2 z-20 p-3 rounded-full bg-black/50 backdrop-blur-sm transition-all ${
          hasPrev
            ? 'text-white hover:bg-black/70 cursor-pointer'
            : 'text-white/20 cursor-not-allowed'
        } ${
          tv.isTVMode
            ? 'opacity-0 pointer-events-none'
            : slideshow.isFullscreen
            ? `${nonTVControlsVisible ? (hasPrev ? 'opacity-100' : 'opacity-30') : 'opacity-0 pointer-events-none'} transition-opacity duration-300`
            : (hasPrev ? 'opacity-0 group-hover/slideshow:opacity-100' : 'opacity-0 group-hover/slideshow:opacity-30')
        }`}
        aria-label={t('buttons.previousPhoto')}
      >
        <ChevronLeft className="h-8 w-8" />
      </button>

      {/* Right arrow */}
      <button
        onClick={slideshow.goToNext}
        disabled={!hasNext}
        className={`absolute right-4 top-1/2 -translate-y-1/2 z-20 p-3 rounded-full bg-black/50 backdrop-blur-sm transition-all ${
          hasNext
            ? 'text-white hover:bg-black/70 cursor-pointer'
            : 'text-white/20 cursor-not-allowed'
        } ${
          tv.isTVMode
            ? 'opacity-0 pointer-events-none'
            : slideshow.isFullscreen
            ? `${nonTVControlsVisible ? (hasNext ? 'opacity-100' : 'opacity-30') : 'opacity-0 pointer-events-none'} transition-opacity duration-300`
            : (hasNext ? 'opacity-0 group-hover/slideshow:opacity-100' : 'opacity-0 group-hover/slideshow:opacity-30')
        }`}
        aria-label={t('buttons.nextPhoto')}
      >
        <ChevronRight className="h-8 w-8" />
      </button>

      {/* Bottom controls (hidden in TV mode) */}
      <div className={overlayClass}>
        <SlideshowControls
          isPlaying={slideshow.isPlaying}
          interval={slideshow.interval}
          currentIndex={slideshow.currentIndex}
          totalPhotos={photos.length}
          isFullscreen={slideshow.isFullscreen}
          isTVMode={tv.isTVMode}
          showInfo={slideshow.showInfo}
          activeEffect={slideshow.activeEffect}
          kenBurnsEnabled={slideshow.kenBurnsEnabled}
          captionsEnabled={slideshow.captionsEnabled}
          onTogglePlayPause={slideshow.togglePlayPause}
          onSetInterval={slideshow.setInterval}
          onToggleFullscreen={slideshow.toggleFullscreen}
          onToggleTVMode={() => void tv.toggle()}
          onToggleInfo={slideshow.toggleInfo}
          onToggleEffect={slideshow.toggleEffect}
          onToggleKenBurns={slideshow.toggleKenBurns}
          onToggleCaptions={slideshow.toggleCaptions}
          onExit={slideshow.exit}
        />
      </div>

      {/* TV-mode floating pill control bar */}
      {tv.isTVMode && (
        <TVControlBar
          isPlaying={slideshow.isPlaying}
          visible={tv.controlsVisible}
          hasPrev={hasPrev}
          hasNext={hasNext}
          onTogglePlayPause={slideshow.togglePlayPause}
          onPrev={slideshow.goToPrev}
          onNext={slideshow.goToNext}
          onExitTVMode={() => void tv.exit()}
        />
      )}

      {/* Toast for fullscreen-denied or similar */}
      {toast && (
        <div className="fixed top-6 left-1/2 -translate-x-1/2 z-40 rounded-md bg-black/85 px-4 py-2 text-white text-sm shadow-lg ring-1 ring-white/15">
          {toast}
        </div>
      )}
    </div>
  );
}
