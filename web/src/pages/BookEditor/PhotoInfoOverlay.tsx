import { useTranslation } from 'react-i18next';
import { StickyNote } from 'lucide-react';

interface Props {
  description?: string;
  note?: string;
  orientation?: 'L' | 'P' | null;
  compact?: boolean;
}

export function PhotoInfoOverlay({ description, note, orientation, compact }: Props) {
  const { t } = useTranslation('pages');
  const hasContent = description ?? note ?? orientation;
  if (!hasContent) return null;

  return (
    <div className="absolute inset-0 pointer-events-none flex flex-col justify-end">
      {orientation && (
        <span className={`absolute bottom-0.5 right-0.5 text-[9px] font-bold leading-none px-1 py-0.5 rounded z-10 ${
          orientation === 'L' ? 'bg-blue-600/80 text-blue-100' : 'bg-amber-600/80 text-amber-100'
        }`} style={note || description ? { bottom: `${(note ? 1.25 : 0) + (description ? (compact ? 1.25 : 2.25) : 0)}rem` } : undefined}>
          {orientation === 'L' ? t('books.editor.orientationLandscape') : t('books.editor.orientationPortrait')}
        </span>
      )}
      {note && (
        <div className="bg-amber-900/60 text-amber-200 text-[10px] px-1.5 py-0.5 flex items-center gap-1 line-clamp-1">
          <StickyNote className="h-2.5 w-2.5 flex-shrink-0" />
          <span className="truncate">{note}</span>
        </div>
      )}
      {description && (
        <div className={`bg-black/60 text-white text-xs px-1.5 py-0.5 ${compact ? 'line-clamp-1' : 'line-clamp-2'}`}>
          {description}
        </div>
      )}
    </div>
  );
}
