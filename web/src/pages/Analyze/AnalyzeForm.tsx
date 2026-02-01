import { useTranslation } from 'react-i18next';
import { Play, Square, DollarSign } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { FormInput } from '../../components/FormInput';
import { FormSelect } from '../../components/FormSelect';
import { FormCheckbox } from '../../components/FormCheckbox';
import type { Album, ProviderInfo } from '../../types';

interface AnalyzeFormProps {
  // Data
  albums: Album[];
  availableProviders: ProviderInfo[];
  // Form state
  selectedAlbum: string;
  setSelectedAlbum: (value: string) => void;
  provider: string;
  setProvider: (value: string) => void;
  dryRun: boolean;
  setDryRun: (value: boolean) => void;
  individualDates: boolean;
  setIndividualDates: (value: boolean) => void;
  batchMode: boolean;
  setBatchMode: (value: boolean) => void;
  forceDate: boolean;
  setForceDate: (value: boolean) => void;
  limit: number;
  setLimit: (value: number) => void;
  concurrency: number;
  setConcurrency: (value: number) => void;
  // Job state
  isJobRunning: boolean;
  isJobDone: boolean;
  isStarting: boolean;
  // Actions
  onStart: () => void;
  onCancel: () => void;
  onReset: () => void;
}

export function AnalyzeForm({
  albums,
  availableProviders,
  selectedAlbum,
  setSelectedAlbum,
  provider,
  setProvider,
  dryRun,
  setDryRun,
  individualDates,
  setIndividualDates,
  batchMode,
  setBatchMode,
  forceDate,
  setForceDate,
  limit,
  setLimit,
  concurrency,
  setConcurrency,
  isJobRunning,
  isJobDone,
  isStarting,
  onStart,
  onCancel,
  onReset,
}: AnalyzeFormProps) {
  const { t } = useTranslation(['pages', 'common']);
  const hasCurrentJob = isJobRunning || isJobDone;

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('pages:analyze.configuration')}</h2>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Album selection */}
        <FormSelect
          label={t('pages:analyze.album')}
          value={selectedAlbum}
          onChange={(e) => setSelectedAlbum(e.target.value)}
          disabled={isJobRunning}
        >
          <option value="">{t('pages:analyze.selectAlbum')}</option>
          {albums.map((album) => (
            <option key={album.uid} value={album.uid}>
              {album.title} ({t('common:units.photo', { count: album.photo_count })})
            </option>
          ))}
        </FormSelect>

        {/* Provider selection */}
        <FormSelect
          label={t('pages:analyze.aiProvider')}
          value={provider}
          onChange={(e) => setProvider(e.target.value)}
          disabled={isJobRunning}
        >
          {availableProviders.map((p) => (
            <option key={p.name} value={p.name}>
              {p.name}
            </option>
          ))}
        </FormSelect>

        {/* Options */}
        <div className="space-y-3">
          <FormCheckbox
            label={t('pages:analyze.dryRun')}
            checked={dryRun}
            onChange={(e) => setDryRun(e.target.checked)}
            disabled={isJobRunning}
          />
          <FormCheckbox
            label={t('pages:analyze.individualDates')}
            checked={individualDates}
            onChange={(e) => setIndividualDates(e.target.checked)}
            disabled={isJobRunning}
          />
          <FormCheckbox
            label={t('pages:analyze.batchMode')}
            checked={batchMode}
            onChange={(e) => setBatchMode(e.target.checked)}
            disabled={isJobRunning}
          />
          <FormCheckbox
            label={t('pages:analyze.forceDate')}
            checked={forceDate}
            onChange={(e) => setForceDate(e.target.checked)}
            disabled={isJobRunning}
          />
        </div>

        {/* Limit */}
        <FormInput
          label={t('pages:analyze.limit')}
          type="number"
          value={limit}
          onChange={(e) => setLimit(parseInt(e.target.value) || 0)}
          disabled={isJobRunning}
          min={0}
        />

        {/* Concurrency */}
        <FormInput
          label={t('pages:analyze.concurrency')}
          type="number"
          value={concurrency}
          onChange={(e) => setConcurrency(parseInt(e.target.value) || 5)}
          disabled={isJobRunning}
          min={1}
          max={20}
        />

        {/* Actions */}
        <div className="flex space-x-3 pt-4">
          {!hasCurrentJob && (
            <Button
              onClick={onStart}
              disabled={!selectedAlbum}
              isLoading={isStarting}
              className="flex-1"
              title={t('pages:analyze.paidServiceWarning')}
            >
              <Play className="h-4 w-4 mr-2" />
              {t('pages:analyze.startAnalysis')}
              <DollarSign className="h-4 w-4 ml-2 text-yellow-400" />
            </Button>
          )}

          {isJobRunning && (
            <Button variant="danger" onClick={onCancel} className="flex-1">
              <Square className="h-4 w-4 mr-2" />
              {t('common:buttons.cancel')}
            </Button>
          )}

          {isJobDone && (
            <Button variant="secondary" onClick={onReset} className="flex-1">
              {t('common:buttons.startNew')}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
