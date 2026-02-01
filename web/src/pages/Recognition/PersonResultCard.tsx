import { useTranslation } from 'react-i18next';
import { CheckCheck } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { FaceMatchGrid } from '../../components/PhotoWithBBox';
import type { FaceMatch } from '../../types';

interface PersonResultCardProps {
  name: string;
  actionable: FaceMatch[];
  photoprismDomain?: string;
  isAccepting: boolean;
  acceptProgress: { current: number; total: number };
  onAcceptAll: () => void;
  onApprove: (match: FaceMatch) => Promise<void>;
  onReject: (match: FaceMatch) => void;
  disabled: boolean;
}

export function PersonResultCard({
  name,
  actionable,
  photoprismDomain,
  isAccepting,
  acceptProgress,
  onAcceptAll,
  onApprove,
  onReject,
  disabled,
}: PersonResultCardProps) {
  const { t } = useTranslation('common');

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">
            {name}
            <span className="ml-2 text-sm font-normal text-slate-400">
              ({t('units.match', { count: actionable.length })})
            </span>
          </h2>
          <Button
            onClick={onAcceptAll}
            disabled={disabled}
            className="bg-green-600 hover:bg-green-700 disabled:bg-slate-600"
          >
            <CheckCheck className="h-4 w-4 mr-2" />
            {isAccepting
              ? `${acceptProgress.current}/${acceptProgress.total}`
              : `${t('buttons.acceptAll')} (${actionable.length})`}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <FaceMatchGrid
          matches={actionable}
          photoprismDomain={photoprismDomain}
          onApprove={onApprove}
          onReject={onReject}
        />
      </CardContent>
    </Card>
  );
}
