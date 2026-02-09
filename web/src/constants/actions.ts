import type { MatchAction } from '../types';

interface ActionStyle {
  label: string;
  descriptiveLabel: string;
  borderColor: string;
  bgColor: string;
  panelStyle: string;
}

const ACTION_CONFIG: Record<MatchAction, ActionStyle> = {
  create_marker: {
    label: 'New',
    descriptiveLabel: 'Unassigned',
    borderColor: 'border-red-500',
    bgColor: 'bg-red-500',
    panelStyle: 'bg-red-500/10 text-red-400 border-red-500/30',
  },
  assign_person: {
    label: 'Assign',
    descriptiveLabel: 'Needs assignment',
    borderColor: 'border-yellow-500',
    bgColor: 'bg-yellow-500',
    panelStyle: 'bg-yellow-500/10 text-yellow-400 border-yellow-500/30',
  },
  already_done: {
    label: 'Done',
    descriptiveLabel: 'Assigned',
    borderColor: 'border-green-500',
    bgColor: 'bg-green-500',
    panelStyle: 'bg-green-500/10 text-green-400 border-green-500/30',
  },
  unassign_person: {
    label: 'Outlier',
    descriptiveLabel: 'Outlier',
    borderColor: 'border-orange-500',
    bgColor: 'bg-orange-500',
    panelStyle: 'bg-orange-500/10 text-orange-400 border-orange-500/30',
  },
};

// Backward-compatible derived exports
function derive<K extends keyof ActionStyle>(key: K): Record<MatchAction, string> {
  const result = {} as Record<MatchAction, string>;
  for (const [action, style] of Object.entries(ACTION_CONFIG)) {
    result[action as MatchAction] = style[key];
  }
  return result;
}

export const ACTION_LABELS = derive('label');
export const ACTION_DESCRIPTIVE_LABELS = derive('descriptiveLabel');
export const ACTION_BORDER_COLORS = derive('borderColor');
export const ACTION_BG_COLORS = derive('bgColor');
export const ACTION_PANEL_STYLES = derive('panelStyle');
