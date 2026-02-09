import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, AlertCircle, Check, X } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../components/Card';
import { Button } from '../components/Button';
import { Alert } from '../components/Alert';
import { PageHeader } from '../components/PageHeader';
import { PAGE_CONFIGS } from '../constants/pageConfig';
import { PhotoCard } from '../components/PhotoCard';
import { BulkActionBar } from '../components/BulkActionBar';
import { findSimilarToCollection, getConfig, getAlbums, getLabels } from '../api/client';
import { percentToDistance } from '../constants';
import { usePhotoSelection } from '../hooks/usePhotoSelection';
import type { CollectionSimilarResponse, Config, Album, Label } from '../types';

export function ExpandPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [config, setConfig] = useState<Config | null>(null);
  const [isConfigLoaded, setIsConfigLoaded] = useState(false);

  // Form state
  const [sourceType, setSourceType] = useState<'label' | 'album'>('label');
  const [sourceId, setSourceId] = useState('');
  const [threshold, setThreshold] = useState(70);
  const [limit, setLimit] = useState(50);

  // Data for dropdowns
  const [availableLabels, setAvailableLabels] = useState<Label[]>([]);
  const [availableAlbums, setAvailableAlbums] = useState<Album[]>([]);
  const [isLoadingOptions, setIsLoadingOptions] = useState(false);

  // Results state
  const [result, setResult] = useState<CollectionSimilarResponse | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Selection state (shared hook)
  const selection = usePhotoSelection();

  // Load labels and albums for dropdowns on mount
  useEffect(() => {
    loadOptions();
  }, []);

  const loadOptions = async () => {
    setIsLoadingOptions(true);
    try {
      const [labelsData, albumsData] = await Promise.all([
        getLabels({ count: 500, all: true }),
        getAlbums({ count: 500, order: 'name' }),
      ]);
      setAvailableLabels(labelsData);
      setAvailableAlbums(albumsData);
    } catch (err) {
      console.error('Failed to load options:', err);
    } finally {
      setIsLoadingOptions(false);
    }
  };

  // Load config on first search if not loaded
  const ensureConfig = async () => {
    if (!isConfigLoaded) {
      try {
        const configData = await getConfig();
        setConfig(configData);
      } catch {
        // Config is optional, continue without it
      }
      setIsConfigLoaded(true);
    }
  };

  const performSearch = async () => {
    if (!sourceId) return;

    await ensureConfig();

    setIsSearching(true);
    setSearchError(null);
    setResult(null);
    selection.deselectAll();

    try {
      const searchResult = await findSimilarToCollection({
        source_type: sourceType,
        source_id: sourceId,
        threshold: percentToDistance(threshold),
        limit,
      });
      setResult(searchResult);
    } catch (err) {
      console.error('Similar photos search failed:', err);
      setSearchError(
        err instanceof Error ? err.message : 'Search failed. Make sure embeddings are computed.'
      );
    } finally {
      setIsSearching(false);
    }
  };

  // Get display name for source
  const getSourceDisplayName = () => {
    if (!sourceId) return '';
    if (sourceType === 'label') {
      return sourceId;
    } else {
      const album = availableAlbums.find(a => a.uid === sourceId);
      return album?.title || sourceId;
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.expand.icon}
        title={t('pages:expand.title')}
        subtitle={t('pages:expand.subtitle')}
        color="emerald"
        category="tools"
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Configuration Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:expand.search')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Source Type Toggle */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:expand.sourceType')}
              </label>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => { setSourceType('label'); setSourceId(''); }}
                  className={`flex-1 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                    sourceType === 'label'
                      ? 'bg-blue-600 text-white'
                      : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
                  }`}
                >
                  {t('pages:expand.label')}
                </button>
                <button
                  type="button"
                  onClick={() => { setSourceType('album'); setSourceId(''); }}
                  className={`flex-1 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                    sourceType === 'album'
                      ? 'bg-blue-600 text-white'
                      : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
                  }`}
                >
                  {t('pages:expand.album')}
                </button>
              </div>
            </div>

            {/* Source Selector */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {sourceType === 'label' ? t('pages:expand.selectLabel') : t('pages:expand.selectAlbum')}
              </label>
              {isLoadingOptions ? (
                <div className="text-slate-400 text-sm">{t('common:status.loading')}</div>
              ) : (
                <select
                  value={sourceId}
                  onChange={(e) => setSourceId(e.target.value)}
                  disabled={isSearching}
                  className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
                >
                  <option value="">{t('pages:expand.selectSource', { type: sourceType === 'label' ? t('pages:expand.label').toLowerCase() : t('pages:expand.album').toLowerCase() })}</option>
                  {sourceType === 'label'
                    ? availableLabels
                        .filter(l => l.photo_count > 0)
                        .sort((a, b) => b.photo_count - a.photo_count)
                        .map((label) => (
                          <option key={label.uid} value={label.name}>
                            {label.name} ({label.photo_count} {t('pages:labels.photos').toLowerCase()})
                          </option>
                        ))
                    : availableAlbums
                        .filter(a => a.photo_count > 0)
                        .map((album) => (
                          <option key={album.uid} value={album.uid}>
                            {album.title} ({album.photo_count} {t('pages:labels.photos').toLowerCase()})
                          </option>
                        ))
                  }
                </select>
              )}
              <p className="text-xs text-slate-500 mt-1">
                {sourceType === 'label'
                  ? t('pages:expand.labelHelp')
                  : t('pages:expand.albumHelp')}
              </p>
            </div>

            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:expand.minMatch')}: {threshold} %
              </label>
              <input
                type="range"
                min="20"
                max="80"
                step="5"
                value={threshold}
                onChange={(e) => setThreshold(parseInt(e.target.value))}
                disabled={isSearching}
                className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
              />
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{t('pages:expand.moreResults')}</span>
                <span>{t('pages:expand.betterMatches')}</span>
              </div>
            </div>

            {/* Limit */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:expand.limit')}
              </label>
              <input
                type="number"
                value={limit}
                onChange={(e) => setLimit(parseInt(e.target.value) || 50)}
                disabled={isSearching}
                min={1}
                max={200}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              />
            </div>

            {/* Search button */}
            <Button
              onClick={performSearch}
              disabled={!sourceId}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('pages:expand.findSimilar')}
            </Button>

            {/* Error */}
            {searchError && (
              <Alert variant="error">{searchError}</Alert>
            )}
          </CardContent>
        </Card>

        {/* Results Summary */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:expand.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:expand.selectAndFind', { type: sourceType === 'label' ? t('pages:expand.label').toLowerCase() : t('pages:expand.album').toLowerCase() })}</p>
              </div>
            ) : (
              <div className="space-y-4">
                {/* Source info */}
                <div className="p-4 bg-slate-800 rounded-lg">
                  <div className="text-sm text-slate-400 mb-1">{t('pages:expand.source')}</div>
                  <div className="text-white font-medium">
                    {result.source_type === 'label' ? `${t('pages:expand.label')}: ` : `${t('pages:expand.album')}: `}
                    {getSourceDisplayName()}
                  </div>
                </div>

                {/* Summary stats */}
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.source_photo_count}</div>
                    <div className="text-xs text-slate-400">{t('pages:expand.sourcePhotos')}</div>
                  </div>
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.source_embedding_count}</div>
                    <div className="text-xs text-slate-400">{t('pages:expand.withEmbeddings')}</div>
                  </div>
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.min_match_count}</div>
                    <div className="text-xs text-slate-400">{t('pages:expand.minMatches')}</div>
                  </div>
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.count}</div>
                    <div className="text-xs text-slate-400">{t('pages:expand.resultsFound')}</div>
                  </div>
                </div>

                {/* Info */}
                <div className="text-xs text-slate-500 pt-2 border-t border-slate-700">
                  <p>{t('pages:expand.minMatchInfo', { count: result.min_match_count })}</p>
                  <p>{t('pages:expand.sortInfo')}</p>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Photo Grid */}
      {result && result.results && result.results.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <h2 className="text-lg font-semibold text-white">
              {t('pages:expand.similarPhotos')} ({result.results.length})
            </h2>
            <div className="flex gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => selection.selectAll(result.results.map(p => p.photo_uid))}
                disabled={selection.selectedPhotos.size === result.results.length}
              >
                <Check className="h-3 w-3 mr-1" />
                {t('common:buttons.selectAll')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={selection.deselectAll}
                disabled={selection.selectedPhotos.size === 0}
              >
                <X className="h-3 w-3 mr-1" />
                {t('common:buttons.deselect')}
              </Button>
            </div>
          </CardHeader>

          {/* Bulk Action Bar */}
          {selection.selectedPhotos.size > 0 && (
            <div className="mx-4 mb-4">
              <BulkActionBar selection={selection} datalistId="expand-label-suggestions" />
            </div>
          )}

          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
              {result.results.map((photo) => (
                <PhotoCard
                  key={photo.photo_uid}
                  photoUid={photo.photo_uid}
                  photoprismDomain={config?.photoprism_domain}
                  matchPercent={Math.round((1 - photo.distance) * 100)}
                  badge={`${photo.match_count} matches`}
                  thumbnailSize="tile_500"
                  selectable
                  selected={selection.selectedPhotos.has(photo.photo_uid)}
                  onSelectionChange={() => selection.toggleSelection(photo.photo_uid)}
                />
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {result && (!result.results || result.results.length === 0) && (
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-slate-400">
              <AlertCircle className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>{t('pages:expand.noSimilarPhotos')}</p>
              <p className="text-sm mt-2">
                {result.source_embedding_count === 0
                  ? t('pages:expand.noEmbeddingsHelp')
                  : t('pages:expand.tryDifferentSource')}
              </p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
