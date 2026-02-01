import { useTranslation } from 'react-i18next';
import { Search, Square } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import type { Subject } from '../../types';

interface ScanProgress {
  current: number;
  total: number;
  currentPerson: string;
}

interface ScanConfigPanelProps {
  subjects: Subject[];
  confidence: number;
  setConfidence: (value: number) => void;
  isScanning: boolean;
  scanProgress: ScanProgress;
  scanError: string | null;
  onScan: () => void;
  onCancel: () => void;
}

export function ScanConfigPanel({
  subjects,
  confidence,
  setConfidence,
  isScanning,
  scanProgress,
  scanError,
  onScan,
  onCancel,
}: ScanConfigPanelProps) {
  const { t } = useTranslation(['pages', 'common']);
  const eligibleCount = subjects.filter((s) => s.photo_count > 0).length;

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('pages:recognition.configuration')}</h2>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Confidence slider */}
        <div>
          <label className="block text-sm font-medium text-slate-300 mb-2">
            {t('pages:recognition.minConfidence')}: {confidence}%
          </label>
          <input
            type="range"
            min="50"
            max="95"
            step="1"
            value={confidence}
            onChange={(e) => setConfidence(parseInt(e.target.value))}
            disabled={isScanning}
            className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
          />
          <div className="flex justify-between text-xs text-slate-500 mt-1">
            <span>{t('pages:recognition.moreResults')}</span>
            <span>{t('pages:recognition.betterMatches')}</span>
          </div>
        </div>

        <div className="text-xs text-slate-500">
          {t('pages:recognition.peopleToScan', { count: eligibleCount })}
        </div>

        {/* Scan / Cancel button */}
        {!isScanning ? (
          <Button
            onClick={onScan}
            disabled={subjects.length === 0}
            className="w-full"
          >
            <Search className="h-4 w-4 mr-2" />
            {t('common:buttons.scanAllPeople')}
          </Button>
        ) : (
          <Button
            onClick={onCancel}
            className="w-full bg-red-600 hover:bg-red-700"
          >
            <Square className="h-4 w-4 mr-2" />
            {t('common:buttons.stop')}
          </Button>
        )}

        {/* Progress */}
        {isScanning && (
          <div className="space-y-2">
            <div className="flex justify-between text-xs text-slate-400">
              <span>{scanProgress.current}/{scanProgress.total}</span>
              <span className="truncate ml-2">{scanProgress.currentPerson}</span>
            </div>
            <div className="w-full bg-slate-700 rounded-full h-2">
              <div
                className="bg-blue-500 h-2 rounded-full transition-all"
                style={{ width: `${scanProgress.total ? (scanProgress.current / scanProgress.total) * 100 : 0}%` }}
              />
            </div>
          </div>
        )}

        {/* Error */}
        {scanError && (
          <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
            {scanError}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
