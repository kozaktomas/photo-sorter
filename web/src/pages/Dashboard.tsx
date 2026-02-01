import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FolderOpen, Tags, Sparkles, Image, Users, Clock } from 'lucide-react';
import { Card, CardContent } from '../components/Card';
import { getAlbums, getLabels, getConfig, getStats } from '../api/client';
import type { Config, StatsResponse } from '../types';

export function DashboardPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [albumCount, setAlbumCount] = useState<number | null>(null);
  const [labelCount, setLabelCount] = useState<number | null>(null);
  const [config, setConfig] = useState<Config | null>(null);
  const [stats, setStats] = useState<StatsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    async function loadData() {
      try {
        const [albums, labels, configData, statsData] = await Promise.all([
          getAlbums({ count: 1000 }),
          getLabels({ count: 1000, all: true }),
          getConfig(),
          getStats(),
        ]);
        setAlbumCount(albums.length);
        setLabelCount(labels.length);
        setConfig(configData);
        setStats(statsData);
      } catch (err) {
        console.error('Failed to load dashboard data:', err);
      } finally {
        setIsLoading(false);
      }
    }
    loadData();
  }, []);

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-3xl font-bold text-white">{t('pages:dashboard.title')}</h1>
        <p className="text-slate-400 mt-2">{t('pages:dashboard.welcome')}</p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <Link to="/albums">
          <Card className="hover:border-blue-500 transition-colors cursor-pointer">
            <CardContent className="flex items-center space-x-4">
              <div className="p-3 bg-blue-500/10 rounded-lg">
                <FolderOpen className="h-8 w-8 text-blue-500" />
              </div>
              <div>
                <p className="text-slate-400 text-sm">{t('pages:dashboard.albums')}</p>
                <p className="text-2xl font-bold text-white">
                  {isLoading ? '...' : albumCount}
                </p>
              </div>
            </CardContent>
          </Card>
        </Link>

        <Link to="/labels">
          <Card className="hover:border-green-500 transition-colors cursor-pointer">
            <CardContent className="flex items-center space-x-4">
              <div className="p-3 bg-green-500/10 rounded-lg">
                <Tags className="h-8 w-8 text-green-500" />
              </div>
              <div>
                <p className="text-slate-400 text-sm">{t('pages:dashboard.labels')}</p>
                <p className="text-2xl font-bold text-white">
                  {isLoading ? '...' : labelCount}
                </p>
              </div>
            </CardContent>
          </Card>
        </Link>

        <Link to="/similar">
          <Card className="hover:border-purple-500 transition-colors cursor-pointer">
            <CardContent className="flex items-center space-x-4">
              <div className="p-3 bg-purple-500/10 rounded-lg">
                <Image className="h-8 w-8 text-purple-500" />
              </div>
              <div>
                <p className="text-slate-400 text-sm">{t('pages:dashboard.processed')}</p>
                <p className="text-2xl font-bold text-white">
                  {isLoading ? '...' : stats ? `${stats.photos_processed}/${stats.total_photos}` : '0/0'}
                </p>
              </div>
            </CardContent>
          </Card>
        </Link>

        <Link to="/recognition">
          <Card className="hover:border-amber-500 transition-colors cursor-pointer">
            <CardContent className="flex items-center space-x-4">
              <div className="p-3 bg-amber-500/10 rounded-lg">
                <Users className="h-8 w-8 text-amber-500" />
              </div>
              <div>
                <p className="text-slate-400 text-sm">{t('pages:dashboard.faceEmbeddings')}</p>
                <p className="text-2xl font-bold text-white">
                  {isLoading ? '...' : stats?.total_faces ?? 0}
                </p>
              </div>
            </CardContent>
          </Card>
        </Link>

        <Link to="/process">
          <Card className="hover:border-orange-500 transition-colors cursor-pointer">
            <CardContent className="flex items-center space-x-4">
              <div className="p-3 bg-orange-500/10 rounded-lg">
                <Clock className="h-8 w-8 text-orange-500" />
              </div>
              <div>
                <p className="text-slate-400 text-sm">{t('pages:dashboard.waiting')}</p>
                <p className="text-2xl font-bold text-white">
                  {isLoading ? '...' : stats ? stats.total_photos - stats.photos_processed : 0}
                </p>
              </div>
            </CardContent>
          </Card>
        </Link>
      </div>

      {/* Quick actions */}
      <div>
        <h2 className="text-xl font-semibold text-white mb-4">{t('pages:dashboard.quickActions')}</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          <Link to="/albums">
            <Card className="hover:border-blue-500 transition-colors cursor-pointer">
              <CardContent className="flex items-center space-x-3">
                <FolderOpen className="h-5 w-5 text-blue-500" />
                <span className="text-white">{t('pages:dashboard.browseAlbums')}</span>
              </CardContent>
            </Card>
          </Link>

          <Link to="/analyze">
            <Card className="hover:border-purple-500 transition-colors cursor-pointer">
              <CardContent className="flex items-center space-x-3">
                <Sparkles className="h-5 w-5 text-purple-500" />
                <span className="text-white">{t('pages:dashboard.analyzePhotos')}</span>
              </CardContent>
            </Card>
          </Link>

          <Link to="/labels">
            <Card className="hover:border-green-500 transition-colors cursor-pointer">
              <CardContent className="flex items-center space-x-3">
                <Tags className="h-5 w-5 text-green-500" />
                <span className="text-white">{t('pages:dashboard.manageLabels')}</span>
              </CardContent>
            </Card>
          </Link>
        </div>
      </div>

      {/* Provider status */}
      {config && (
        <div>
          <h2 className="text-xl font-semibold text-white mb-4">{t('pages:dashboard.aiProviders')}</h2>
          <Card>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                {config.providers.map((provider) => (
                  <div
                    key={provider.name}
                    className={`flex items-center space-x-2 p-3 rounded-lg ${
                      provider.available ? 'bg-green-500/10' : 'bg-slate-700/50'
                    }`}
                  >
                    <div
                      className={`w-2 h-2 rounded-full ${
                        provider.available ? 'bg-green-500' : 'bg-slate-500'
                      }`}
                    />
                    <span className={provider.available ? 'text-white' : 'text-slate-500'}>
                      {provider.name}
                    </span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
