import { useState, useEffect } from 'react';
import { getSubjects, getConfig } from '../api/client';
import { MAX_SUBJECTS_FETCH } from '../constants';
import type { Subject, Config } from '../types';

export interface UseSubjectsAndConfigReturn {
  subjects: Subject[];
  config: Config | null;
  isLoading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
}

export interface UseSubjectsAndConfigOptions {
  // Only load subjects with photo_count > 0
  onlyWithPhotos?: boolean;
  // Custom error message
  errorMessage?: string;
}

export function useSubjectsAndConfig(
  options: UseSubjectsAndConfigOptions = {}
): UseSubjectsAndConfigReturn {
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadData = async () => {
    setIsLoading(true);
    setError(null);

    try {
      const [subjectsData, configData] = await Promise.all([
        getSubjects({ count: MAX_SUBJECTS_FETCH }),
        getConfig(),
      ]);

      let filteredSubjects = subjectsData;
      if (options.onlyWithPhotos) {
        filteredSubjects = subjectsData.filter((s) => s.photo_count > 0);
      }

      setSubjects(filteredSubjects);
      setConfig(configData);
    } catch (err) {
      console.error('Failed to load data:', err);
      setError(options.errorMessage || 'Failed to load data. Make sure you are logged in.');
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  return {
    subjects,
    config,
    isLoading,
    error,
    refresh: loadData,
  };
}

// Hook that only loads config
export function useConfig(): {
  config: Config | null;
  isLoading: boolean;
  error: string | null;
} {
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function loadConfig() {
      try {
        const configData = await getConfig();
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load config:', err);
        setError('Failed to load configuration');
      } finally {
        setIsLoading(false);
      }
    }
    loadConfig();
  }, []);

  return { config, isLoading, error };
}
