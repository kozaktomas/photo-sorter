import { useTranslation } from 'react-i18next';
import { AlertCircle, ScanFace } from 'lucide-react';
import { Button } from '../../components/Button';
import type { EmbeddingsStatus as EmbeddingsStatusType } from './hooks/usePhotoData';

interface EmbeddingsStatusProps {
  status: EmbeddingsStatusType;
  onCompute: () => void;
  isComputing: boolean;
}

export function EmbeddingsStatus({ status, onCompute, isComputing }: EmbeddingsStatusProps) {
  const { t } = useTranslation('pages');

  if (status !== 'missing') {
    return null;
  }

  return (
    <div className="flex items-center justify-between px-6 py-2 bg-yellow-500/10 border-b border-yellow-500/30 shrink-0">
      <div className="flex items-center gap-2 text-yellow-400 text-sm">
        <AlertCircle className="h-4 w-4" />
        <span>{t('photoDetail.faceDetectionNotRun')}</span>
      </div>
      <Button
        size="sm"
        variant="primary"
        onClick={onCompute}
        isLoading={isComputing}
        disabled={isComputing}
      >
        <ScanFace className="h-4 w-4 mr-1" />
        {t('common:buttons.detectFaces')}
      </Button>
    </div>
  );
}
