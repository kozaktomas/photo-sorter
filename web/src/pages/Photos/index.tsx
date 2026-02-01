import { useEffect, useState, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Camera, Loader2 } from 'lucide-react';
import { Card, CardContent } from '../../components/Card';
import { Button } from '../../components/Button';
import { PhotoGrid } from '../../components/PhotoGrid';
import { getLabels, getAlbums, getConfig } from '../../api/client';
import { MAX_LABELS_FETCH, MAX_ALBUMS_FETCH, PHOTOS_CACHE_KEY } from '../../constants';
import { usePhotosFilters } from './hooks/usePhotosFilters';
import { usePhotosPagination } from './hooks/usePhotosPagination';
import { PhotosFilters } from './PhotosFilters';
import type { Photo, Label, Album, Config } from '../../types';

export function PhotosPage() {
  const { t } = useTranslation(['pages', 'common']);
  const navigate = useNavigate();

  // Filter state from hook
  const filters = usePhotosFilters();

  // Pagination state from hook
  const pagination = usePhotosPagination({
    search: filters.search,
    selectedYear: filters.selectedYear,
    selectedLabel: filters.selectedLabel,
    selectedAlbum: filters.selectedAlbum,
    sortBy: filters.sortBy,
    filterKey: filters.filterKey,
  });

  // Dropdown data
  const [labels, setLabels] = useState<Label[]>([]);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [config, setConfig] = useState<Config | null>(null);

  const handlePhotoClick = (photo: Photo) => {
    // Save current state to cache before navigating
    pagination.saveCache();
    navigate(`/photos/${photo.uid}`);
  };

  // Load dropdown data and config
  useEffect(() => {
    async function loadFilterData() {
      try {
        const [labelsData, albumsData, configData] = await Promise.all([
          getLabels({ count: MAX_LABELS_FETCH, all: true }),
          getAlbums({ count: MAX_ALBUMS_FETCH }),
          getConfig().catch(() => null),
        ]);
        setLabels(labelsData.sort((a, b) => a.name.localeCompare(b.name)));
        setAlbums(albumsData.sort((a, b) => a.title.localeCompare(b.title)));
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load filter data:', err);
      }
    }
    loadFilterData();
  }, []);

  // Reload photos when filters change (skip if restored from cache on first render)
  const isFirstRender = useRef(true);
  useEffect(() => {
    if (isFirstRender.current && pagination.restoredFromCache) {
      isFirstRender.current = false;
      return;
    }
    isFirstRender.current = false;
    // Clear cache when filters change (user is browsing, not returning)
    sessionStorage.removeItem(PHOTOS_CACHE_KEY);
    pagination.loadPhotos(true);
  }, [filters.search, filters.selectedYear, filters.selectedLabel, filters.selectedAlbum, filters.sortBy]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white flex items-center gap-2">
            <Camera className="h-8 w-8" />
            {t('pages:photos.title')}
          </h1>
          <p className="text-slate-400 mt-1">
            {t('pages:photos.photosLoaded', { count: pagination.photos.length, more: pagination.hasMore ? '+' : '' })}
          </p>
        </div>
      </div>

      {/* Filters */}
      <PhotosFilters
        search={filters.search}
        setSearch={filters.setSearch}
        selectedYear={filters.selectedYear}
        setSelectedYear={filters.setSelectedYear}
        selectedLabel={filters.selectedLabel}
        setSelectedLabel={filters.setSelectedLabel}
        selectedAlbum={filters.selectedAlbum}
        setSelectedAlbum={filters.setSelectedAlbum}
        sortBy={filters.sortBy}
        setSortBy={filters.setSortBy}
        hasActiveFilters={filters.hasActiveFilters}
        clearFilters={filters.clearFilters}
        labels={labels}
        albums={albums}
      />

      {/* Photo grid */}
      {pagination.isLoading ? (
        <Card>
          <CardContent>
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 text-blue-500 animate-spin" />
              <span className="ml-3 text-slate-400">{t('pages:photos.loadingPhotos')}</span>
            </div>
          </CardContent>
        </Card>
      ) : (
        <>
          <Card>
            <CardContent>
              <PhotoGrid
                photos={pagination.photos}
                onPhotoClick={handlePhotoClick}
                photoprismDomain={config?.photoprism_domain}
              />
            </CardContent>
          </Card>

          {/* Load more button */}
          {pagination.hasMore && pagination.photos.length > 0 && (
            <div className="flex justify-center">
              <Button
                onClick={pagination.handleLoadMore}
                disabled={pagination.isLoadingMore}
                variant="secondary"
              >
                {pagination.isLoadingMore ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    {t('common:status.loading')}
                  </>
                ) : (
                  t('common:buttons.loadMore')
                )}
              </Button>
            </div>
          )}

          {!pagination.hasMore && pagination.photos.length > 0 && (
            <div className="text-center text-slate-500 text-sm">
              {t('pages:photos.allPhotosLoaded', { count: pagination.photos.length })}
            </div>
          )}
        </>
      )}
    </div>
  );
}
