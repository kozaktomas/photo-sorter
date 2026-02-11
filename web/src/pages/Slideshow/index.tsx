import { useState, useEffect, useLayoutEffect, useRef, useCallback, type CSSProperties } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { useSlideshowPhotos } from './hooks/useSlideshowPhotos';
import { useSlideshow } from './hooks/useSlideshow';
import { SlideshowControls } from './SlideshowControls';
import { EFFECT_CONFIGS } from './effectConfigs';
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
  const { photos, title, isLoading, error, sourceType } = useSlideshowPhotos();
  const slideshow = useSlideshow(photos);
  const [imageLoaded, setImageLoaded] = useState(false);
  const [prevPhoto, setPrevPhoto] = useState<Photo | null>(null);
  const [isTransitioning, setIsTransitioning] = useState(false);
  const [outgoingEndStyle, setOutgoingEndStyle] = useState<CSSProperties | null>(null);
  const prevPhotoRef = useRef<Photo | null>(null);
  const preloadedRef = useRef<Set<string>>(new Set());
  const { controlsVisible, onMouseMove } = useMouseActivity(slideshow.isFullscreen);

  const currentPhoto = photos[slideshow.currentIndex];
  const activeConfig = slideshow.activeEffect !== 'none'
    ? EFFECT_CONFIGS[slideshow.activeEffect]
    : null;

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
    if (activeConfig && prevPhotoRef.current && prevPhotoRef.current.uid !== currentPhoto?.uid) {
      // Freeze the outgoing photo at its during-animation end state via static CSS
      // kenBurnsVariant still holds the OLD value here (effect hasn't updated it yet)
      if (activeConfig.duringEndStyle) {
        setOutgoingEndStyle(activeConfig.duringEndStyle(slideshow.kenBurnsVariant));
      } else {
        setOutgoingEndStyle(null);
      }
      setPrevPhoto(prevPhotoRef.current);
      setIsTransitioning(true);
      const timer = setTimeout(() => {
        setIsTransitioning(false);
        setPrevPhoto(null);
        setOutgoingEndStyle(null);
      }, activeConfig.transitionDuration);
      return () => clearTimeout(timer);
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

  // Format date for display
  const photoDate = currentPhoto?.taken_at
    ? new Date(currentPhoto.taken_at).toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
      })
    : null;

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

  // Determine overlay visibility
  const overlayClass = slideshow.isFullscreen
    ? `transition-opacity duration-300 ${controlsVisible ? 'opacity-100' : 'opacity-0 pointer-events-none'}`
    : 'opacity-0 group-hover/slideshow:opacity-100 transition-opacity duration-300';

  return (
    <div
      className={`fixed inset-0 bg-black z-50 ${!slideshow.isFullscreen ? 'group/slideshow' : ''} ${
        slideshow.isFullscreen && !controlsVisible ? 'cursor-none' : ''
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
      <div className={`absolute inset-0 flex items-center justify-center ${activeConfig?.overflowHidden !== false ? 'overflow-hidden' : ''}`}>
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
              if (!activeConfig || !imageLoaded) return undefined;
              // Build animation list: during animation first (stable index), incoming on top
              const anims: string[] = [];
              if (activeConfig.during) {
                anims.push(activeConfig.during(slideshow.kenBurnsVariant, slideshow.interval));
              }
              if (isTransitioning && activeConfig.incoming) {
                anims.push(activeConfig.incoming);
              }
              if (anims.length === 0) return undefined;
              return { animation: anims.join(', '), ...activeConfig.incomingStyle };
            })()}
            onLoad={() => setImageLoaded(true)}
            onError={() => setImageLoaded(true)}
          />
        )}
        {/* Outgoing photo (on top, fading/animating out) */}
        {activeConfig && isTransitioning && prevPhoto && (
          <img
            key={`prev-${prevPhoto.uid}`}
            src={getThumbnailUrl(prevPhoto.uid, 'fit_1920')}
            alt=""
            className="absolute inset-0 h-full w-full object-contain"
            style={{
              animation: activeConfig.outgoing ?? undefined,
              ...activeConfig.outgoingStyle,
              ...outgoingEndStyle,
            }}
          />
        )}
      </div>

      {/* Left arrow */}
      <button
        onClick={slideshow.goToPrev}
        disabled={!hasPrev}
        className={`absolute left-4 top-1/2 -translate-y-1/2 z-20 p-3 rounded-full bg-black/50 backdrop-blur-sm transition-all ${
          hasPrev
            ? 'text-white hover:bg-black/70 cursor-pointer'
            : 'text-white/20 cursor-not-allowed'
        } ${
          slideshow.isFullscreen
            ? `${controlsVisible ? (hasPrev ? 'opacity-100' : 'opacity-30') : 'opacity-0 pointer-events-none'} transition-opacity duration-300`
            : (hasPrev ? 'opacity-0 group-hover/slideshow:opacity-100' : 'opacity-0 group-hover/slideshow:opacity-30')
        }`}
        aria-label="Previous photo"
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
          slideshow.isFullscreen
            ? `${controlsVisible ? (hasNext ? 'opacity-100' : 'opacity-30') : 'opacity-0 pointer-events-none'} transition-opacity duration-300`
            : (hasNext ? 'opacity-0 group-hover/slideshow:opacity-100' : 'opacity-0 group-hover/slideshow:opacity-30')
        }`}
        aria-label="Next photo"
      >
        <ChevronRight className="h-8 w-8" />
      </button>

      {/* Bottom controls */}
      <div className={overlayClass}>
        <SlideshowControls
          isPlaying={slideshow.isPlaying}
          interval={slideshow.interval}
          currentIndex={slideshow.currentIndex}
          totalPhotos={photos.length}
          isFullscreen={slideshow.isFullscreen}
          showInfo={slideshow.showInfo}
          activeEffect={slideshow.activeEffect}
          onTogglePlayPause={slideshow.togglePlayPause}
          onSetInterval={slideshow.setInterval}
          onToggleFullscreen={slideshow.toggleFullscreen}
          onToggleInfo={slideshow.toggleInfo}
          onToggleEffect={slideshow.toggleEffect}
          onExit={slideshow.exit}
        />
      </div>
    </div>
  );
}
