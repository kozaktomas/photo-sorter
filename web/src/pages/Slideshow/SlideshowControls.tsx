import { Play, Pause, X, Maximize, Minimize, Info, Wand2, Captions, Tv, Move } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { SlideshowEffect } from './hooks/useSlideshow';

interface SlideshowControlsProps {
  isPlaying: boolean;
  interval: number;
  currentIndex: number;
  totalPhotos: number;
  isFullscreen: boolean;
  isTVMode: boolean;
  showInfo: boolean;
  activeEffect: SlideshowEffect;
  kenBurnsEnabled: boolean;
  captionsEnabled: boolean;
  onTogglePlayPause: () => void;
  onSetInterval: (ms: number) => void;
  onToggleFullscreen: () => void;
  onToggleTVMode: () => void;
  onToggleInfo: () => void;
  onToggleEffect: () => void;
  onToggleKenBurns: () => void;
  onToggleCaptions: () => void;
  onExit: () => void;
}

const SPEED_OPTIONS = [
  { label: '5s', value: 5000 },
  { label: '10s', value: 10000 },
  { label: '20s', value: 20000 },
  { label: '30s', value: 30000 },
];

function formatDuration(totalSeconds: number): string {
  if (totalSeconds < 60) {
    return `${Math.round(totalSeconds)}s`;
  }
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.ceil((totalSeconds % 3600) / 60);
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  return `${minutes}m`;
}

export function SlideshowControls({
  isPlaying,
  interval,
  currentIndex,
  totalPhotos,
  isFullscreen,
  isTVMode,
  showInfo,
  activeEffect,
  kenBurnsEnabled,
  captionsEnabled,
  onTogglePlayPause,
  onSetInterval,
  onToggleFullscreen,
  onToggleTVMode,
  onToggleInfo,
  onToggleEffect,
  onToggleKenBurns,
  onToggleCaptions,
  onExit,
}: SlideshowControlsProps) {
  const { t } = useTranslation('common');
  const totalSeconds = totalPhotos * (interval / 1000);

  return (
    <div className="absolute bottom-0 left-0 right-0 z-20 bg-gradient-to-t from-black/80 to-transparent pt-16 pb-6 px-6">
      <div className="flex items-center justify-between max-w-4xl mx-auto">
        {/* Play/Pause + Speed */}
        <div className="flex items-center space-x-3">
          <button
            onClick={onTogglePlayPause}
            className="p-2.5 rounded-full bg-white/15 hover:bg-white/25 text-white transition-colors"
            aria-label={isPlaying ? t('buttons.pause') : t('buttons.play')}
          >
            {isPlaying ? <Pause className="h-5 w-5" /> : <Play className="h-5 w-5" />}
          </button>

          <div className="flex items-center space-x-1">
            {SPEED_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                onClick={() => onSetInterval(opt.value)}
                className={`px-2.5 py-1 rounded text-sm transition-colors ${
                  interval === opt.value
                    ? 'bg-white/25 text-white font-medium'
                    : 'text-white/60 hover:text-white hover:bg-white/10'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* Counter + Total Time */}
        <div className="text-white/80 text-sm font-medium tabular-nums">
          {currentIndex + 1} / {totalPhotos}
          <span className="text-white/40 ml-2">~{formatDuration(totalSeconds)}</span>
        </div>

        {/* Effect toggle + KB + Captions + Info + TV + Fullscreen + Exit */}
        <div className="flex items-center space-x-2">
          <button
            onClick={onToggleEffect}
            className={`flex items-center space-x-1.5 rounded-full transition-colors ${
              activeEffect !== 'none'
                ? 'bg-white/25 text-white pl-3 pr-3.5 py-2'
                : 'bg-white/15 text-white/50 hover:bg-white/25 hover:text-white p-2.5'
            }`}
            aria-label={t('effects.' + activeEffect)}
            title={t('effects.' + activeEffect)}
          >
            <Wand2 className="h-5 w-5" />
            {activeEffect !== 'none' && (
              <span className="text-sm font-medium">{t('effects.' + activeEffect)}</span>
            )}
          </button>

          <button
            onClick={onToggleKenBurns}
            className={`p-2.5 rounded-full transition-colors ${
              kenBurnsEnabled
                ? 'bg-white/25 text-white'
                : 'bg-white/15 text-white/50 hover:bg-white/25 hover:text-white'
            }`}
            aria-label={t('slideshow.toggleKenBurns')}
            title={t('slideshow.toggleKenBurns')}
          >
            <Move className="h-5 w-5" />
          </button>

          <button
            onClick={onToggleCaptions}
            className={`p-2.5 rounded-full transition-colors ${
              captionsEnabled
                ? 'bg-white/25 text-white'
                : 'bg-white/15 text-white/50 hover:bg-white/25 hover:text-white'
            }`}
            aria-label={t('slideshow.toggleCaptions')}
            title={t('slideshow.toggleCaptions')}
          >
            <Captions className="h-5 w-5" />
          </button>

          <button
            onClick={onToggleInfo}
            className={`p-2.5 rounded-full transition-colors ${
              showInfo
                ? 'bg-white/25 text-white'
                : 'bg-white/15 text-white/50 hover:bg-white/25 hover:text-white'
            }`}
            aria-label={showInfo ? t('tooltips.hideInfo') : t('tooltips.showInfo')}
            title={t('tooltips.toggleInfo')}
          >
            <Info className="h-5 w-5" />
          </button>

          <button
            onClick={onToggleTVMode}
            className={`p-2.5 rounded-full transition-colors ${
              isTVMode
                ? 'bg-white/25 text-white'
                : 'bg-white/15 text-white/50 hover:bg-white/25 hover:text-white'
            }`}
            aria-label={isTVMode ? t('slideshow.exitTVMode') : t('slideshow.enterTVMode')}
            title={isTVMode ? t('slideshow.exitTVMode') : t('slideshow.enterTVMode')}
          >
            <Tv className="h-5 w-5" />
          </button>

          <button
            onClick={onToggleFullscreen}
            className="p-2.5 rounded-full bg-white/15 hover:bg-white/25 text-white transition-colors"
            aria-label={isFullscreen ? t('tooltips.exitFullscreen') : t('tooltips.enterFullscreen')}
            title={t('tooltips.fullscreen')}
          >
            {isFullscreen ? <Minimize className="h-5 w-5" /> : <Maximize className="h-5 w-5" />}
          </button>

          <button
            onClick={onExit}
            className="p-2.5 rounded-full bg-white/15 hover:bg-white/25 text-white transition-colors"
            aria-label={t('tooltips.exitSlideshow')}
            title={t('tooltips.exit')}
          >
            <X className="h-5 w-5" />
          </button>
        </div>
      </div>
    </div>
  );
}
