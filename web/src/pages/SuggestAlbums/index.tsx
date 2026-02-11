import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, AlertCircle, FolderPlus } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { Alert } from '../../components/Alert';
import { PageHeader } from '../../components/PageHeader';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { PhotoCard } from '../../components/PhotoCard';
import { StatsGrid } from '../../components/StatsGrid';
import { suggestAlbums, addPhotosToAlbum, getConfig } from '../../api/client';
import { DEFAULT_SUGGEST_ALBUM_THRESHOLD, DEFAULT_SUGGEST_ALBUM_TOP_K } from '../../constants';
import type { SuggestAlbumsResponse, Config } from '../../types';

export function SuggestAlbumsPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [config, setConfig] = useState<Config | null>(null);

  // Form state
  const [threshold, setThreshold] = useState(DEFAULT_SUGGEST_ALBUM_THRESHOLD);
  const [topK, setTopK] = useState(DEFAULT_SUGGEST_ALBUM_TOP_K);

  // Results state
  const [result, setResult] = useState<SuggestAlbumsResponse | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Add-to-album state
  const [addingAlbum, setAddingAlbum] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<{ type: 'success' | 'error'; text: string; albumUid?: string } | null>(null);

  // Load config on mount
  useEffect(() => {
    void getConfig().then(setConfig).catch(() => null);
  }, []);

  const handleSuggest = async () => {
    setIsSearching(true);
    setSearchError(null);
    setResult(null);
    setActionMessage(null);

    try {
      const data = await suggestAlbums({
        threshold: threshold / 100, // Convert percentage to cosine similarity
        top_k: topK,
      });
      setResult(data);
    } catch (err) {
      console.error('Album completion failed:', err);
      setSearchError(
        err instanceof Error ? err.message : t('common:errors.searchFailed')
      );
    } finally {
      setIsSearching(false);
    }
  };

  const handleAddAllToAlbum = async (albumUid: string, photoUids: string[]) => {
    setAddingAlbum(albumUid);
    setActionMessage(null);

    try {
      const result = await addPhotosToAlbum(albumUid, photoUids);
      setActionMessage({
        type: 'success',
        text: `Added ${result.added} photos to album`,
        albumUid,
      });
    } catch (err) {
      setActionMessage({
        type: 'error',
        text: err instanceof Error ? err.message : 'Failed to add photos',
        albumUid,
      });
    } finally {
      setAddingAlbum(null);
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.suggestAlbums.icon}
        title={t('pages:suggestAlbums.title')}
        subtitle={t('pages:suggestAlbums.subtitle')}
        color="green"
        category="tools"
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Configuration Panel */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:suggestAlbums.configuration')}</h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Threshold slider */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:suggestAlbums.minSimilarity')}: {threshold}%
              </label>
              <input
                type="range"
                min="50"
                max="90"
                step="5"
                value={threshold}
                onChange={(e) => setThreshold(parseInt(e.target.value))}
                disabled={isSearching}
                className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
              />
              <div className="flex justify-between text-xs text-slate-500 mt-1">
                <span>{t('pages:suggestAlbums.moreResults')}</span>
                <span>{t('pages:suggestAlbums.betterMatches')}</span>
              </div>
            </div>

            {/* Top K */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:suggestAlbums.topK')}
              </label>
              <input
                type="number"
                value={topK}
                onChange={(e) => setTopK(parseInt(e.target.value) || DEFAULT_SUGGEST_ALBUM_TOP_K)}
                disabled={isSearching}
                min={1}
                max={50}
                className="w-full px-4 py-2 bg-slate-900 border border-slate-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
              />
            </div>

            {/* Scan button */}
            <Button
              onClick={handleSuggest}
              disabled={isSearching}
              isLoading={isSearching}
              className="w-full"
            >
              <Search className="h-4 w-4 mr-2" />
              {t('pages:suggestAlbums.suggest')}
            </Button>

            {searchError && (
              <Alert variant="error">{searchError}</Alert>
            )}
          </CardContent>
        </Card>

        {/* Results Summary */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">{t('pages:suggestAlbums.results')}</h2>
          </CardHeader>
          <CardContent>
            {!result ? (
              <div className="text-center py-8 text-slate-400">
                <Search className="h-12 w-12 mx-auto mb-4 opacity-50" />
                <p>{t('pages:suggestAlbums.clickToSuggest')}</p>
              </div>
            ) : (
              <StatsGrid
                columns={3}
                items={[
                  { value: result.albums_analyzed, label: t('pages:suggestAlbums.albumsAnalyzed') },
                  { value: result.photos_analyzed, label: t('pages:suggestAlbums.photosAnalyzed') },
                  { value: result.skipped, label: t('pages:suggestAlbums.skipped'), color: result.skipped > 0 ? 'yellow' : 'white' },
                ]}
              />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Suggested Albums */}
      {result?.suggestions && result.suggestions.length > 0 && (
        <div className="space-y-4">
          {result.suggestions.map((suggestion) => (
            <Card key={suggestion.album_uid}>
              <CardHeader className="flex flex-row items-center justify-between">
                <div>
                  <h3 className="text-lg font-semibold text-white">{suggestion.album_title}</h3>
                  <p className="text-sm text-slate-400">
                    {suggestion.photos.length} {t('pages:suggestAlbums.matchingPhotos').toLowerCase()}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    onClick={() => void handleAddAllToAlbum(suggestion.album_uid, suggestion.photos.map(p => p.photo_uid))}
                    disabled={addingAlbum === suggestion.album_uid}
                    isLoading={addingAlbum === suggestion.album_uid}
                  >
                    <FolderPlus className="h-3 w-3 mr-1" />
                    {t('pages:suggestAlbums.addAllToAlbum')}
                  </Button>
                </div>
              </CardHeader>

              {/* Action message for this album */}
              {actionMessage?.albumUid === suggestion.album_uid && (
                <div className={`mx-4 mb-2 text-sm ${actionMessage.type === 'success' ? 'text-green-400' : 'text-red-400'}`}>
                  {actionMessage.text}
                </div>
              )}

              <CardContent>
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-3">
                  {suggestion.photos.map((photo) => (
                    <PhotoCard
                      key={photo.photo_uid}
                      photoUid={photo.photo_uid}
                      photoprismDomain={config?.photoprism_domain}
                      matchPercent={Math.round(photo.similarity * 100)}
                    />
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {result && (!result.suggestions || result.suggestions.length === 0) && (
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-slate-400">
              <AlertCircle className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>{t('pages:suggestAlbums.noSuggestions')}</p>
              <p className="text-sm mt-2">{t('pages:suggestAlbums.lowerThreshold')}</p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
