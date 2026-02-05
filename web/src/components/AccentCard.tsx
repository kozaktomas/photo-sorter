import type { ReactNode } from 'react';
import type { AccentColor } from '../constants/pageConfig';
import { colorMap } from '../constants/pageConfig';

interface AccentCardProps {
  children: ReactNode;
  color: AccentColor;
  className?: string;
}

export function AccentCard({ children, color, className = '' }: AccentCardProps) {
  const c = colorMap[color];
  return (
    <div className={`bg-slate-800 rounded-xl border border-slate-700 border-t-2 ${c.topBorder} ${className}`}>
      {children}
    </div>
  );
}
