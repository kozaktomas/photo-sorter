import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Filter, Plus, Pencil, Trash2, Image as ImageIcon } from 'lucide-react';
import { Card, CardContent } from '../../components/Card';
import { Button } from '../../components/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { InlineEditableText } from '../../components/InlineEditableText';
import { SmartAlbumModal } from './SmartAlbumModal';
import {
  listSmartAlbums,
  createSmartAlbum,
  updateSmartAlbum,
  deleteSmartAlbum,
} from '../../api/client';
import { useToast } from '../../components/Toast';
import type { SmartAlbum, SmartAlbumFilters } from '../../types';

/**
 * SmartAlbumsSection renders the "Smart albums" block on the /albums page.
 * It owns its own list state and create/edit modal — the parent page just
 * drops it in above the regular albums grid.
 */
export function SmartAlbumsSection() {
  const { t } = useTranslation(['pages', 'common']);
  const toast = useToast();
  const [items, setItems] = useState<SmartAlbum[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<SmartAlbum | undefined>(undefined);
  const [confirmDelete, setConfirmDelete] = useState<SmartAlbum | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listSmartAlbums();
      setItems(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreateClick = () => {
    setEditing(undefined);
    setModalOpen(true);
  };

  const handleEdit = (album: SmartAlbum) => {
    setEditing(album);
    setModalOpen(true);
  };

  const handleSubmit = async (name: string, filters: SmartAlbumFilters) => {
    // Awaited so the modal can react to thrown errors with inline validation.
    if (editing) {
      await updateSmartAlbum(editing.uid, name, filters);
      toast.success(t('common:toasts.smartAlbums.updated', { name }));
    } else {
      await createSmartAlbum(name, filters);
      toast.success(t('common:toasts.smartAlbums.created', { name }));
    }
    await load();
  };

  const handleConfirmDelete = async () => {
    if (!confirmDelete) return;
    const uid = confirmDelete.uid;
    setConfirmDelete(null);
    try {
      await deleteSmartAlbum(uid);
      toast.success(t('common:toasts.smartAlbums.deleted'));
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('common:toasts.smartAlbums.deleteFailed'));
    }
  };

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold text-white flex items-center gap-2">
            <Filter className="h-5 w-5 text-purple-400" />
            {t('pages:smartAlbums.sectionTitle')}
          </h2>
          <p className="text-sm text-slate-400">{t('pages:smartAlbums.sectionSubtitle')}</p>
        </div>
        <Button onClick={handleCreateClick}>
          <Plus className="h-4 w-4 mr-2" />
          {t('pages:smartAlbums.createButton')}
        </Button>
      </div>

      {error && (
        <div className="px-3 py-2 bg-red-900/40 border border-red-700 rounded text-sm text-red-200">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-sm text-slate-500">{t('common:status.loading')}</div>
      ) : items.length === 0 ? (
        <div className="text-sm text-slate-500 italic">
          {t('pages:smartAlbums.noSmartAlbums')}
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {items.map((album) => (
            <Card
              key={album.uid}
              className="hover:border-purple-500 transition-colors overflow-hidden"
            >
              <Link to={`/smart-albums/${album.uid}`} className="block">
                <div className="aspect-video bg-gradient-to-br from-purple-900/40 to-slate-800 flex items-center justify-center">
                  <Filter className="h-10 w-10 text-purple-400/70" />
                </div>
              </Link>
              <CardContent>
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <h3 className="font-semibold text-white truncate">
                      <InlineEditableText
                        value={album.name}
                        ariaLabel={t('pages:smartAlbums.renameAria', { defaultValue: 'Rename smart album' })}
                        textClassName="inline-block truncate"
                        inputClassName="w-full bg-slate-900 border border-slate-600 rounded px-1 py-0.5 text-white font-semibold focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
                        onSave={async (name) => {
                          await updateSmartAlbum(album.uid, name, album.filters);
                          setItems((prev) =>
                            prev.map((a) => (a.uid === album.uid ? { ...a, name } : a)),
                          );
                        }}
                      />
                    </h3>
                    <Link to={`/smart-albums/${album.uid}`} className="block">
                      <div className="flex items-center text-sm text-slate-400 mt-1">
                        <ImageIcon className="h-4 w-4 mr-1" />
                        {t('common:units.photo', { count: album.photo_count })}
                      </div>
                    </Link>
                  </div>
                  <div className="flex gap-1">
                    <button
                      type="button"
                      onClick={() => handleEdit(album)}
                      className="p-1 text-slate-400 hover:text-purple-300"
                      aria-label={t('pages:smartAlbums.editButton')}
                      title={t('pages:smartAlbums.editButton')}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(album)}
                      className="p-1 text-slate-400 hover:text-red-400"
                      aria-label={t('pages:smartAlbums.deleteButton')}
                      title={t('pages:smartAlbums.deleteButton')}
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <SmartAlbumModal
        open={modalOpen}
        album={editing}
        onClose={() => setModalOpen(false)}
        onSubmit={handleSubmit}
      />

      <ConfirmDialog
        open={!!confirmDelete}
        title={t('pages:smartAlbums.deleteButton')}
        message={t('pages:smartAlbums.confirmDelete')}
        confirmLabel={t('pages:smartAlbums.deleteButton')}
        cancelLabel={t('pages:smartAlbums.cancelButton')}
        variant="danger"
        onConfirm={() => void handleConfirmDelete()}
        onCancel={() => setConfirmDelete(null)}
      />
    </section>
  );
}
