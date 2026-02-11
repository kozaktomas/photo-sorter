import { useState, useEffect, useCallback } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Loader2 } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { colorMap } from '../../constants/pageConfig';
import { getPhoto } from '../../api/client';
import { CompareView } from './CompareView';
import { CompareSummary } from './CompareSummary';
import { useCompareState } from './hooks/useCompareState';
import type { Photo } from '../../types';

interface CompareLocationState {
  photoUids: string[];
  groupIndex: number;
}

export function ComparePage() {
  const { t } = useTranslation(['pages']);
  const location = useLocation();
  const navigate = useNavigate();
  const state = location.state as CompareLocationState | null;

  const [photos, setPhotos] = useState<Photo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Redirect if no state
  useEffect(() => {
    if (!state?.photoUids?.length) {
      void navigate('/duplicates', { replace: true });
    }
  }, [state, navigate]);

  // Load photo metadata
  useEffect(() => {
    if (!state?.photoUids?.length) return;

    async function loadPhotos() {
      setIsLoading(true);
      setLoadError(null);
      try {
        const results = await Promise.all(
          state!.photoUids.map((uid) => getPhoto(uid))
        );
        setPhotos(results);
      } catch (err) {
        setLoadError(err instanceof Error ? err.message : 'Failed to load photos');
      } finally {
        setIsLoading(false);
      }
    }
    void loadPhotos();
  }, [state]);

  if (!state?.photoUids?.length) {
    return null; // Will redirect
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="h-8 w-8 animate-spin text-blue-500" />
      </div>
    );
  }

  if (loadError) {
    return (
      <Card>
        <CardContent className="py-8">
          <div className="text-center text-red-400">{loadError}</div>
        </CardContent>
      </Card>
    );
  }

  if (photos.length < 2) {
    return (
      <Card>
        <CardContent className="py-8">
          <div className="text-center text-slate-400">
            {t('pages:duplicates.compare.noState')}
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <ComparePageContent
      photos={photos}
      groupIndex={state.groupIndex}
    />
  );
}

function ComparePageContent({
  photos,
  groupIndex,
}: {
  photos: Photo[];
  groupIndex: number;
}) {
  const { t } = useTranslation(['pages']);
  const compare = useCompareState(photos);

  // Keyboard shortcuts
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Ignore if user is typing in an input
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement
      ) {
        return;
      }

      switch (e.key) {
        case '1':
          e.preventDefault();
          void compare.keepLeft();
          break;
        case '2':
          e.preventDefault();
          void compare.keepRight();
          break;
        case ' ':
          e.preventDefault();
          compare.keepBoth();
          break;
        case 'ArrowLeft':
          e.preventDefault();
          compare.goToPrev();
          break;
        case 'ArrowRight':
          e.preventDefault();
          compare.goToNext();
          break;
      }
    },
    [compare]
  );

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div className="space-y-6">
      {/* Accent line */}
      <div className={`h-0.5 ${colorMap.lime.gradient} rounded-full`} />
      <div>
        <h1 className="text-2xl font-bold text-white">
          {t('pages:duplicates.compare.title')}
        </h1>
        <p className="text-slate-400 mt-1">
          {t('pages:duplicates.compare.group')} {groupIndex} &mdash; {photos.length} {t('pages:labels.photos').toLowerCase()}
        </p>
      </div>

      <Card>
        <CardHeader>
          <h2 className="text-lg font-semibold text-white">
            {t('pages:duplicates.compare.title')}
          </h2>
        </CardHeader>
        <CardContent>
          {compare.isComplete ? (
            <CompareSummary
              totalPairs={compare.totalPairs}
              archivedCount={compare.archiveCount}
              skippedCount={compare.skippedCount}
            />
          ) : compare.currentPair ? (
            <CompareView
              leftPhoto={compare.photos[compare.currentPair.leftIndex]}
              rightPhoto={compare.photos[compare.currentPair.rightIndex]}
              pairIndex={compare.currentPairIndex}
              totalPairs={compare.activePairs.length}
              isArchiving={compare.isArchiving}
              archiveError={compare.archiveError}
              hasPrev={compare.currentPairIndex > 0}
              hasNext={compare.currentPairIndex < compare.activePairs.length - 1}
              onKeepLeft={compare.keepLeft}
              onKeepRight={compare.keepRight}
              onKeepBoth={compare.keepBoth}
              onPrev={compare.goToPrev}
              onNext={compare.goToNext}
            />
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
