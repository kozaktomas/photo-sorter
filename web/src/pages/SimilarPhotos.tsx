import { useState, useEffect, useRef } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Search, AlertCircle, Check, X, FolderPlus, Tag, Copy, ExternalLink } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../components/Card';
import { Button } from '../components/Button';
import { PhotoCard } from '../components/PhotoCard';
import { findSimilarPhotos, getThumbnailUrl, getConfig, getAlbums, getLabels, addPhotosToAlbum, batchAddLabels } from '../api/client';
import type { SimilarPhotosResponse, Config, Album, Label } from '../types';

export function SimilarPhotosPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [config, setConfig] = useState<Config | null>(null);
  const [isConfigLoaded, setIsConfigLoaded] = useState(false);
  const hasAutoSearched = useRef(false);

  // Form state
  const [photoUID, setPhotoUID] = useState('');
  const [threshold, setThreshold] = useState(70);
  const [limit, setLimit] = useState(50);

  // Results state
  const [result, setResult] = useState<SimilarPhotosResponse | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Selection state
  const [selectedPhotos, setSelectedPhotos] = useState<Set<string>>(new Set());
  const [albums, setAlbums] = useState<Album[]>([]);
  const [labels, setLabels] = useState<Label[]>([]);
  const [selectedAlbum, setSelectedAlbum] = useState('');
  const [labelInput, setLabelInput] = useState('');
  const [isAddingToAlbum, setIsAddingToAlbum] = useState(false);
  const [isAddingLabel, setIsAddingLabel] = useState(false);
  const [actionMessage, setActionMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

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

  // Auto-fill and auto-search from URL params
  useEffect(() => {
    const photoParam = searchParams.get('photo');
    if (photoParam && !hasAutoSearched.current) {
      setPhotoUID(photoParam);
      hasAutoSearched.current = true;
      // Trigger search after state update
      setTimeout(() => {
        performSearch(photoParam);
      }, 0);
    }
  }, [searchParams]);

  const performSearch = async (uid: string) => {
    if (!uid.trim()) return;

    await ensureConfig();

    setIsSearching(true);
    setSearchError(null);
    setResult(null);

    try {
      const searchResult = await findSimilarPhotos({
        photo_uid: uid.trim(),
        threshold: 1 - threshold / 100,
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

  const handleSearch = () => {
    performSearch(photoUID);
  };

  const handlePhotoClick = (uid: string) => {
    navigate(`/photos/${uid}`);
  };

  const handleOpenInPhotoprism = (uid: string) => {
    if (config?.photoprism_domain) {
      const url = `${config.photoprism_domain}/library/browse?view=cards&order=oldest&q=uid:${uid}`;
      window.open(url, '_blank');
    }
  };

  const handleCopyUID = (uid: string) => {
    navigator.clipboard.writeText(uid);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && photoUID.trim()) {
      handleSearch();
    }
  };

  // Load albums and labels for batch actions
  const loadAlbumsAndLabels = async () => {
    try {
      const [albumsData, labelsData] = await Promise.all([
        getAlbums({ count: 500, order: 'name' }),
        getLabels({ count: 500, all: true }),
      ]);
      setAlbums(albumsData);
      setLabels(labelsData);
    } catch (err) {
      console.error('Failed to load albums/labels:', err);
    }
  };

  // Toggle photo selection
  const toggleSelection = (photoUID: string) => {
    const newSelection = new Set(selectedPhotos);
    if (newSelection.has(photoUID)) {
      newSelection.delete(photoUID);
    } else {
      newSelection.add(photoUID);
    }
    setSelectedPhotos(newSelection);

    // Load albums and labels when first photo is selected
    if (newSelection.size === 1 && albums.length === 0) {
      loadAlbumsAndLabels();
    }
  };

  // Select all photos in results
  const selectAll = () => {
    if (!result?.results) return;
    const allUIDs = new Set(result.results.map((p) => p.photo_uid));
    setSelectedPhotos(allUIDs);
    if (albums.length === 0) {
      loadAlbumsAndLabels();
    }
  };

  // Deselect all photos
  const deselectAll = () => {
    setSelectedPhotos(new Set());
  };

  // Add selected photos to album
  const handleAddToAlbum = async () => {
    if (!selectedAlbum || selectedPhotos.size === 0) return;

    setIsAddingToAlbum(true);
    setActionMessage(null);

    try {
      const result = await addPhotosToAlbum(selectedAlbum, Array.from(selectedPhotos));
      setActionMessage({ type: 'success', text: `Added ${result.added} photos to album` });
      setSelectedPhotos(new Set());
      setSelectedAlbum('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add to album' });
    } finally {
      setIsAddingToAlbum(false);
    }
  };

  // Add label to selected photos
  const handleAddLabel = async () => {
    if (!labelInput.trim() || selectedPhotos.size === 0) return;

    setIsAddingLabel(true);
    setActionMessage(null);

    try {
      const result = await batchAddLabels(Array.from(selectedPhotos), labelInput.trim());
      if (result.errors && result.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${result.updated} photos, ${result.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Added label to ${result.updated} photos` });
      }
      setSelectedPhotos(new Set());
      setLabelInput('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add label' });
    } finally {
      setIsAddingLabel(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold text-white">{t('pages:similar.title')}</h1>
        <p className="text-slate-400 mt-1">{t('pages:similar.subtitle')}</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Configuration Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:similar.search')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Photo UID input */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:similar.photoUid')}
              </label>
              <input
                type="text"
                value={photoUID}
                onChange={(e) => setPhotoUID(e.target.value)}
                onKeyDown={handleKeyDown}
                disabled={isSearching}
                placeholder={t('pages:similar.photoUidPlaceholder')}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              />
              <p className="text-xs text-slate-500 mt-1">
                {t('pages:similar.photoUidHelp')}
              </p>
            </div>

            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:similar.minMatch')}: {threshold} %
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
                <span>{t('pages:similar.moreResults')}</span>
                <span>{t('pages:similar.betterMatches')}</span>
              </div>
            </div>

            {/* Limit */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:similar.limit')}
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
              onClick={handleSearch}
              disabled={!photoUID.trim()}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('pages:similar.findSimilar')}
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
            <h2 className="text-lg font-semibold text-white">{t('pages:similar.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:similar.enterPhotoUid')}</p>
              </div>
            ) : (
              <div className="space-y-4">
                {/* Source photo */}
                <div className="flex items-center gap-4 p-4 bg-slate-800 rounded-lg">
                  <img
                    src={getThumbnailUrl(result.source_photo_uid, 'tile_100')}
                    alt="Source"
                    className="w-16 h-16 object-cover rounded cursor-pointer hover:opacity-80"
                    onClick={() => handlePhotoClick(result.source_photo_uid)}
                  />
                  <div className="flex-1">
                    <div className="text-sm text-slate-400">{t('pages:similar.sourcePhoto')}</div>
                    <div className="text-white font-mono text-sm">{result.source_photo_uid}</div>
                  </div>
                  <div className="flex gap-2">
                    <button
                      onClick={() => handleCopyUID(result.source_photo_uid)}
                      className="p-2 text-slate-400 hover:text-white transition-colors"
                      title={t('common:buttons.copyUid')}
                    >
                      <Copy className="h-4 w-4" />
                    </button>
                    {config?.photoprism_domain && (
                      <button
                        onClick={() => handleOpenInPhotoprism(result.source_photo_uid)}
                        className="p-2 text-slate-400 hover:text-white transition-colors"
                        title="Open in PhotoPrism"
                      >
                        <ExternalLink className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                </div>

                {/* Summary stats */}
                <div className="grid grid-cols-2 gap-4">
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.count}</div>
                    <div className="text-xs text-slate-400">{t('pages:similar.similarPhotosFound')}</div>
                  </div>
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.threshold.toFixed(2)}</div>
                    <div className="text-xs text-slate-400">{t('pages:similar.distanceThreshold')}</div>
                  </div>
                </div>

                {/* Info */}
                <div className="text-xs text-slate-500 pt-2 border-t border-slate-700">
                  <p>{t('pages:similar.distanceInfo')}</p>
                  <p>{t('pages:similar.similarityInfo')}</p>
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
              {t('pages:similar.similarPhotos')} ({result.results.length})
            </h2>
            <div className="flex gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={selectAll}
                disabled={selectedPhotos.size === result.results.length}
              >
                <Check className="h-3 w-3 mr-1" />
                {t('common:buttons.selectAll')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={deselectAll}
                disabled={selectedPhotos.size === 0}
              >
                <X className="h-3 w-3 mr-1" />
                {t('common:buttons.deselect')}
              </Button>
            </div>
          </CardHeader>

          {/* Action Panel - shown when photos are selected */}
          {selectedPhotos.size > 0 && (
            <div className="mx-4 mb-4 p-4 bg-blue-500/10 border border-blue-500/20 rounded-lg">
              <div className="flex flex-wrap items-center gap-4">
                <span className="text-blue-400 font-medium">
                  {t('common:units.selected', { count: selectedPhotos.size })}
                </span>

                {/* Add to Album */}
                <div className="flex items-center gap-2">
                  <select
                    value={selectedAlbum}
                    onChange={(e) => setSelectedAlbum(e.target.value)}
                    className="px-3 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="">{t('pages:similar.selectAlbum')}</option>
                    {albums.map((album) => (
                      <option key={album.uid} value={album.uid}>
                        {album.title}
                      </option>
                    ))}
                  </select>
                  <Button
                    size="sm"
                    onClick={handleAddToAlbum}
                    disabled={!selectedAlbum || isAddingToAlbum}
                    isLoading={isAddingToAlbum}
                  >
                    <FolderPlus className="h-3 w-3 mr-1" />
                    {t('common:buttons.addToAlbum')}
                  </Button>
                </div>

                {/* Add Label */}
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    value={labelInput}
                    onChange={(e) => setLabelInput(e.target.value)}
                    placeholder={t('pages:similar.enterLabel')}
                    list="label-suggestions"
                    className="px-3 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
                  />
                  <datalist id="label-suggestions">
                    {labels.map((label) => (
                      <option key={label.uid} value={label.name} />
                    ))}
                  </datalist>
                  <Button
                    size="sm"
                    onClick={handleAddLabel}
                    disabled={!labelInput.trim() || isAddingLabel}
                    isLoading={isAddingLabel}
                  >
                    <Tag className="h-3 w-3 mr-1" />
                    {t('common:buttons.addLabel')}
                  </Button>
                </div>

                {/* Clear selection */}
                <button
                  onClick={deselectAll}
                  className="ml-auto text-slate-400 hover:text-white transition-colors"
                  title={t('common:buttons.deselect')}
                >
                  <X className="h-4 w-4" />
                </button>
              </div>

              {/* Action message */}
              {actionMessage && (
                <div
                  className={`mt-3 text-sm ${
                    actionMessage.type === 'success' ? 'text-green-400' : 'text-red-400'
                  }`}
                >
                  {actionMessage.text}
                </div>
              )}
            </div>
          )}

          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
              {result.results.map((photo) => (
                <PhotoCard
                  key={photo.photo_uid}
                  photoUid={photo.photo_uid}
                  photoprismDomain={config?.photoprism_domain}
                  matchPercent={Math.round((1 - photo.distance) * 100)}
                  thumbnailSize="tile_500"
                  selectable
                  selected={selectedPhotos.has(photo.photo_uid)}
                  onSelectionChange={() => toggleSelection(photo.photo_uid)}
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
              <p>{t('pages:similar.noSimilarFound')}</p>
              <p className="text-sm mt-2">{t('pages:similar.increaseThreshold')}</p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
