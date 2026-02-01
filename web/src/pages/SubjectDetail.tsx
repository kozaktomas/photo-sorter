import { useState, useEffect } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Pencil, Check, X, User, Star } from 'lucide-react';
import { Card, CardContent } from '../components/Card';
import { Button } from '../components/Button';
import { getSubject, updateSubject, getPhotos, getThumbnailUrl } from '../api/client';
import type { Subject, Photo } from '../types';

export function SubjectDetailPage() {
  const { t } = useTranslation(['pages', 'common']);
  const { uid } = useParams<{ uid: string }>();
  const navigate = useNavigate();

  const [subject, setSubject] = useState<Subject | null>(null);
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Rename state
  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    if (!uid) return;
    loadSubject();
  }, [uid]);

  useEffect(() => {
    if (!subject) return;
    loadPhotos();
  }, [subject?.slug]);

  async function loadSubject() {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getSubject(uid!);
      setSubject(data);
      setEditName(data.name);
    } catch (err) {
      setError('Failed to load subject');
      console.error(err);
    } finally {
      setIsLoading(false);
    }
  }

  async function loadPhotos() {
    if (!subject) return;
    try {
      const data = await getPhotos({ q: `person:${subject.slug}`, count: 60 });
      setPhotos(data);
    } catch (err) {
      console.error('Failed to load photos:', err);
    }
  }

  async function handleSave() {
    if (!uid || !editName.trim()) return;
    setIsSaving(true);
    try {
      const updated = await updateSubject(uid, { name: editName.trim() });
      setSubject(updated);
      setIsEditing(false);
    } catch (err) {
      console.error('Failed to update subject:', err);
    } finally {
      setIsSaving(false);
    }
  }

  function handleCancel() {
    setEditName(subject?.name || '');
    setIsEditing(false);
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-slate-400">{t('common:status.loading')}</div>
      </div>
    );
  }

  if (error || !subject) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          {t('common:buttons.back')}
        </Button>
        <div className="text-red-400">{error || t('common:errors.failedToLoad')}</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center space-x-4">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex items-center space-x-3 flex-1">
          <div className="h-10 w-10 rounded-full bg-slate-700 flex items-center justify-center shrink-0">
            <User className="h-5 w-5 text-slate-400" />
          </div>
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
              <h1 className="text-3xl font-bold text-white">{subject.name}</h1>
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
              <dt className="text-sm text-slate-400">{t('pages:subjectDetail.slug')}</dt>
              <dd className="text-white mt-1">{subject.slug}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:subjectDetail.photos')}</dt>
              <dd className="text-white mt-1">{subject.photo_count}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:subjectDetail.favorite')}</dt>
              <dd className="mt-1">
                {subject.favorite ? (
                  <Star className="h-4 w-4 text-yellow-400 fill-yellow-400" />
                ) : (
                  <span className="text-slate-500">{t('pages:subjectDetail.no')}</span>
                )}
              </dd>
            </div>
            {subject.alias && (
              <div>
                <dt className="text-sm text-slate-400">Alias</dt>
                <dd className="text-white mt-1">{subject.alias}</dd>
              </div>
            )}
            {subject.about && (
              <div className="col-span-2">
                <dt className="text-sm text-slate-400">About</dt>
                <dd className="text-white mt-1">{subject.about}</dd>
              </div>
            )}
            {subject.bio && (
              <div className="col-span-2">
                <dt className="text-sm text-slate-400">Bio</dt>
                <dd className="text-white mt-1">{subject.bio}</dd>
              </div>
            )}
            {subject.notes && (
              <div className="col-span-2">
                <dt className="text-sm text-slate-400">{t('pages:subjectDetail.notes')}</dt>
                <dd className="text-white mt-1">{subject.notes}</dd>
              </div>
            )}
            <div>
              <dt className="text-sm text-slate-400">{t('pages:subjectDetail.hidden')}</dt>
              <dd className="text-white mt-1">{subject.hidden ? t('pages:subjectDetail.yes') : t('pages:subjectDetail.no')}</dd>
            </div>
            <div>
              <dt className="text-sm text-slate-400">{t('pages:subjectDetail.excluded')}</dt>
              <dd className="text-white mt-1">{subject.excluded ? t('pages:subjectDetail.yes') : t('pages:subjectDetail.no')}</dd>
            </div>
            {subject.created_at && (
              <div>
                <dt className="text-sm text-slate-400">{t('pages:labels.created')}</dt>
                <dd className="text-white mt-1">
                  {new Date(subject.created_at).toLocaleDateString()}
                </dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      {/* Photos */}
      <div>
        <h2 className="text-xl font-semibold text-white mb-4">
          {t('pages:subjectDetail.photos')} ({photos.length})
        </h2>
        {photos.length === 0 ? (
          <div className="text-slate-400 text-center py-8">{t('common:errors.failedToLoad')}</div>
        ) : (
          <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 gap-2">
            {photos.map((photo) => (
              <Link
                key={photo.uid}
                to={`/photos/${photo.uid}`}
                className="aspect-square rounded-lg overflow-hidden bg-slate-800 hover:ring-2 hover:ring-blue-500 transition-all"
              >
                <img
                  src={getThumbnailUrl(photo.uid, 'tile_224')}
                  alt={photo.title}
                  className="w-full h-full object-cover"
                  loading="lazy"
                />
              </Link>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
