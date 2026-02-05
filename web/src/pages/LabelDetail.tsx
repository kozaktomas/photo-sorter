import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Pencil, Check, X, Tags, Star } from 'lucide-react';
import { Card, CardContent } from '../components/Card';
import { Button } from '../components/Button';
import { colorMap } from '../constants/pageConfig';
import { getLabel, updateLabel, getPhotos, getThumbnailUrl } from '../api/client';
import { LABEL_PHOTOS_CACHE_KEY } from '../constants';
import type { Label, Photo } from '../types';

export function LabelDetailPage() {
  const { t } = useTranslation(['pages', 'common']);
  const { uid } = useParams<{ uid: string }>();
  const navigate = useNavigate();

  const [label, setLabel] = useState<Label | null>(null);
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Rename state
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    if (!uid) return;
    loadLabel();
  }, [uid]);

  useEffect(() => {
    if (!label) return;
    loadPhotos();
  }, [label?.slug]);

  async function loadLabel() {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getLabel(uid!);
      setLabel(data);
      setEditName(data.name);
    } catch (err) {
      setError('Failed to load label');
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  }

  async function loadPhotos() {
    if (!label) return;
    try {
      const data = await getPhotos({ label: label.slug, count: 60 });
      setPhotos(data);
    } catch (err) {
      console.error('Failed to load photos:', err);
    }
  }

  async function handleSave() {
    if (!uid || !editName.trim()) return;
    setIsSaving(true);
    try {
      const updated = await updateLabel(uid, { name: editName.trim() });
      setLabel(updated);
      setIsEditing(false);
    } catch (err) {
      console.error('Failed to update label:', err);
    } finally {
      setIsSaving(false);
    }
  }

  function handleCancel() {
    setEditName(label?.name || '');
    setIsEditing(false);
  }

  function handlePhotoClick(photo: Photo) {
    if (!label) return;
    // Cache photo UIDs for navigation in photo detail page
    sessionStorage.setItem(
      LABEL_PHOTOS_CACHE_KEY,
      JSON.stringify({ id: label.slug, photoUids: photos.map((p) => p.uid) })
    );
    navigate(`/photos/${photo.uid}?label=${label.slug}`);
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-slate-400">{t('common:status.loading')}</div>
      </div>
    );
  }

  if (error || !label) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" onClick={() => navigate('/labels')}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          {t('pages:labels.backToLabels')}
        </Button>
        <div className="text-red-400">{error || t('common:errors.labelNotFound')}</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Accent line */}
      <div className={`h-0.5 ${colorMap.cyan.gradient} rounded-full`} />
      {/* Header */}
      <div className="flex items-center space-x-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/labels')}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex items-center space-x-3 flex-1">
          <Tags className="h-6 w-6 text-cyan-400" />
          {isEditing ? (
            <div className="flex items-center space-x-2">
              <input
                type="text"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleSave();
                  if (e.key === 'Escape') handleCancel();
                }}
                autoFocus
                className="bg-slate-800 border border-slate-600 rounded px-3 py-1 text-white text-2xl font-bold focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <Button variant="primary" size="sm" onClick={handleSave} isLoading={isSaving}>
                <Check className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="sm" onClick={handleCancel}>
                <X className="h-4 w-4" />
              </Button>
            </div>
          ) : (
            <div className="flex items-center space-x-2">
              <h1 className="text-3xl font-bold text-white">{label.name}</h1>
              <button
                onClick={() => setIsEditing(true)}
                className="text-slate-500 hover:text-slate-300 p-1"
              >
                <Pencil className="h-4 w-4" />
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Details */}
      <Card>
        <CardContent>
          <dl className="grid grid-cols-2 md:grid-cols-3 gap-4">
            <div>
              <dt className="text-sm text-slate-400">{t('pages:labels.slug')}</dt>
              <dd className="text-white mt-1">{label.slug}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:labels.photos')}</dt>
              <dd className="text-white mt-1">{label.photo_count}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:labels.priority')}</dt>
              <dd className="text-white mt-1">{label.priority}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:labels.favorite')}</dt>
              <dd className="mt-1">
                {label.favorite ? (
                  <Star className="h-4 w-4 text-yellow-400 fill-yellow-400" />
                ) : (
                  <span className="text-slate-500">{t('pages:subjectDetail.no')}</span>
                )}
              </dd>
            </div>
            {label.description && (
              <div className="col-span-2">
                <dt className="text-sm text-slate-400">{t('pages:labels.description')}</dt>
                <dd className="text-white mt-1">{label.description}</dd>
              </div>
            )}
            {label.notes && (
              <div className="col-span-2">
                <dt className="text-sm text-slate-400">{t('pages:labels.notes')}</dt>
                <dd className="text-white mt-1">{label.notes}</dd>
              </div>
            )}
            {label.created_at && (
              <div>
                <dt className="text-sm text-slate-400">{t('pages:labels.created')}</dt>
                <dd className="text-white mt-1">
                  {new Date(label.created_at).toLocaleDateString()}
                </dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      {/* Photos */}
      <div>
        <h2 className="text-xl font-semibold text-white mb-4">
          {t('pages:labels.photos')} ({photos.length})
        </h2>
        {photos.length === 0 ? (
          <div className="text-slate-400 text-center py-8">{t('pages:labels.noPhotosWithLabel')}</div>
        ) : (
          <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 gap-2">
            {photos.map((photo) => (
              <button
                key={photo.uid}
                onClick={() => handlePhotoClick(photo)}
                className="aspect-square rounded-lg overflow-hidden bg-slate-800 hover:ring-2 hover:ring-blue-500 transition-all cursor-pointer"
              >
                <img
                  src={getThumbnailUrl(photo.uid, 'tile_224')}
                  alt={photo.title}
                  className="w-full h-full object-cover"
                  loading="lazy"
                />
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
