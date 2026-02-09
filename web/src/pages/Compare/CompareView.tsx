import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight, Archive, SkipForward } from 'lucide-react';
import { Button } from '../../components/Button';
import { Alert } from '../../components/Alert';
import { getThumbnailUrl } from '../../api/client';
import { MetadataDiff } from './MetadataDiff';
import type { Photo } from '../../types';

interface CompareViewProps {
  leftPhoto: Photo;
  rightPhoto: Photo;
  pairIndex: number;
  totalPairs: number;
  isArchiving: boolean;
  archiveError: string | null;
  hasPrev: boolean;
  hasNext: boolean;
  onKeepLeft: () => void;
  onKeepRight: () => void;
  onKeepBoth: () => void;
  onPrev: () => void;
  onNext: () => void;
}

export function CompareView({
  leftPhoto,
  rightPhoto,
  pairIndex,
  totalPairs,
  isArchiving,
  archiveError,
  hasPrev,
  hasNext,
  onKeepLeft,
  onKeepRight,
  onKeepBoth,
  onPrev,
  onNext,
}: CompareViewProps) {
  const { t } = useTranslation(['pages', 'common']);

  return (
    <div className="space-y-4">
      {/* Header with pair counter and navigation */}
      <div className="flex items-center justify-between">
        <div className="text-sm text-slate-400">
          {t('pages:duplicates.compare.pair')} {pairIndex + 1} {t('pages:duplicates.compare.of')} {totalPairs}
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={onPrev}
            disabled={!hasPrev || isArchiving}
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={onNext}
            disabled={!hasNext || isArchiving}
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Side-by-side photos */}
      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <div className="relative aspect-auto overflow-hidden rounded-lg bg-slate-800 border-2 border-transparent hover:border-blue-500 transition-colors">
            <img
              src={getThumbnailUrl(leftPhoto.uid, 'fit_1280')}
              alt={leftPhoto.title || leftPhoto.file_name}
              className="w-full h-auto object-contain max-h-[60vh]"
              loading="eager"
            />
          </div>
          <Button
            variant="primary"
            className="w-full"
            onClick={onKeepLeft}
            disabled={isArchiving}
            isLoading={isArchiving}
          >
            <Archive className="h-4 w-4 mr-2" />
            {t('pages:duplicates.compare.keepLeft')}
            <kbd className="ml-2 px-1.5 py-0.5 text-xs bg-blue-700 rounded">1</kbd>
          </Button>
        </div>
        <div className="space-y-2">
          <div className="relative aspect-auto overflow-hidden rounded-lg bg-slate-800 border-2 border-transparent hover:border-blue-500 transition-colors">
            <img
              src={getThumbnailUrl(rightPhoto.uid, 'fit_1280')}
              alt={rightPhoto.title || rightPhoto.file_name}
              className="w-full h-auto object-contain max-h-[60vh]"
              loading="eager"
            />
          </div>
          <Button
            variant="primary"
            className="w-full"
            onClick={onKeepRight}
            disabled={isArchiving}
            isLoading={isArchiving}
          >
            <Archive className="h-4 w-4 mr-2" />
            {t('pages:duplicates.compare.keepRight')}
            <kbd className="ml-2 px-1.5 py-0.5 text-xs bg-blue-700 rounded">2</kbd>
          </Button>
        </div>
      </div>

      {/* Keep Both button */}
      <div className="flex justify-center">
        <Button
          variant="secondary"
          onClick={onKeepBoth}
          disabled={isArchiving}
        >
          <SkipForward className="h-4 w-4 mr-2" />
          {t('pages:duplicates.compare.keepBoth')}
          <kbd className="ml-2 px-1.5 py-0.5 text-xs bg-slate-600 rounded">Space</kbd>
        </Button>
      </div>

      {/* Error message */}
      {archiveError && (
        <Alert variant="error" className="text-center">{archiveError}</Alert>
      )}

      {/* Metadata diff */}
      <MetadataDiff left={leftPhoto} right={rightPhoto} />

      {/* Keyboard shortcut help */}
      <div className="flex justify-center gap-6 text-xs text-slate-500">
        <span><kbd className="px-1.5 py-0.5 bg-slate-800 rounded">1</kbd> {t('pages:duplicates.compare.keepLeft')}</span>
        <span><kbd className="px-1.5 py-0.5 bg-slate-800 rounded">2</kbd> {t('pages:duplicates.compare.keepRight')}</span>
        <span><kbd className="px-1.5 py-0.5 bg-slate-800 rounded">Space</kbd> {t('pages:duplicates.compare.keepBoth')}</span>
        <span><kbd className="px-1.5 py-0.5 bg-slate-800 rounded">←</kbd><kbd className="px-1.5 py-0.5 bg-slate-800 rounded">→</kbd> Navigate</span>
      </div>
    </div>
  );
}
