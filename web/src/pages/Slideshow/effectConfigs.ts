import type { CSSProperties } from 'react';
import type { SlideshowEffect } from './hooks/useSlideshow';

export interface EffectConfig {
  outgoing: string | null;
  incoming: string | null;
  during: ((variant: number, intervalMs: number) => string) | null;
  duringEndStyle?: (variant: number) => CSSProperties;
  transitionDuration: number;
  overflowHidden: boolean;
  outgoingStyle?: CSSProperties;
  incomingStyle?: CSSProperties;
}

export const KB_VARIANTS = [
  'kb-zoom-in-left',
  'kb-zoom-in-right',
  'kb-zoom-in-center',
  'kb-zoom-out-left',
  'kb-zoom-out-right',
  'kb-pan-left-to-right',
];

const KB_END_TRANSFORMS = [
  'scale(1.15) translate(-3%, -2%)',   // kb-zoom-in-left
  'scale(1.15) translate(3%, -2%)',    // kb-zoom-in-right
  'scale(1.2)',                         // kb-zoom-in-center
  'scale(1.0) translate(0%, 0%)',      // kb-zoom-out-left
  'scale(1.0) translate(0%, 0%)',      // kb-zoom-out-right
  'scale(1.1) translate(4%, 0%)',      // kb-pan-left-to-right
];

export const EFFECT_CONFIGS: Record<Exclude<SlideshowEffect, 'none'>, EffectConfig> = {
  kenBurns: {
    outgoing: 'kb-fade-out 800ms ease-in forwards',
    incoming: 'kb-fade-in 800ms ease-out forwards',
    during: (variant, intervalMs) =>
      `${KB_VARIANTS[variant]} ${intervalMs}ms ease-in-out forwards`,
    duringEndStyle: (variant) => ({ transform: KB_END_TRANSFORMS[variant] }),
    transitionDuration: 800,
    overflowHidden: true,
  },
  reflections: {
    outgoing: 'refl-out 700ms ease-in-out forwards',
    incoming: 'refl-in 700ms ease-in-out forwards',
    during: (_variant, intervalMs) =>
      `refl-breathe ${intervalMs}ms ease-in-out infinite`,
    transitionDuration: 700,
    overflowHidden: true,
  },
  dissolve: {
    outgoing: 'dissolve-out 1000ms ease-in-out forwards',
    incoming: 'dissolve-in 1000ms ease-in-out forwards',
    during: null,
    transitionDuration: 1000,
    overflowHidden: false,
  },
  push: {
    outgoing: 'push-out 600ms ease-in-out forwards',
    incoming: 'push-in 600ms ease-in-out forwards',
    during: null,
    transitionDuration: 600,
    overflowHidden: true,
  },
  origami: {
    outgoing: 'origami-out 800ms ease-in-out forwards',
    incoming: 'origami-in 800ms ease-in-out forwards',
    during: null,
    transitionDuration: 800,
    overflowHidden: true,
    outgoingStyle: { transformOrigin: 'left center' },
    incomingStyle: { transformOrigin: 'right center' },
  },
};

export const EFFECT_LABELS: Record<SlideshowEffect, string> = {
  none: 'No effect',
  kenBurns: 'Ken Burns',
  reflections: 'Reflections',
  dissolve: 'Dissolve',
  push: 'Push',
  origami: 'Origami',
};
