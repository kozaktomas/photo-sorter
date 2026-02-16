import { useState, useCallback } from 'react';
import { applyFaceMatch } from '../api/client';
import type { FaceMatch, MatchAction } from '../types';

export interface ApprovalProgress {
  current: number;
  total: number;
}

export interface UseFaceApprovalOptions {
  // Called when approval succeeds - use to update local state
  onApprovalSuccess?: (match: FaceMatch) => void;
  // Called when approval fails
  onApprovalError?: (match: FaceMatch, error: string) => void;
}

export interface UseFaceApprovalReturn {
  // Single approval
  approveMatch: (match: FaceMatch, personName: string) => Promise<boolean>;
  // Batch approval
  approveAll: (matches: FaceMatch[], personName: string) => Promise<void>;
  // State
  isApproving: boolean;
  isBatchApproving: boolean;
  batchProgress: ApprovalProgress;
  approvalError: string | null;
  clearError: () => void;
}

export function useFaceApproval(options: UseFaceApprovalOptions = {}): UseFaceApprovalReturn {
  const [isApproving, setIsApproving] = useState(false);
  const [isBatchApproving, setIsBatchApproving] = useState(false);
  const [batchProgress, setBatchProgress] = useState<ApprovalProgress>({ current: 0, total: 0 });
  const [approvalError, setApprovalError] = useState<string | null>(null);

  const clearError = useCallback(() => {
    setApprovalError(null);
  }, []);

  const approveMatch = useCallback(async (match: FaceMatch, personName: string): Promise<boolean> => {
    setIsApproving(true);
    setApprovalError(null);

    try {
      const response = await applyFaceMatch({
        photo_uid: match.photo_uid,
        person_name: personName,
        action: match.action,
        marker_uid: match.marker_uid,
        file_uid: match.file_uid,
        bbox_rel: match.bbox_rel,
        face_index: match.face_index,
      });

      if (response.success) {
        options.onApprovalSuccess?.(match);
        return true;
      } else {
        const errorMsg = response.error ?? 'Failed to apply face assignment';
        setApprovalError(errorMsg);
        options.onApprovalError?.(match, errorMsg);
        return false;
      }
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Failed to apply face assignment';
      setApprovalError(errorMsg);
      options.onApprovalError?.(match, errorMsg);
      return false;
    } finally {
      setIsApproving(false);
    }
  }, [options]);

  const approveAll = useCallback(async (matches: FaceMatch[], personName: string): Promise<void> => {
    if (matches.length === 0) return;

    setIsBatchApproving(true);
    setBatchProgress({ current: 0, total: matches.length });
    setApprovalError(null);

    for (let i = 0; i < matches.length; i++) {
      const match = matches[i];
      setBatchProgress({ current: i + 1, total: matches.length });

      try {
        const response = await applyFaceMatch({
          photo_uid: match.photo_uid,
          person_name: personName,
          action: match.action,
          marker_uid: match.marker_uid,
          file_uid: match.file_uid,
          bbox_rel: match.bbox_rel,
          face_index: match.face_index,
        });

        if (response.success) {
          options.onApprovalSuccess?.(match);
        } else {
          options.onApprovalError?.(match, response.error ?? 'Failed to apply');
        }
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Failed to apply';
        options.onApprovalError?.(match, errorMsg);
        // Continue with next match even if one fails
      }
    }

    setIsBatchApproving(false);
  }, [options]);

  return {
    approveMatch,
    approveAll,
    isApproving,
    isBatchApproving,
    batchProgress,
    approvalError,
    clearError,
  };
}

// Helper to filter actionable matches (not already_done)
export function getActionableMatches(matches: FaceMatch[], filter?: MatchAction | 'all'): FaceMatch[] {
  return matches.filter((m) => {
    if (m.action === 'already_done') return false;
    if (filter && filter !== 'all' && m.action !== filter) return false;
    return true;
  });
}

// Helper to update match list after approval (mark as done)
export function updateMatchAfterApproval<T extends FaceMatch>(
  matches: T[],
  approvedMatch: FaceMatch
): T[] {
  return matches.map((m) =>
    m.photo_uid === approvedMatch.photo_uid && m.face_index === approvedMatch.face_index
      ? { ...m, action: 'already_done' as const }
      : m
  );
}

// Helper to update summary counts after approval
export function updateSummaryAfterApproval(
  summary: { create_marker: number; assign_person: number; already_done: number },
  oldAction: MatchAction
): { create_marker: number; assign_person: number; already_done: number } {
  const newSummary = { ...summary };
  if (oldAction === 'create_marker') {
    newSummary.create_marker--;
    newSummary.already_done++;
  } else if (oldAction === 'assign_person') {
    newSummary.assign_person--;
    newSummary.already_done++;
  }
  return newSummary;
}
