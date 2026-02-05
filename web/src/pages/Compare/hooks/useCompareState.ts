import { useState, useCallback, useMemo } from 'react';
import { archivePhotos } from '../../../api/client';
import type { Photo } from '../../../types';

export interface PhotoPair {
  leftIndex: number;
  rightIndex: number;
}

export type PairDecision = 'keep_left' | 'keep_right' | 'keep_both';

export interface CompareState {
  photos: Photo[];
  pairs: PhotoPair[];
  currentPairIndex: number;
  decisions: Map<number, PairDecision>;
  archivedUids: Set<string>;
  isArchiving: boolean;
  archiveError: string | null;
  isComplete: boolean;
}

export function useCompareState(photos: Photo[]) {
  // Generate all unique pairs
  const allPairs = useMemo(() => {
    const pairs: PhotoPair[] = [];
    for (let i = 0; i < photos.length; i++) {
      for (let j = i + 1; j < photos.length; j++) {
        pairs.push({ leftIndex: i, rightIndex: j });
      }
    }
    return pairs;
  }, [photos]);

  const [currentPairIndex, setCurrentPairIndex] = useState(0);
  const [decisions, setDecisions] = useState<Map<number, PairDecision>>(new Map());
  const [archivedUids, setArchivedUids] = useState<Set<string>>(new Set());
  const [isArchiving, setIsArchiving] = useState(false);
  const [archiveError, setArchiveError] = useState<string | null>(null);

  // Filter out pairs where either photo has been archived
  const activePairs = useMemo(() => {
    return allPairs.filter(
      (pair) =>
        !archivedUids.has(photos[pair.leftIndex]?.uid) &&
        !archivedUids.has(photos[pair.rightIndex]?.uid)
    );
  }, [allPairs, archivedUids, photos]);

  // Clamp currentPairIndex to valid range
  const safePairIndex = Math.min(currentPairIndex, Math.max(0, activePairs.length - 1));
  const currentPair = activePairs[safePairIndex] ?? null;
  const isComplete = activePairs.length === 0 || safePairIndex >= activePairs.length;

  const archiveCount = archivedUids.size;
  const skippedCount = decisions.size - archiveCount;

  const goToNext = useCallback(() => {
    setCurrentPairIndex((prev) => Math.min(prev + 1, activePairs.length - 1));
  }, [activePairs.length]);

  const goToPrev = useCallback(() => {
    setCurrentPairIndex((prev) => Math.max(prev - 1, 0));
  }, []);

  const keepLeft = useCallback(async () => {
    if (!currentPair || isArchiving) return;
    const archiveUid = photos[currentPair.rightIndex].uid;
    setIsArchiving(true);
    setArchiveError(null);
    try {
      await archivePhotos([archiveUid]);
      setArchivedUids((prev) => new Set([...prev, archiveUid]));
      setDecisions((prev) => new Map(prev).set(safePairIndex, 'keep_left'));
      // Don't advance index since the active pairs list shrinks automatically
    } catch (err) {
      setArchiveError(err instanceof Error ? err.message : 'Failed to archive');
    } finally {
      setIsArchiving(false);
    }
  }, [currentPair, isArchiving, photos, safePairIndex]);

  const keepRight = useCallback(async () => {
    if (!currentPair || isArchiving) return;
    const archiveUid = photos[currentPair.leftIndex].uid;
    setIsArchiving(true);
    setArchiveError(null);
    try {
      await archivePhotos([archiveUid]);
      setArchivedUids((prev) => new Set([...prev, archiveUid]));
      setDecisions((prev) => new Map(prev).set(safePairIndex, 'keep_right'));
    } catch (err) {
      setArchiveError(err instanceof Error ? err.message : 'Failed to archive');
    } finally {
      setIsArchiving(false);
    }
  }, [currentPair, isArchiving, photos, safePairIndex]);

  const keepBoth = useCallback(() => {
    if (!currentPair || isArchiving) return;
    setDecisions((prev) => new Map(prev).set(safePairIndex, 'keep_both'));
    goToNext();
  }, [currentPair, isArchiving, safePairIndex, goToNext]);

  return {
    photos,
    allPairs,
    activePairs,
    currentPair,
    currentPairIndex: safePairIndex,
    totalPairs: allPairs.length,
    decisions,
    archivedUids,
    isArchiving,
    archiveError,
    isComplete,
    archiveCount,
    skippedCount,
    goToNext,
    goToPrev,
    keepLeft,
    keepRight,
    keepBoth,
  };
}
