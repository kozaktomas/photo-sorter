import { useEffect, useState, useCallback } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Check, ChevronDown, Filter, Pencil, SortAsc, Trash2, X } from 'lucide-react';
import { Card, CardContent } from '../../components/Card';
import { Button } from '../../components/Button';
import { BulkActionBar } from '../../components/BulkActionBar';
import { PhotoGrid } from '../../components/PhotoGrid';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { InlineEditableText } from '../../components/InlineEditableText';
import { SmartAlbumModal } from './SmartAlbumModal';
import {
  getSmartAlbum,
  getSmartAlbumPhotos,
  updateSmartAlbum,
  deleteSmartAlbum,
} from '../../api/client';
import { usePhotoSelection } from '../../hooks/usePhotoSelection';
import { useSelectionShortcuts } from '../../hooks/useSelectionShortcuts';
import { SORT_OPTIONS } from '../Photos/hooks/usePhotosFilters';
import type { Photo, SmartAlbum, SmartAlbumFilters } from '../../types';

const PAGE_SIZE = 100;

export function SmartAlbumDetailPage() {
  const { uid } = useParams<{ uid: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation(['pages', 'common']);

  const [album, setAlbum] = useState<SmartAlbum | null>(null);
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [sort, setSort] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editOpen, setEditOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [selectionMode, setSelectionMode] = useState(false);
  const selection = usePhotoSelection();

  const exitSelectionMode = useCallback(() => {
    setSelectionMode(false);
    selection.deselectAll();
  }, [selection]);

  useSelectionShortcuts({
    onSelectAll: () => {
      if (photos.length === 0) return;
      setSelectionMode(true);
      selection.selectAll(photos.map((p) => p.uid));
    },
    onClear: selection.selectedPhotos.size > 0 ? selection.deselectAll : undefined,
  });

  const loadAlbum = useCallback(async () => {
    if (!uid) return;
    try {
      const a = await getSmartAlbum(uid);
      setAlbum(a);
      // Default sort honours the saved filter unless the user overrides it.
      if (!sort && a.filters.sort) setSort(a.filters.sort);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [uid, sort]);

  const loadPhotos = useCallback(async () => {
    if (!uid) return;
    setLoading(true);
    try {
      const resp = await getSmartAlbumPhotos(uid, {
        limit: PAGE_SIZE,
        offset,
        sort: sort || undefined,
      });
      setPhotos(resp.photos);
      setTotal(resp.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [uid, offset, sort]);

  useEffect(() => {
    void loadAlbum();
  }, [loadAlbum]);

  useEffect(() => {
    void loadPhotos();
  }, [loadPhotos]);

  const handleSubmitEdit = async (name: string, filters: SmartAlbumFilters) => {
    if (!uid) return;
    await updateSmartAlbum(uid, name, filters);
    setOffset(0);
    await loadAlbum();
    await loadPhotos();
  };

  const handleDelete = async () => {
    if (!uid) return;
    setConfirmDelete(false);
    try {
      await deleteSmartAlbum(uid);
      void navigate('/albums');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handlePhotoClick = (photo: Photo) => {
    void navigate(`/photos/${photo.uid}`);
  };

  if (!album && loading) {
    return <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>;
  }
  if (!album) {
    return (
      <div className="text-center py-12 text-slate-400">
        {error ?? t('pages:smartAlbums.smartAlbumNotFound')}
      </div>
    );
  }

  const hasMore = offset + photos.length < total;

  return (
    <div className="space-y-6">
      <div className="flex items-center space-x-4">
        <Button variant="ghost" onClick={() => navigate('/albums')}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          {t('common:buttons.back')}
        </Button>
      </div>

      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-3xl font-bold text-white flex items-center gap-2">
            <Filter className="h-7 w-7 text-purple-400 shrink-0" />
            <InlineEditableText
              value={album.name}
              ariaLabel={t('pages:smartAlbums.renameAria', { defaultValue: 'Rename smart album' })}
              textClassName="truncate inline-block"
              inputClassName="bg-slate-800 border border-slate-600 rounded px-2 py-0.5 text-white text-3xl font-bold focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
              onSave={async (name) => {
                await updateSmartAlbum(album.uid, name, album.filters);
                setAlbum((prev) => (prev ? { ...prev, name } : prev));
              }}
            />
          </h1>
          <p className="text-slate-500 mt-2">{t('common:units.photo', { count: total })}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <div className="relative">
            <SortAsc className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 pointer-events-none" />
            <select
              value={sort}
              onChange={(e) => {
                setOffset(0);
                setSort(e.target.value);
              }}
              className="pl-9 pr-8 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 appearance-none cursor-pointer"
            >
              <option value="">{t('pages:smartAlbums.sortLabel')}</option>
              {SORT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {t(opt.label)}
                </option>
              ))}
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 pointer-events-none" />
          </div>
          {selectionMode ? (
            <Button variant="secondary" size="sm" onClick={exitSelectionMode}>
              <X className="h-4 w-4 mr-1" />
              {t('common:buttons.cancel')}
            </Button>
          ) : (
            <Button variant="secondary" size="sm" onClick={() => setSelectionMode(true)}>
              <Check className="h-4 w-4 mr-1" />
              {t('common:buttons.select')}
            </Button>
          )}
          <Button variant="ghost" onClick={() => setEditOpen(true)}>
            <Pencil className="h-4 w-4 mr-2" />
            {t('pages:smartAlbums.editButton')}
          </Button>
          <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
            <Trash2 className="h-4 w-4 mr-2" />
            {t('pages:smartAlbums.deleteButton')}
          </Button>
        </div>
      </div>

      {selectionMode && (
        <div className="sticky top-0 z-10">
          {selection.selectedPhotos.size > 0 && (
            <div className="flex gap-2 mb-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => selection.selectAll(photos.map((p) => p.uid))}
                disabled={selection.selectedPhotos.size === photos.length}
              >
                <Check className="h-3 w-3 mr-1" />
                {t('common:buttons.selectAll')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={selection.deselectAll}
              >
                <X className="h-3 w-3 mr-1" />
                {t('common:buttons.deselect')}
              </Button>
            </div>
          )}
          <BulkActionBar selection={selection} datalistId="smart-album-labels" showFavorite />
        </div>
      )}

      {error && (
        <div className="px-3 py-2 bg-red-900/40 border border-red-700 rounded text-sm text-red-200">
          {error}
        </div>
      )}

      <Card>
        <CardContent>
          {loading ? (
            <div className="text-center py-12 text-slate-400">
              {t('common:status.loading')}
            </div>
          ) : photos.length === 0 ? (
            <div className="text-center py-12 text-slate-400">
              {t('pages:smartAlbums.emptyResults')}
            </div>
          ) : (
            <>
              <PhotoGrid
                photos={photos}
                onPhotoClick={selectionMode ? undefined : handlePhotoClick}
                selectable={selectionMode}
                selectedPhotos={selection.selectedPhotos}
                onSelectionChange={(photoUid, event) =>
                  selection.handleSelectionClick(
                    photoUid,
                    photos.map((p) => p.uid),
                    event ?? {},
                  )
                }
                enableQuickActions={!selectionMode}
                onArchived={(archivedUid) => {
                  setPhotos((prev) => prev.filter((p) => p.uid !== archivedUid));
                  setTotal((prev) => Math.max(prev - 1, 0));
                }}
                onFavoriteChanged={(photoUid, favorite) =>
                  setPhotos((prev) =>
                    prev.map((p) => (p.uid === photoUid ? { ...p, favorite } : p)),
                  )
                }
              />
              {hasMore && (
                <div className="flex justify-center mt-4">
                  <Button
                    variant="ghost"
                    onClick={() => setOffset(offset + PAGE_SIZE)}
                  >
                    {t('common:buttons.loadMore', { defaultValue: 'Load more' })}
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      <SmartAlbumModal
        open={editOpen}
        album={album}
        onClose={() => setEditOpen(false)}
        onSubmit={handleSubmitEdit}
      />

      <ConfirmDialog
        open={confirmDelete}
        title={t('pages:smartAlbums.deleteButton')}
        message={t('pages:smartAlbums.confirmDelete')}
        confirmLabel={t('pages:smartAlbums.deleteButton')}
        cancelLabel={t('pages:smartAlbums.cancelButton')}
        variant="danger"
        onConfirm={() => void handleDelete()}
        onCancel={() => setConfirmDelete(false)}
      />
    </div>
  );
}
