import type { MatchAction } from '../types';

// Action labels for display
export const ACTION_LABELS: Record<MatchAction, string> = {
  create_marker: 'New',
  assign_person: 'Assign',
  already_done: 'Done',
  unassign_person: 'Outlier',
};

// More descriptive labels for face assignment panel
export const ACTION_DESCRIPTIVE_LABELS: Record<MatchAction, string> = {
  create_marker: 'Unassigned',
  assign_person: 'Needs assignment',
  already_done: 'Assigned',
  unassign_person: 'Outlier',
};

// Border colors for bounding boxes and cards
export const ACTION_BORDER_COLORS: Record<MatchAction, string> = {
  create_marker: 'border-red-500',
  assign_person: 'border-yellow-500',
  already_done: 'border-green-500',
  unassign_person: 'border-orange-500',
};

// Background colors for badges
export const ACTION_BG_COLORS: Record<MatchAction, string> = {
  create_marker: 'bg-red-500',
  assign_person: 'bg-yellow-500',
  already_done: 'bg-green-500',
  unassign_person: 'bg-orange-500',
};

// Combined background + text + border for panel badges
export const ACTION_PANEL_STYLES: Record<MatchAction, string> = {
  create_marker: 'bg-red-500/10 text-red-400 border-red-500/30',
  assign_person: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/30',
  already_done: 'bg-green-500/10 text-green-400 border-green-500/30',
  unassign_person: 'bg-orange-500/10 text-orange-400 border-orange-500/30',
};

// Solid color dots for summary views
export const ACTION_DOT_COLORS: Record<MatchAction, string> = {
  create_marker: 'bg-red-500',
  assign_person: 'bg-yellow-500',
  already_done: 'bg-green-500',
  unassign_person: 'bg-orange-500',
};
