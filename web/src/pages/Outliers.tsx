import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Search, AlertCircle } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../components/Card';
import { Button } from '../components/Button';
import { PageHeader } from '../components/PageHeader';
import { PAGE_CONFIGS } from '../constants/pageConfig';
import { PhotoCard } from '../components/PhotoCard';
import { StatsGrid } from '../components/StatsGrid';
import { FormInput } from '../components/FormInput';
import { FormSelect } from '../components/FormSelect';
import { getSubjects, findFaceOutliers, getConfig, applyFaceMatch } from '../api/client';
import type { Subject, OutlierResponse, OutlierResult, Config } from '../types';

export function OutliersPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Form state
  const [selectedPerson, setSelectedPerson] = useState('');
  const [threshold, setThreshold] = useState(0);
  const [limit, setLimit] = useState(0);

  // Results state
  const [result, setResult] = useState<OutlierResponse | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [unassigned, setUnassigned] = useState<Set<string>>(new Set());

  useEffect(() => {
    async function loadData() {
      try {
        const [subjectsData, configData] = await Promise.all([
          getSubjects({ count: 500 }),
          getConfig(),
        ]);
        setSubjects(subjectsData);
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load subjects:', err);
        setError('Failed to load subjects. Make sure you are logged in.');
      } finally {
        setIsLoading(false);
      }
    }
    loadData();
  }, []);

  const handleAnalyze = async () => {
    if (!selectedPerson) return;

    setIsSearching(true);
    setSearchError(null);
    setResult(null);
    setUnassigned(new Set());

    try {
      const outlierResult = await findFaceOutliers({
        person_name: selectedPerson,
        threshold: threshold / 100, // convert percentage to cosine distance
        limit,
      });
      setResult(outlierResult);
    } catch (err) {
      console.error('Outlier detection failed:', err);
      setSearchError(
        err instanceof Error ? err.message : 'Outlier detection failed. Database may not be configured.'
      );
    } finally {
      setIsSearching(false);
    }
  };

  const handleUnassign = async (outlier: OutlierResult) => {
    if (!outlier.marker_uid) return;

    try {
      const response = await applyFaceMatch({
        photo_uid: outlier.photo_uid,
        person_name: selectedPerson,
        action: 'unassign_person',
        marker_uid: outlier.marker_uid,
        face_index: outlier.face_index,
      });

      if (response.success) {
        setUnassigned((prev) => {
          const next = new Set(prev);
          next.add(`${outlier.photo_uid}-${outlier.face_index}`);
          return next;
        });
      } else {
        alert(`Failed to unassign: ${response.error}`);
      }
    } catch (err) {
      console.error('Failed to unassign face:', err);
      alert('Failed to unassign face');
    }
  };

  if (isLoading) {
    return <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>;
  }

  if (error) {
    return (
      <div className="text-center py-12">
        <AlertCircle className="h-12 w-12 text-red-400 mx-auto mb-4" />
        <p className="text-red-400">{error}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.outliers.icon}
        title={t('pages:outliers.title')}
        subtitle={t('pages:outliers.subtitle')}
        color="orange"
        category="faces"
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Configuration Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:outliers.configuration')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Person selection */}
            <FormSelect
              label={t('pages:outliers.person')}
              value={selectedPerson}
              onChange={(e) => setSelectedPerson(e.target.value)}
              disabled={isSearching}
            >
              <option value="">{t('pages:outliers.selectPerson')}</option>
              {subjects.map((subject) => (
                <option key={subject.uid} value={subject.slug}>
                  {subject.name} ({subject.photo_count} {t('pages:labels.photos').toLowerCase()})
                </option>
              ))}
            </FormSelect>

            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:outliers.minDistance')}: {threshold}%
              </label>
              <input
                type="range"
                min="0"
                max="50"
                step="1"
                value={threshold}
                onChange={(e) => setThreshold(parseInt(e.target.value))}
                disabled={isSearching}
                className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
              />
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{t('pages:outliers.showAll')}</span>
                <span>{t('pages:outliers.onlyExtreme')}</span>
              </div>
            </div>

            {/* Limit */}
            <FormInput
              label={t('pages:outliers.limit')}
              type="number"
              value={limit}
              onChange={(e) => setLimit(parseInt(e.target.value) || 0)}
              disabled={isSearching}
              min={0}
            />

            {/* Analyze button */}
            <Button
              onClick={handleAnalyze}
              disabled={!selectedPerson}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('common:buttons.analyze')}
            </Button>

            {/* Error */}
            {searchError && (
              <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
                {searchError}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Results Summary */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:outliers.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <AlertTriangle className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:outliers.selectAndAnalyze')}</p>
              </div>
            ) : (
              <div className="space-y-4">
                <StatsGrid
                  columns={3}
                  items={[
                    { value: result.total_faces, label: t('pages:outliers.totalFaces') },
                    { value: `${(result.avg_distance * 100).toFixed(1)}%`, label: t('pages:outliers.avgDistance') },
                    { value: result.outliers.length, label: t('pages:outliers.outliersShown'), color: 'orange' },
                    ...(result.missing_embeddings?.length > 0
                      ? [{ value: result.missing_embeddings.length, label: t('pages:outliers.missingEmbeddings'), color: 'red' as const }]
                      : []),
                  ]}
                />

                <div className="text-xs text-slate-500 pt-2 border-t border-slate-700">
                  <p>{t('pages:outliers.explanation')}</p>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Photo Grid */}
      {result && result.outliers.length > 0 && (
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">
              {t('pages:outliers.suspiciousFaces')} ({result.outliers.length})
            </h2>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {result.outliers.map((outlier) => {
                const key = `${outlier.photo_uid}-${outlier.face_index}`;
                const isDone = unassigned.has(key);
                return (
                  <PhotoCard
                    key={key}
                    photoUid={outlier.photo_uid}
                    photoprismDomain={config?.photoprism_domain}
                    matchPercent={Math.round((1 - outlier.dist_from_centroid) * 100)}
                    thumbnailSize="fit_720"
                    aspectRatio="auto"
                    bboxRel={outlier.bbox_rel}
                    bboxPadding={0.3}
                    action={isDone ? 'already_done' : 'unassign_person'}
                    onApprove={isDone ? undefined : () => handleUnassign(outlier)}
                  />
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Missing Embeddings */}
      {result && result.missing_embeddings?.length > 0 && (
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">
              {t('pages:outliers.missingEmbeddingsTitle')} ({result.missing_embeddings.length})
            </h2>
            <p className="text-sm text-slate-400 mt-1">
              {t('pages:outliers.missingEmbeddingsDesc')}
            </p>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              {result.missing_embeddings.map((outlier) => {
                const key = `missing-${outlier.photo_uid}-${outlier.marker_uid}`;
                const isDone = unassigned.has(key);
                return (
                  <PhotoCard
                    key={key}
                    photoUid={outlier.photo_uid}
                    photoprismDomain={config?.photoprism_domain}
                    thumbnailSize="fit_720"
                    aspectRatio="auto"
                    bboxRel={outlier.bbox_rel}
                    bboxPadding={0.3}
                    action={isDone ? 'already_done' : 'create_marker'}
                    onApprove={isDone ? undefined : () => handleUnassign(outlier)}
                  />
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
