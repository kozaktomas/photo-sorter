import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { getAlbums, getConfig } from '../../api/client';
import { useSortJob } from './hooks/useSortJob';
import { AnalyzeForm } from './AnalyzeForm';
import { AnalyzeStatus } from './AnalyzeStatus';
import { AnalyzeResults } from './AnalyzeResults';
import { PageLoading } from '../../components/LoadingState';
import { DEFAULT_CONCURRENCY, MAX_ALBUMS_FETCH } from '../../constants';
import type { Album, Config } from '../../types';

export function AnalyzePage() {
  const { t } = useTranslation('pages');
  const [searchParams] = useSearchParams();
  const preselectedAlbum = searchParams.get('album');

  const [albums, setAlbums] = useState<Album[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Form state
  const [selectedAlbum, setSelectedAlbum] = useState(preselectedAlbum || '');
  const [provider, setProvider] = useState('openai');
  const [dryRun, setDryRun] = useState(true);
  const [individualDates, setIndividualDates] = useState(false);
  const [batchMode, setBatchMode] = useState(false);
  const [forceDate, setForceDate] = useState(false);
  const [limit, setLimit] = useState(0);
  const [concurrency, setConcurrency] = useState(DEFAULT_CONCURRENCY);

  // Job state via custom hook
  const {
    currentJob,
    isStarting,
    startJob,
    cancelJob,
    resetJob,
    isJobRunning,
    isJobDone,
  } = useSortJob();

  useEffect(() => {
    async function loadData() {
      try {
        const [albumsData, configData] = await Promise.all([
          getAlbums({ count: MAX_ALBUMS_FETCH }),
          getConfig(),
        ]);
        setAlbums(albumsData);
        setConfig(configData);

        // Set default provider to first available
        const firstAvailable = configData.providers.find(p => p.available);
        if (firstAvailable) {
          setProvider(firstAvailable.name);
        }
      } catch (err) {
        console.error('Failed to load data:', err);
      } finally {
        setIsLoading(false);
      }
    }
    loadData();
  }, []);

  const handleStart = async () => {
    if (!selectedAlbum) return;

    try {
      await startJob(selectedAlbum, {
        dry_run: dryRun,
        limit: limit || undefined,
        individual_dates: individualDates,
        batch_mode: batchMode,
        provider,
        force_date: forceDate,
        concurrency,
      });
    } catch {
      alert('Failed to start analysis');
    }
  };

  const availableProviders = config?.providers.filter(p => p.available) || [];

  if (isLoading) {
    return <PageLoading text="Loading..." />;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">{t('analyze.title')}</h1>
        <p className="text-slate-400 mt-1">{t('analyze.subtitle')}</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <AnalyzeForm
          albums={albums}
          availableProviders={availableProviders}
          selectedAlbum={selectedAlbum}
          setSelectedAlbum={setSelectedAlbum}
          provider={provider}
          setProvider={setProvider}
          dryRun={dryRun}
          setDryRun={setDryRun}
          individualDates={individualDates}
          setIndividualDates={setIndividualDates}
          batchMode={batchMode}
          setBatchMode={setBatchMode}
          forceDate={forceDate}
          setForceDate={setForceDate}
          limit={limit}
          setLimit={setLimit}
          concurrency={concurrency}
          setConcurrency={setConcurrency}
          isJobRunning={isJobRunning}
          isJobDone={isJobDone}
          isStarting={isStarting}
          onStart={handleStart}
          onCancel={cancelJob}
          onReset={resetJob}
        />

        <AnalyzeStatus
          currentJob={currentJob}
          selectedAlbum={selectedAlbum}
        />
      </div>

      {currentJob?.result?.suggestions && (
        <AnalyzeResults suggestions={currentJob.result.suggestions} />
      )}
    </div>
  );
}
