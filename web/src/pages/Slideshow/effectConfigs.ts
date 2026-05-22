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

// Ken Burns is now an independent toggle (K), not part of the transition effect
// cycle. It can be layered on top of any transition effect (or 'none'). When
// enabled, photos animate with a slow pan/zoom for their full dwell time.
export const KEN_BURNS_CONFIG = {
  during: (variant: number, intervalMs: number): string =>
    // duration covers the visible dwell + crossfade so the freeze frame
    // captured for the outgoing photo matches what was on screen
    `${KB_VARIANTS[variant]} ${intervalMs + 300}ms cubic-bezier(0.4, 0, 0.2, 1) forwards`,
  duringEndStyle: (variant: number): CSSProperties => ({
    transform: KB_END_TRANSFORMS[variant],
  }),
};

export const EFFECT_CONFIGS: Record<Exclude<SlideshowEffect, 'none'>, EffectConfig> = {
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
  reflections: 'Reflections',
  dissolve: 'Dissolve',
  push: 'Push',
  origami: 'Origami',
};
