import { ChevronLeft, ChevronRight, Pause, Play, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';

interface TVControlBarProps {
  isPlaying: boolean;
  visible: boolean;
  hasPrev: boolean;
  hasNext: boolean;
  onTogglePlayPause: () => void;
  onPrev: () => void;
  onNext: () => void;
  onExitTVMode: () => void;
}

// Floating pill-shaped control bar shown in TV mode. Auto-hides after 3s of
// inactivity (managed by useTVMode). 56px touch targets so it stays usable
// with a TV remote air-mouse.
export function TVControlBar({
  isPlaying,
  visible,
  hasPrev,
  hasNext,
  onTogglePlayPause,
  onPrev,
  onNext,
  onExitTVMode,
}: TVControlBarProps) {
  const { t } = useTranslation('common');

  return (
    <div
      className={`fixed left-1/2 bottom-10 z-30 -translate-x-1/2 transition-opacity duration-300 ${
        visible ? 'opacity-100' : 'opacity-0 pointer-events-none'
      }`}
    >
      <div className="flex items-center gap-2 rounded-full bg-black/70 backdrop-blur-md px-3 py-2 shadow-xl ring-1 ring-white/10">
        <button
          onClick={onPrev}
          disabled={!hasPrev}
          className={`flex h-14 w-14 items-center justify-center rounded-full text-white transition-colors ${
            hasPrev ? 'hover:bg-white/15' : 'opacity-30 cursor-not-allowed'
          }`}
          aria-label={t('buttons.previousPhoto')}
        >
          <ChevronLeft className="h-7 w-7" />
        </button>
        <button
          onClick={onTogglePlayPause}
          className="flex h-14 w-14 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25 transition-colors"
          aria-label={isPlaying ? t('buttons.pause') : t('buttons.play')}
        >
          {isPlaying ? <Pause className="h-7 w-7" /> : <Play className="h-7 w-7" />}
        </button>
        <button
          onClick={onNext}
          disabled={!hasNext}
          className={`flex h-14 w-14 items-center justify-center rounded-full text-white transition-colors ${
            hasNext ? 'hover:bg-white/15' : 'opacity-30 cursor-not-allowed'
          }`}
          aria-label={t('buttons.nextPhoto')}
        >
          <ChevronRight className="h-7 w-7" />
        </button>
        <div className="mx-1 h-8 w-px bg-white/15" aria-hidden />
        <button
          onClick={onExitTVMode}
          className="flex h-14 w-14 items-center justify-center rounded-full text-white hover:bg-white/15 transition-colors"
          aria-label={t('slideshow.exitTVMode')}
          title={t('slideshow.exitTVMode')}
        >
          <X className="h-7 w-7" />
        </button>
      </div>
    </div>
  );
}
