import { useTranslation } from 'react-i18next';
import { ShieldCheck } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { StatsGrid } from '../../components/StatsGrid';

interface ScanResultsSummaryProps {
  resultsCount: number;
  totalActionable: number;
  totalAlreadyDone: number;
  isScanning: boolean;
}

export function ScanResultsSummary({
  resultsCount,
  totalActionable,
  totalAlreadyDone,
  isScanning,
}: ScanResultsSummaryProps) {
  const { t } = useTranslation('pages');

  return (
    <Card className="lg:col-span-2">
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('recognition.resultsSummary')}</h2>
      </CardHeader>
      <CardContent>
        {resultsCount === 0 && !isScanning ? (
          <div className="text-center py-8 text-slate-400">
            <ShieldCheck className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>{t('recognition.clickToScan')}</p>
          </div>
        ) : (
          <StatsGrid
            columns={3}
            items={[
              { value: totalActionable, label: t('recognition.actionable'), color: 'blue' },
              { value: totalAlreadyDone, label: t('recognition.alreadyDone'), color: 'green' },
              { value: resultsCount, label: t('recognition.peopleWithMatches') },
            ]}
          />
        )}
      </CardContent>
    </Card>
  );
}
