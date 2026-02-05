import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { CheckCircle } from 'lucide-react';
import { Button } from '../../components/Button';
import { StatsGrid } from '../../components/StatsGrid';

interface CompareSummaryProps {
  totalPairs: number;
  archivedCount: number;
  skippedCount: number;
}

export function CompareSummary({ totalPairs, archivedCount, skippedCount }: CompareSummaryProps) {
  const { t } = useTranslation(['pages']);
  const navigate = useNavigate();

  return (
    <div className="flex flex-col items-center justify-center py-12 space-y-6">
      <CheckCircle className="h-16 w-16 text-green-500" />
      <h2 className="text-2xl font-bold text-white">
        {t('pages:duplicates.compare.allResolved')}
      </h2>

      <StatsGrid
        columns={3}
        items={[
          { value: totalPairs, label: t('pages:duplicates.compare.totalPairs') },
          { value: archivedCount, label: t('pages:duplicates.compare.archived'), color: 'orange' },
          { value: skippedCount, label: t('pages:duplicates.compare.skipped'), color: 'blue' },
        ]}
      />

      <Button onClick={() => navigate('/duplicates')}>
        {t('pages:duplicates.compare.backToDuplicates')}
      </Button>
    </div>
  );
}
