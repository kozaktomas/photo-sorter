import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, AlertCircle, Check, X, FolderPlus, Tag } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../components/Card';
import { Button } from '../components/Button';
import { PhotoCard } from '../components/PhotoCard';
import { searchByText, getConfig, getAlbums, getLabels, addPhotosToAlbum, batchAddLabels } from '../api/client';
import type { TextSearchResponse, Config, Album, Label } from '../types';

export function TextSearchPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [config, setConfig] = useState<Config | null>(null);
  const [isConfigLoaded, setIsConfigLoaded] = useState(false);

  // Form state
  const [text, setText] = useState('');
  const [threshold, setThreshold] = useState(50);
  const [limit, setLimit] = useState(50);

  // Results state
  const [result, setResult] = useState<TextSearchResponse | null>(null);
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

  const ensureConfig = async () => {
    if (!isConfigLoaded) {
      try {
        const configData = await getConfig();
        setConfig(configData);
      } catch {
        // Config is optional
      }
      setIsConfigLoaded(true);
    }
  };

  const handleSearch = async () => {
    if (!text.trim()) return;

    await ensureConfig();

    setIsSearching(true);
    setSearchError(null);
    setResult(null);
    setSelectedPhotos(new Set());
    setActionMessage(null);

    try {
      const searchResult = await searchByText({
        text: text.trim(),
        threshold: 1 - threshold / 100,
        limit,
      });
      setResult(searchResult);
    } catch (err) {
      console.error('Text search failed:', err);
      setSearchError(
        err instanceof Error ? err.message : 'Search failed. Make sure embeddings are computed.'
      );
    } finally {
      setIsSearching(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && text.trim()) {
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

    if (newSelection.size === 1 && albums.length === 0) {
      loadAlbumsAndLabels();
    }
  };

  const selectAll = () => {
    if (!result?.results) return;
    const allUIDs = new Set(result.results.map((p) => p.photo_uid));
    setSelectedPhotos(allUIDs);
    if (albums.length === 0) {
      loadAlbumsAndLabels();
    }
  };

  const deselectAll = () => {
    setSelectedPhotos(new Set());
  };

  const handleAddToAlbum = async () => {
    if (!selectedAlbum || selectedPhotos.size === 0) return;

    setIsAddingToAlbum(true);
    setActionMessage(null);

    try {
      const res = await addPhotosToAlbum(selectedAlbum, Array.from(selectedPhotos));
      setActionMessage({ type: 'success', text: `Added ${res.added} photos to album` });
      setSelectedPhotos(new Set());
      setSelectedAlbum('');
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to add to album' });
    } finally {
      setIsAddingToAlbum(false);
    }
  };

  const handleAddLabel = async () => {
    if (!labelInput.trim() || selectedPhotos.size === 0) return;

    setIsAddingLabel(true);
    setActionMessage(null);

    try {
      const res = await batchAddLabels(Array.from(selectedPhotos), labelInput.trim());
      if (res.errors && res.errors.length > 0) {
        setActionMessage({ type: 'error', text: `Updated ${res.updated} photos, ${res.errors.length} errors` });
      } else {
        setActionMessage({ type: 'success', text: `Added label to ${res.updated} photos` });
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
        <h1 className="text-3xl font-bold text-white">{t('pages:textSearch.title')}</h1>
        <p className="text-slate-400 mt-1">{t('pages:textSearch.subtitle')}</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Search Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:textSearch.search')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Text input */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:textSearch.query')}
              </label>
              <input
                type="text"
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={handleKeyDown}
                disabled={isSearching}
                placeholder={t('pages:textSearch.queryPlaceholder')}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              />
              <p className="text-xs text-slate-500 mt-1">
                {t('pages:textSearch.queryHelp')}
              </p>
            </div>

            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:textSearch.minMatch')}: {threshold} %
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
                <span>{t('pages:textSearch.moreResults')}</span>
                <span>{t('pages:textSearch.betterMatches')}</span>
              </div>
            </div>

            {/* Limit */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:textSearch.limit')}
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
              disabled={!text.trim()}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('common:buttons.search')}
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
            <h2 className="text-lg font-semibold text-white">{t('pages:textSearch.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:textSearch.enterQuery')}</p>
              </div>
            ) : (
              <div className="space-y-4">
                {/* Query info */}
                <div className="p-4 bg-slate-800 rounded-lg">
                  <div className="text-sm text-slate-400">{t('pages:textSearch.queryLabel')}</div>
                  <div className="text-white font-medium mt-1">{result.query}</div>
                  {result.translated_query && (
                    <>
                      <div className="text-sm text-slate-400 mt-3">{t('pages:textSearch.clipQuery')}</div>
                      <div className="text-slate-200 text-sm mt-1">
                        {result.translated_query}
                        {result.translate_cost_usd != null && result.translate_cost_usd > 0 && (
                          <span className="text-slate-500 ml-2">
                            ({(result.translate_cost_usd * 24).toFixed(4)} Kč)
                          </span>
                        )}
                      </div>
                    </>
                  )}
                </div>

                {/* Summary stats */}
                <div className="grid grid-cols-2 gap-4">
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.count}</div>
                    <div className="text-xs text-slate-400">{t('pages:textSearch.photosFound')}</div>
                  </div>
                  <div className="bg-slate-800 rounded-lg p-4 text-center">
                    <div className="text-2xl font-bold text-white">{result.threshold.toFixed(2)}</div>
                    <div className="text-xs text-slate-400">{t('pages:textSearch.distanceThreshold')}</div>
                  </div>
                </div>

                <div className="text-xs text-slate-500 pt-2 border-t border-slate-700">
                  <p>{t('pages:textSearch.similarityInfo')}</p>
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
              {t('pages:textSearch.matchingPhotos')} ({result.results.length})
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
                    list="text-search-label-suggestions"
                    className="px-3 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
                  />
                  <datalist id="text-search-label-suggestions">
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
                  matchPercent={Math.round(photo.similarity * 100)}
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
              <p>{t('pages:textSearch.noMatchingPhotos')}</p>
              <p className="text-sm mt-2">{t('pages:textSearch.tryDifferentQuery')}</p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
