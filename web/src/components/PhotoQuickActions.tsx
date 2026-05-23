import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FolderPlus, Loader2, Star, Trash2, X } from 'lucide-react';
import { addPhotosToAlbum, archivePhotos, batchEditPhotos, getAlbums } from '../api/client';
import { MAX_ALBUMS_FETCH } from '../constants';
import { useAuth } from '../hooks/useAuth';
import { Combobox } from './Combobox';
import { ConfirmDialog } from './ConfirmDialog';
import type { Album } from '../types';

interface PhotoQuickActionsProps {
  photoUid: string;
  favorite: boolean;
  onArchived?: (uid: string) => void;
  onFavoriteChanged?: (uid: string, favorite: boolean) => void;
}

// PhotoQuickActions renders the hover-only quick-action toolbar on a PhotoCard
// (favorite toggle, archive, add to album). Buttons stopPropagation so the
// card's main click handler (open detail) does not fire.
export function PhotoQuickActions({
  photoUid,
  favorite,
  onArchived,
  onFavoriteChanged,
}: PhotoQuickActionsProps) {
  const { t } = useTranslation(['common', 'pages']);
  const { user } = useAuth();
  const hasWriteAccess = user?.role === 'admin' || user?.role === 'editor';

  // Mirror the favorite prop so the star can update optimistically without
  // forcing every caller to wire `onFavoriteChanged` to its photo list state.
  const [isFavorite, setIsFavorite] = useState(favorite);
  useEffect(() => {
    setIsFavorite(favorite);
  }, [favorite]);

  const [favPending, setFavPending] = useState(false);
  const [archivePending, setArchivePending] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const [albumPopoverOpen, setAlbumPopoverOpen] = useState(false);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [albumsLoaded, setAlbumsLoaded] = useState(false);
  const [selectedAlbum, setSelectedAlbum] = useState('');
  const [adding, setAdding] = useState(false);
  const [addedMsg, setAddedMsg] = useState<string | null>(null);

  // Close the popover on Escape (matches the Combobox + ConfirmDialog UX).
  useEffect(() => {
    if (!albumPopoverOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setAlbumPopoverOpen(false);
        setSelectedAlbum('');
        setAddedMsg(null);
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [albumPopoverOpen]);

  const ensureAlbumsLoaded = useCallback(async () => {
    if (albumsLoaded) return;
    try {
      const data = await getAlbums({ count: MAX_ALBUMS_FETCH, order: 'name' });
      setAlbums(data);
      setAlbumsLoaded(true);
    } catch (err) {
      console.error('Failed to load albums:', err);
    }
  }, [albumsLoaded]);

  if (!hasWriteAccess) return null;

  const stop = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
  };

  const handleToggleFavorite = async (e: React.MouseEvent) => {
    stop(e);
    if (favPending) return;
    const next = !isFavorite;
    setFavPending(true);
    setIsFavorite(next);
    try {
      // Match BulkActionBar — use the batch edit endpoint with a single UID
      // so the success/error path stays consistent with bulk actions.
      await batchEditPhotos([photoUid], { favorite: next });
      onFavoriteChanged?.(photoUid, next);
    } catch (err) {
      console.error('Failed to toggle favorite:', err);
      setIsFavorite(!next);
    } finally {
      setFavPending(false);
    }
  };

  const handleRequestArchive = (e: React.MouseEvent) => {
    stop(e);
    setConfirmOpen(true);
  };

  const handleConfirmArchive = async () => {
    if (archivePending) return;
    setArchivePending(true);
    try {
      await archivePhotos([photoUid]);
      setConfirmOpen(false);
      onArchived?.(photoUid);
    } catch (err) {
      console.error('Failed to archive photo:', err);
    } finally {
      setArchivePending(false);
    }
  };

  const handleOpenAlbumPopover = (e: React.MouseEvent) => {
    stop(e);
    setAlbumPopoverOpen(true);
    setAddedMsg(null);
    void ensureAlbumsLoaded();
  };

  const handleCloseAlbumPopover = () => {
    setAlbumPopoverOpen(false);
    setSelectedAlbum('');
    setAddedMsg(null);
  };

  const handleAddToAlbum = async () => {
    if (!selectedAlbum || adding) return;
    setAdding(true);
    try {
      await addPhotosToAlbum(selectedAlbum, [photoUid]);
      const album = albums.find((a) => a.uid === selectedAlbum);
      setAddedMsg(t('common:quickActions.addedTo', { name: album?.title ?? '' }));
      setSelectedAlbum('');
      setTimeout(handleCloseAlbumPopover, 1000);
    } catch (err) {
      console.error('Failed to add to album:', err);
      setAddedMsg(t('common:errors.failedToApply'));
    } finally {
      setAdding(false);
    }
  };

  return (
    <>
      {/* Hover toolbar — sits just above the existing utility action row at
          the bottom edge, so the two hover groups don't overlap. Hidden on
          coarse-pointer (touch) devices since there is no hover concept. */}
      <div
        className="absolute bottom-10 right-2 z-10 flex gap-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity pointer-coarse:hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          onClick={handleToggleFavorite}
          disabled={favPending}
          className={`p-1.5 rounded text-white transition-colors ${
            isFavorite
              ? 'bg-yellow-500/90 hover:bg-yellow-500'
              : 'bg-black/60 hover:bg-black/80'
          }`}
          title={isFavorite ? t('common:buttons.unfavorite') : t('common:buttons.favorite')}
          aria-label={
            isFavorite ? t('common:buttons.unfavorite') : t('common:buttons.favorite')
          }
          aria-pressed={isFavorite}
        >
          {favPending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Star className={`h-3 w-3 ${isFavorite ? 'fill-current' : ''}`} />
          )}
        </button>
        <button
          type="button"
          onClick={handleOpenAlbumPopover}
          className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
          title={t('common:buttons.addToAlbum')}
          aria-label={t('common:buttons.addToAlbum')}
        >
          <FolderPlus className="h-3 w-3" />
        </button>
        <button
          type="button"
          onClick={handleRequestArchive}
          disabled={archivePending}
          className="p-1.5 bg-black/60 rounded text-white hover:bg-red-600/80 transition-colors"
          title={t('common:buttons.archive')}
          aria-label={t('common:buttons.archive')}
        >
          {archivePending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Trash2 className="h-3 w-3" />
          )}
        </button>
      </div>

      {/* Add-to-album popover — overlays the whole card so the Combobox dropdown
          stays inside the card's overflow:hidden bounding box. */}
      {albumPopoverOpen && (
        <div
          className="absolute inset-0 z-20 bg-slate-900/95 flex flex-col p-3 gap-2"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-white">
              {t('common:buttons.addToAlbum')}
            </span>
            <button
              type="button"
              onClick={handleCloseAlbumPopover}
              className="text-slate-400 hover:text-white"
              aria-label={t('common:buttons.close')}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <Combobox
            value={selectedAlbum}
            onChange={setSelectedAlbum}
            options={albums.map((a) => ({ value: a.uid, label: a.title }))}
            placeholder={t('pages:similar.selectAlbum')}
            size="sm"
          />
          {addedMsg && (
            <div className="text-xs text-green-400">{addedMsg}</div>
          )}
          <div className="mt-auto flex justify-end gap-2">
            <button
              type="button"
              onClick={handleCloseAlbumPopover}
              className="px-3 py-1 bg-slate-700 hover:bg-slate-600 text-white text-xs rounded transition-colors"
            >
              {t('common:buttons.cancel')}
            </button>
            <button
              type="button"
              onClick={() => void handleAddToAlbum()}
              disabled={!selectedAlbum || adding}
              className="px-3 py-1 bg-blue-600 hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-500 text-white text-xs rounded transition-colors inline-flex items-center gap-1"
            >
              {adding && <Loader2 className="h-3 w-3 animate-spin" />}
              {t('common:buttons.addToAlbum')}
            </button>
          </div>
        </div>
      )}

      {/* ConfirmDialog renders fixed-positioned but lives in the card's React
          tree, so clicks inside it would otherwise bubble to the card root
          and trigger the open-detail handler. Stop propagation at the wrapper
          so the dialog stays self-contained. */}
      {confirmOpen && (
        <div onClick={(e) => e.stopPropagation()}>
          <ConfirmDialog
            open={confirmOpen}
            title={t('pages:shortcuts.archiveConfirmTitle')}
            message={t('pages:shortcuts.archiveConfirmMessage')}
            confirmLabel={t('pages:shortcuts.archiveConfirmButton')}
            cancelLabel={t('common:buttons.cancel')}
            variant="danger"
            onConfirm={() => void handleConfirmArchive()}
            onCancel={() => setConfirmOpen(false)}
          />
        </div>
      )}
    </>
  );
}
