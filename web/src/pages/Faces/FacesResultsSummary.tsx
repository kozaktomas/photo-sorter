import { useTranslation } from 'react-i18next';
import { Users, Plus, UserCheck, CheckCircle } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { StatsGrid } from '../../components/StatsGrid';
import type { FaceMatchResult } from '../../types';

interface FacesResultsSummaryProps {
  result: FaceMatchResult | null;
}

export function FacesResultsSummary({ result }: FacesResultsSummaryProps) {
  const { t } = useTranslation('pages');

  return (
    <Card className="lg:col-span-2">
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('faces.results')}</h2>
      </CardHeader>
      <CardContent>
        {!result ? (
          <div className="text-center py-8 text-slate-400">
            <Users className="h-12 w-12 mx-auto mb-4 opacity-50" />
            <p>{t('faces.selectPersonToSearch')}</p>
          </div>
        ) : (
          <div className="space-y-4">
            {/* Summary stats */}
            <StatsGrid
              columns={4}
              items={[
                { value: result.source_photos, label: t('faces.sourcePhotos') },
                { value: result.source_faces, label: t('faces.sourceFaces') },
                { value: result.matches.length, label: t('faces.matchesFound') },
                { value: result.summary.already_done, label: t('faces.alreadyDone'), color: 'green' },
              ]}
            />

            {/* Action breakdown */}
            <div className="grid grid-cols-3 gap-4 pt-2">
              <div className="flex items-center gap-2 text-sm">
                <div className="w-3 h-3 rounded bg-red-500" />
                <span className="text-slate-300">{t('faces.newFaces')}:</span>
                <span className="text-white font-medium">{result.summary.create_marker}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <div className="w-3 h-3 rounded bg-yellow-500" />
                <span className="text-slate-300">{t('faces.assign')}:</span>
                <span className="text-white font-medium">{result.summary.assign_person}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <div className="w-3 h-3 rounded bg-green-500" />
                <span className="text-slate-300">{t('faces.done')}:</span>
                <span className="text-white font-medium">{result.summary.already_done}</span>
              </div>
            </div>

            {/* Legend */}
            <div className="text-xs text-slate-500 pt-2 border-t border-slate-700">
              <div className="flex flex-wrap gap-4">
                <span className="flex items-center gap-1">
                  <Plus className="h-3 w-3 text-red-500" />
                  {t('faces.legendNew')}
                </span>
                <span className="flex items-center gap-1">
                  <UserCheck className="h-3 w-3 text-yellow-500" />
                  {t('faces.legendAssign')}
                </span>
                <span className="flex items-center gap-1">
                  <CheckCircle className="h-3 w-3 text-green-500" />
                  {t('faces.legendDone')}
                </span>
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
