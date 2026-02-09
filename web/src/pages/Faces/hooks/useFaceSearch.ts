import { useState, useCallback } from 'react';
import { matchFaces } from '../../../api/client';
import { DEFAULT_FACE_THRESHOLD, percentToDistance } from '../../../constants';
import type { FaceMatchResult, FaceMatch, MatchAction } from '../../../types';

export type FilterTab = 'all' | MatchAction;

export interface UseFaceSearchReturn {
  // Form state
  selectedPerson: string;
  setSelectedPerson: (value: string) => void;
  threshold: number;
  setThreshold: (value: number) => void;
  limit: number;
  setLimit: (value: number) => void;
  // Search state
  result: FaceMatchResult | null;
  isSearching: boolean;
  searchError: string | null;
  // Filter state
  activeFilter: FilterTab;
  setActiveFilter: (filter: FilterTab) => void;
  // Actions
  handleSearch: () => Promise<void>;
  // Computed
  actionableCount: number;
  // Update helpers
  updateMatchToAlreadyDone: (match: FaceMatch) => void;
  removeMatch: (match: FaceMatch) => void;
}

export function useFaceSearch(): UseFaceSearchReturn {
  // Form state
  const [selectedPerson, setSelectedPerson] = useState('');
  const [threshold, setThreshold] = useState(DEFAULT_FACE_THRESHOLD);
  const [limit, setLimit] = useState(0);

  // Search state
  const [result, setResult] = useState<FaceMatchResult | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Filter state
  const [activeFilter, setActiveFilter] = useState<FilterTab>('all');

  const handleSearch = useCallback(async () => {
    if (!selectedPerson) return;

    setIsSearching(true);
    setSearchError(null);
    setResult(null);

    try {
      const matchResult = await matchFaces({
        person_name: selectedPerson,
        threshold: percentToDistance(threshold),
        limit,
      });
      setResult(matchResult);
      setActiveFilter('all');
    } catch (err) {
      console.error('Face matching failed:', err);
      setSearchError(
        err instanceof Error ? err.message : 'Face matching failed. Database may not be configured.'
      );
    } finally {
      setIsSearching(false);
    }
  }, [selectedPerson, threshold, limit]);

  const updateMatchToAlreadyDone = useCallback((match: FaceMatch) => {
    setResult((prev) => {
      if (!prev) return prev;

      const updatedMatches = prev.matches.map((m) =>
        m.photo_uid === match.photo_uid && m.face_index === match.face_index
          ? { ...m, action: 'already_done' as const }
          : m
      );

      const oldAction = match.action;
      const newSummary = { ...prev.summary };
      if (oldAction === 'create_marker') {
        newSummary.create_marker--;
        newSummary.already_done++;
      } else if (oldAction === 'assign_person') {
        newSummary.assign_person--;
        newSummary.already_done++;
      }

      return { ...prev, matches: updatedMatches, summary: newSummary };
    });
  }, []);

  const removeMatch = useCallback((match: FaceMatch) => {
    setResult((prev) => {
      if (!prev) return prev;

      const updatedMatches = prev.matches.filter(
        (m) => !(m.photo_uid === match.photo_uid && m.face_index === match.face_index)
      );

      const newSummary = { ...prev.summary };
      if (match.action === 'create_marker') {
        newSummary.create_marker--;
      } else if (match.action === 'assign_person') {
        newSummary.assign_person--;
      } else if (match.action === 'already_done') {
        newSummary.already_done--;
      }

      return { ...prev, matches: updatedMatches, summary: newSummary };
    });
  }, []);

  // Compute actionable count based on current filter
  const actionableCount = result
    ? result.matches.filter((m) => {
        if (m.action === 'already_done') return false;
        if (activeFilter !== 'all' && m.action !== activeFilter) return false;
        return true;
      }).length
    : 0;

  return {
    selectedPerson,
    setSelectedPerson,
    threshold,
    setThreshold,
    limit,
    setLimit,
    result,
    isSearching,
    searchError,
    activeFilter,
    setActiveFilter,
    handleSearch,
    actionableCount,
    updateMatchToAlreadyDone,
    removeMatch,
  };
}
