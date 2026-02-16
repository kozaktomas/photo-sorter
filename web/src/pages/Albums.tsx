import { useEffect, useState } from 'react';
import { Link, useParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FolderOpen, ArrowLeft, Image, Sparkles, Search, Play } from 'lucide-react';
import { Card, CardContent } from '../components/Card';
import { Button } from '../components/Button';
import { PageHeader } from '../components/PageHeader';
import { PAGE_CONFIGS } from '../constants/pageConfig';
import { PhotoGrid } from '../components/PhotoGrid';
import { getAlbums, getAlbum, getAlbumPhotos, getThumbnailUrl, getConfig } from '../api/client';
import { ALBUM_PHOTOS_CACHE_KEY } from '../constants';
import type { Album, Photo, Config } from '../types';

export function AlbumsPage() {
  const { uid } = useParams<{ uid: string }>();

  if (uid) {
    return <AlbumDetailPage uid={uid} />;
  }

  return <AlbumListPage />;
}

function AlbumListPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    async function loadAlbums() {
      try {
        const data = await getAlbums({ count: 500, q: search || undefined });
        setAlbums(data);
      } catch (err) {
        console.error('Failed to load albums:', err);
      } finally {
        setIsLoading(false);
      }
    }
    void loadAlbums();
  }, [search]);

  const filteredAlbums = albums.filter(
    (album) =>
      album.title.toLowerCase().includes(search.toLowerCase()) ||
      album.description?.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.albums.icon}
        title={t('pages:albums.title')}
        subtitle={t('common:units.album', { count: albums.length })}
        color="blue"
        category="browse"
      />

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-slate-400" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('pages:albums.searchPlaceholder')}
          className="w-full pl-10 pr-4 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
        />
      </div>

      {/* Album grid */}
      {isLoading ? (
        <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {filteredAlbums.map((album) => (
            <Link key={album.uid} to={`/albums/${album.uid}`}>
              <Card className="hover:border-blue-500 transition-colors cursor-pointer overflow-hidden">
                <div className="aspect-video bg-slate-700 relative">
                  {album.thumb ? (
                    <img
                      src={getThumbnailUrl(album.thumb, 'tile_500')}
                      alt={album.title}
                      className="w-full h-full object-cover"
                    />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center">
                      <FolderOpen className="h-12 w-12 text-slate-500" />
                    </div>
                  )}
                </div>
                <CardContent>
                  <h3 className="font-semibold text-white truncate">{album.title}</h3>
                  <div className="flex items-center text-sm text-slate-400 mt-1">
                    <Image className="h-4 w-4 mr-1" />
                    {t('common:units.photo', { count: album.photo_count })}
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}

      {!isLoading && filteredAlbums.length === 0 && (
        <div className="text-center py-12 text-slate-400">
          {t('pages:albums.noAlbumsFound')}
        </div>
      )}
    </div>
  );
}

function AlbumDetailPage({ uid }: { uid: string }) {
  const navigate = useNavigate();
  const { t } = useTranslation(['pages', 'common']);
  const [album, setAlbum] = useState<Album | null>(null);
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const handlePhotoClick = (photo: Photo) => {
    // Cache photo UIDs for navigation in photo detail page
    sessionStorage.setItem(
      ALBUM_PHOTOS_CACHE_KEY,
      JSON.stringify({ id: uid, photoUids: photos.map((p) => p.uid) })
    );
    void navigate(`/photos/${photo.uid}?album=${uid}`);
  };

  useEffect(() => {
    async function loadData() {
      try {
        const [albumData, photosData, configData] = await Promise.all([
          getAlbum(uid),
          getAlbumPhotos(uid, { count: 500 }),
          getConfig().catch(() => null),
        ]);
        setAlbum(albumData);
        setPhotos(photosData);
        setConfig(configData);
      } catch (err) {
        console.error('Failed to load album:', err);
      } finally {
        setIsLoading(false);
      }
    }
    void loadData();
  }, [uid]);

  if (isLoading) {
    return <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>;
  }

  if (!album) {
    return <div className="text-center py-12 text-slate-400">{t('pages:albums.albumNotFound')}</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center space-x-4">
        <Button variant="ghost" onClick={() => navigate('/albums')}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          {t('common:buttons.back')}
        </Button>
      </div>

      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold text-white">{album.title}</h1>
          {album.description && (
            <p className="text-slate-400 mt-1">{album.description}</p>
          )}
          <p className="text-slate-500 mt-2">{t('common:units.photo', { count: photos.length })}</p>
        </div>
        <div className="flex items-center space-x-2">
          <Link to={`/slideshow?album=${uid}`}>
            <Button variant="ghost">
              <Play className="h-4 w-4 mr-2" />
              {t('common:buttons.slideshow')}
            </Button>
          </Link>
          <Link to={`/analyze?album=${uid}`}>
            <Button>
              <Sparkles className="h-4 w-4 mr-2" />
              {t('common:buttons.analyze')}
            </Button>
          </Link>
        </div>
      </div>

      <Card>
        <CardContent>
          <PhotoGrid photos={photos} onPhotoClick={handlePhotoClick} photoprismDomain={config?.photoprism_domain} />
        </CardContent>
      </Card>
    </div>
  );
}
