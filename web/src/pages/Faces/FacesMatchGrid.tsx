import { useTranslation } from 'react-i18next';
import { CheckCheck } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { FaceMatchGrid } from '../../components/PhotoWithBBox';
import type { FaceMatch, MatchAction } from '../../types';

export type FilterTab = 'all' | MatchAction;

interface FilterTabConfig {
  key: FilterTab;
  label: string;
  count?: number;
}

interface FacesMatchGridCardProps {
  matches: FaceMatch[];
  summary: { create_marker: number; assign_person: number; already_done: number };
  photoprismDomain?: string;
  activeFilter: FilterTab;
  setActiveFilter: (filter: FilterTab) => void;
  actionableCount: number;
  isAcceptingAll: boolean;
  acceptAllProgress: { current: number; total: number };
  onAcceptAll: () => void;
  onApprove: (match: FaceMatch) => Promise<void>;
  onReject: (match: FaceMatch) => void;
  onPhotoClick: (match: { photo_uid: string }) => void;
}

export function FacesMatchGridCard({
  matches,
  summary,
  photoprismDomain,
  activeFilter,
  setActiveFilter,
  actionableCount,
  isAcceptingAll,
  acceptAllProgress,
  onAcceptAll,
  onApprove,
  onReject,
  onPhotoClick,
}: FacesMatchGridCardProps) {
  const { t } = useTranslation(['pages', 'common']);

  if (matches.length === 0) return null;

  const filterTabs: FilterTabConfig[] = [
    { key: 'all', label: t('pages:faces.allActions'), count: matches.length },
    { key: 'create_marker', label: t('pages:faces.createMarker'), count: summary.create_marker },
    { key: 'assign_person', label: t('pages:faces.assignPerson'), count: summary.assign_person },
    { key: 'already_done', label: t('pages:faces.alreadyDone'), count: summary.already_done },
  ];

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">
            {t('pages:faces.matchesFound')} ({matches.length})
          </h2>
          {/* Filter tabs and Accept All button */}
          <div className="flex items-center gap-3">
            <div className="flex gap-1">
              {filterTabs.map((tab) => (
                <button
                  key={tab.key}
                  onClick={() => setActiveFilter(tab.key)}
                  disabled={isAcceptingAll}
                  className={`px-3 py-1.5 text-sm rounded-lg transition-colors ${
                    activeFilter === tab.key
                      ? 'bg-blue-500 text-white'
                      : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
                  } disabled:opacity-50`}
                >
                  {tab.label}
                  {tab.count !== undefined && (
                    <span className="ml-1.5 text-xs opacity-75">({tab.count})</span>
                  )}
                </button>
              ))}
            </div>
            {/* Accept All button */}
            <Button
              onClick={onAcceptAll}
              disabled={actionableCount === 0 || isAcceptingAll}
              className="bg-green-600 hover:bg-green-700 disabled:bg-slate-600"
            >
              <CheckCheck className="h-4 w-4 mr-2" />
              {isAcceptingAll
                ? `${acceptAllProgress.current}/${acceptAllProgress.total}`
                : `${t('common:buttons.acceptAll')} (${actionableCount})`}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <FaceMatchGrid
          matches={matches}
          filter={activeFilter}
          photoprismDomain={photoprismDomain}
          onApprove={onApprove}
          onReject={onReject}
          onPhotoClick={onPhotoClick}
        />
      </CardContent>
    </Card>
  );
}
