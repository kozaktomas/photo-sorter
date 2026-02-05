import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, AlertCircle, Check, X } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { PhotoCard } from '../../components/PhotoCard';
import { BulkActionBar } from '../../components/BulkActionBar';
import { StatsGrid } from '../../components/StatsGrid';
import { findDuplicates, getAlbums, getConfig } from '../../api/client';
import { usePhotoSelection } from '../../hooks/usePhotoSelection';
import { DEFAULT_DUPLICATE_THRESHOLD, DEFAULT_DUPLICATE_LIMIT, MAX_ALBUMS_FETCH } from '../../constants';
import type { DuplicatesResponse, Config, Album } from '../../types';

export function DuplicatesPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [config, setConfig] = useState<Config | null>(null);

  // Form state
  const [scopeAlbum, setScopeAlbum] = useState('');
  const [threshold, setThreshold] = useState(DEFAULT_DUPLICATE_THRESHOLD);
  const [limit, setLimit] = useState(DEFAULT_DUPLICATE_LIMIT);
  const [availableAlbums, setAvailableAlbums] = useState<Album[]>([]);

  // Results state
  const [result, setResult] = useState<DuplicatesResponse | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Selection state
  const selection = usePhotoSelection();

  // Load albums + config on mount
  useEffect(() => {
    async function loadData() {
      try {
        const [albumsData, configData] = await Promise.all([
          getAlbums({ count: MAX_ALBUMS_FETCH, order: 'name' }),
          getConfig().catch(() => null),
        ]);
        setAvailableAlbums(albumsData);
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load data:', err);
      }
    }
    loadData();
  }, []);

  const handleScan = async () => {
    setIsSearching(true);
    setSearchError(null);
    setResult(null);
    selection.deselectAll();

    try {
      const data = await findDuplicates({
        album_uid: scopeAlbum || undefined,
        threshold: 1 - threshold / 100, // Convert percentage to cosine distance
        limit,
      });
      setResult(data);
    } catch (err) {
      console.error('Duplicate scan failed:', err);
      setSearchError(
        err instanceof Error ? err.message : t('common:errors.searchFailed')
      );
    } finally {
      setIsSearching(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">{t('pages:duplicates.title')}</h1>
        <p className="text-slate-400 mt-1">{t('pages:duplicates.subtitle')}</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Configuration Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:duplicates.configuration')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Scope selector */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:duplicates.scope')}
              </label>
              <select
                value={scopeAlbum}
                onChange={(e) => setScopeAlbum(e.target.value)}
                disabled={isSearching}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              >
                <option value="">{t('pages:duplicates.allPhotos')}</option>
                {availableAlbums.map((album) => (
                  <option key={album.uid} value={album.uid}>
                    {album.title} ({album.photo_count})
                  </option>
                ))}
              </select>
            </div>

            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:duplicates.similarity')}: {threshold}%
              </label>
              <input
                type="range"
                min="80"
                max="99"
                step="1"
                value={threshold}
                onChange={(e) => setThreshold(parseInt(e.target.value))}
                disabled={isSearching}
                className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
              />
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{t('pages:duplicates.moreGroups')}</span>
                <span>{t('pages:duplicates.exactDuplicates')}</span>
              </div>
            </div>

            {/* Limit */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:duplicates.maxGroups')}
              </label>
              <input
                type="number"
                value={limit}
                onChange={(e) => setLimit(parseInt(e.target.value) || DEFAULT_DUPLICATE_LIMIT)}
                disabled={isSearching}
                min={1}
                max={500}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              />
            </div>

            {/* Scan button */}
            <Button
              onClick={handleScan}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('pages:duplicates.scan')}
            </Button>

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
            <h2 className="text-lg font-semibold text-white">{t('pages:duplicates.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:duplicates.clickToScan')}</p>
              </div>
            ) : (
              <StatsGrid
                columns={3}
                items={[
                  { value: result.total_photos_scanned, label: t('pages:duplicates.photosScanned') },
                  { value: result.total_groups, label: t('pages:duplicates.groupsFound'), color: result.total_groups > 0 ? 'yellow' : 'white' },
                  { value: result.total_duplicates, label: t('pages:duplicates.totalDuplicates'), color: result.total_duplicates > 0 ? 'orange' : 'white' },
                ]}
              />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Bulk Action Bar */}
      {selection.selectedPhotos.size > 0 && (
        <BulkActionBar selection={selection} datalistId="duplicates-label-suggestions" showFavorite />
      )}

      {/* Duplicate Groups */}
      {result && result.duplicate_groups && result.duplicate_groups.length > 0 && (
        <div className="space-y-4">
          {result.duplicate_groups.map((group, groupIdx) => (
            <Card key={groupIdx}>
              <CardHeader className="flex flex-row items-center justify-between">
                <h3 className="text-sm font-semibold text-white">
                  {t('pages:duplicates.group')} {groupIdx + 1} ({group.photo_count} {t('pages:labels.photos').toLowerCase()}, {t('pages:duplicates.avgDistance')}: {group.avg_distance.toFixed(3)})
                </h3>
                <div className="flex gap-2">
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => selection.selectAll(group.photos.map(p => p.photo_uid))}
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
              <CardContent>
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3">
                  {group.photos.map((photo) => (
                    <PhotoCard
                      key={photo.photo_uid}
                      photoUid={photo.photo_uid}
                      photoprismDomain={config?.photoprism_domain}
                      matchPercent={Math.round((1 - photo.distance) * 100)}
                      selectable
                      selected={selection.selectedPhotos.has(photo.photo_uid)}
                      onSelectionChange={() => selection.toggleSelection(photo.photo_uid)}
                    />
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {result && (!result.duplicate_groups || result.duplicate_groups.length === 0) && (
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-slate-400">
              <AlertCircle className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>{t('pages:duplicates.noDuplicates')}</p>
              <p className="text-sm mt-2">{t('pages:duplicates.lowerThreshold')}</p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
