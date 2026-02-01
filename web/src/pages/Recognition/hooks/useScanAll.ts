import { useState, useCallback, useRef } from 'react';
import { matchFaces } from '../../../api/client';
import { RECOGNITION_CONCURRENCY, DEFAULT_RECOGNITION_CONFIDENCE } from '../../../constants';
import type { Subject, FaceMatch } from '../../../types';

export interface PersonResult {
  slug: string;
  name: string;
  actionable: FaceMatch[];
  alreadyDone: number;
}

export interface ScanProgress {
  current: number;
  total: number;
  currentPerson: string;
}

export interface UseScanAllReturn {
  // State
  confidence: number;
  setConfidence: (value: number) => void;
  isScanning: boolean;
  scanProgress: ScanProgress;
  results: PersonResult[];
  scanError: string | null;
  // Actions
  startScan: (subjects: Subject[]) => Promise<void>;
  cancelScan: () => void;
  // Update helpers
  updatePersonResult: (
    personSlug: string,
    updater: (prev: PersonResult) => PersonResult | null
  ) => void;
  // Computed
  totalActionable: number;
  totalAlreadyDone: number;
}

export function useScanAll(): UseScanAllReturn {
  const [confidence, setConfidence] = useState(DEFAULT_RECOGNITION_CONFIDENCE);
  const [isScanning, setIsScanning] = useState(false);
  const [scanProgress, setScanProgress] = useState<ScanProgress>({ current: 0, total: 0, currentPerson: '' });
  const [results, setResults] = useState<PersonResult[]>([]);
  const [scanError, setScanError] = useState<string | null>(null);
  const cancelRef = useRef(false);

  const startScan = useCallback(async (subjects: Subject[]) => {
    const eligibleSubjects = subjects.filter((s) => s.photo_count > 0);
    if (eligibleSubjects.length === 0) return;

    setIsScanning(true);
    setScanError(null);
    setResults([]);
    cancelRef.current = false;

    const distanceThreshold = 1 - confidence / 100;
    const total = eligibleSubjects.length;
    let completed = 0;

    setScanProgress({ current: 0, total, currentPerson: '' });

    // Process subjects with concurrency limit
    const queue = [...eligibleSubjects];

    const worker = async () => {
      while (queue.length > 0) {
        if (cancelRef.current) return;
        const subject = queue.shift();
        if (!subject) return;

        setScanProgress((prev) => ({ ...prev, currentPerson: subject.name }));

        try {
          const matchResult = await matchFaces({
            person_name: subject.slug,
            threshold: distanceThreshold,
            limit: 0,
          });

          const actionable = matchResult.matches.filter((m) => m.action !== 'already_done');
          const alreadyDone = matchResult.matches.length - actionable.length;

          if (actionable.length > 0) {
            setResults((prev) => [
              ...prev,
              { slug: subject.slug, name: subject.name, actionable, alreadyDone },
            ]);
          }
        } catch (err) {
          console.error(`Failed to scan ${subject.name}:`, err);
        }

        completed++;
        setScanProgress({ current: completed, total, currentPerson: subject.name });
      }
    };

    const workers = Array.from({ length: RECOGNITION_CONCURRENCY }, () => worker());
    await Promise.all(workers);

    setIsScanning(false);
  }, [confidence]);

  const cancelScan = useCallback(() => {
    cancelRef.current = true;
  }, []);

  const updatePersonResult = useCallback((
    personSlug: string,
    updater: (prev: PersonResult) => PersonResult | null
  ) => {
    setResults((prev) =>
      prev
        .map((r) => (r.slug === personSlug ? updater(r) : r))
        .filter((r): r is PersonResult => r !== null && r.actionable.length > 0)
    );
  }, []);

  const totalActionable = results.reduce((sum, r) => sum + r.actionable.length, 0);
  const totalAlreadyDone = results.reduce((sum, r) => sum + r.alreadyDone, 0);

  return {
    confidence,
    setConfidence,
    isScanning,
    scanProgress,
    results,
    scanError,
    startScan,
    cancelScan,
    updatePersonResult,
    totalActionable,
    totalAlreadyDone,
  };
}
