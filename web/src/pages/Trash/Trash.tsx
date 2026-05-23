import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Loader2, RotateCcw, Trash2, X } from 'lucide-react';
import { Alert } from '../../components/Alert';
import { Button } from '../../components/Button';
import { Card, CardContent } from '../../components/Card';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { PageHeader } from '../../components/PageHeader';
import { PhotoGrid } from '../../components/PhotoGrid';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { useAuth } from '../../hooks/useAuth';
import { useGridSelection } from '../../hooks/useGridSelection';
import { useSelectionShortcuts } from '../../hooks/useSelectionShortcuts';
import { getTrashPhotos, purgePhotos, restorePhotos } from '../../api/client';
import type { Photo } from '../../types';

const PAGE_SIZE = 100;

// Bulk-action message banner displayed below the page header.
interface ActionMessage { type: 'success' | 'error'; text: string }

export function TrashPage() {
  const { t } = useTranslation(['pages', 'common']);
  const { user } = useAuth();

  const [photos, setPhotos] = useState<Photo[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const gridSelection = useGridSelection();
  const selected = gridSelection.selectedPhotos;
  const [actionMessage, setActionMessage] = useState<ActionMessage | null>(null);
  const [isRestoring, setIsRestoring] = useState(false);
  const [isPurging, setIsPurging] = useState(false);
  const [confirmPurge, setConfirmPurge] = useState(false);

  const isAdmin = user?.role === 'admin';

  const loadTrash = useCallback(async () => {
    setIsLoading(true);
    setLoadError(null);
    try {
      const resp = await getTrashPhotos({ limit: PAGE_SIZE, sort: '-archived_at' });
      setPhotos(resp.photos);
      setTotal(resp.total);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to load trash');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadTrash();
  }, [loadTrash]);

  const selectAll = useCallback(() => {
    gridSelection.selectAll(photos.map((p) => p.uid));
  }, [gridSelection, photos]);

  const clearSelection = useCallback(() => {
    gridSelection.deselectAll();
  }, [gridSelection]);

  useSelectionShortcuts({
    onSelectAll: photos.length > 0 ? selectAll : undefined,
    onClear: selected.size > 0 ? clearSelection : undefined,
  });

  const handleRestore = async () => {
    if (selected.size === 0) return;
    setIsRestoring(true);
    setActionMessage(null);
    try {
      const result = await restorePhotos(Array.from(selected));
      const errCount = result.errors?.length ?? 0;
      setActionMessage(
        errCount > 0
          ? { type: 'error', text: t('pages:trash.restoredWithErrors', { count: result.updated, errors: errCount }) }
          : { type: 'success', text: t('pages:trash.restored', { count: result.updated }) },
      );
      gridSelection.deselectAll();
      await loadTrash();
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to restore' });
    } finally {
      setIsRestoring(false);
    }
  };

  const handlePurge = async () => {
    setConfirmPurge(false);
    if (selected.size === 0) return;
    setIsPurging(true);
    setActionMessage(null);
    try {
      const result = await purgePhotos(Array.from(selected));
      const errCount = result.errors?.length ?? 0;
      setActionMessage(
        errCount > 0
          ? { type: 'error', text: t('pages:trash.purgedWithErrors', { count: result.purged, errors: errCount }) }
          : { type: 'success', text: t('pages:trash.purged', { count: result.purged }) },
      );
      gridSelection.deselectAll();
      await loadTrash();
    } catch (err) {
      setActionMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to purge' });
    } finally {
      setIsPurging(false);
    }
  };

  const selectionCount = selected.size;

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.trash.icon}
        title={t('pages:trash.title')}
        subtitle={t('pages:trash.subtitle', { count: total })}
        color={PAGE_CONFIGS.trash.color}
        category={PAGE_CONFIGS.trash.category}
      />

      {/* Bulk action bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="secondary"
          size="sm"
          onClick={selectAll}
          disabled={photos.length === 0 || selectionCount === photos.length}
        >
          {t('common:buttons.selectAll')}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={clearSelection}
          disabled={selectionCount === 0}
        >
          <X className="h-3 w-3 mr-1" />
          {t('common:buttons.deselect')}
        </Button>
        <span className="text-sm text-slate-400">
          {t('common:units.selected', { count: selectionCount })}
        </span>

        <div className="ml-auto flex gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={handleRestore}
            disabled={selectionCount === 0 || isRestoring}
            isLoading={isRestoring}
          >
            <RotateCcw className="h-4 w-4 mr-1" />
            {t('pages:trash.restore')}
          </Button>
          {isAdmin && (
            <Button
              variant="danger"
              size="sm"
              onClick={() => setConfirmPurge(true)}
              disabled={selectionCount === 0 || isPurging}
              isLoading={isPurging}
            >
              <Trash2 className="h-4 w-4 mr-1" />
              {t('pages:trash.purge')}
            </Button>
          )}
        </div>
      </div>

      {actionMessage && (
        <Alert variant={actionMessage.type === 'success' ? 'success' : 'error'}>
          {actionMessage.text}
        </Alert>
      )}

      {loadError && (
        <Alert variant="error">{loadError}</Alert>
      )}

      {isLoading ? (
        <Card>
          <CardContent>
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 text-rose-400 animate-spin" />
              <span className="ml-3 text-slate-400">{t('common:status.loading')}</span>
            </div>
          </CardContent>
        </Card>
      ) : photos.length === 0 ? (
        <Card>
          <CardContent>
            <div className="text-center py-12 text-slate-400">
              <Trash2 className="h-12 w-12 mx-auto mb-3 opacity-50" />
              <p>{t('pages:trash.empty')}</p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent>
            <PhotoGrid
              photos={photos}
              selectable
              selectedPhotos={selected}
              onSelectionChange={(uid, event) =>
                gridSelection.handleSelectionClick(
                  uid,
                  photos.map((p) => p.uid),
                  event ?? {},
                )
              }
            />
          </CardContent>
        </Card>
      )}

      <ConfirmDialog
        open={confirmPurge}
        title={t('pages:trash.confirmPurgeTitle')}
        message={t('pages:trash.confirmPurgeMessage', { count: selectionCount })}
        confirmLabel={t('pages:trash.purge')}
        cancelLabel={t('common:buttons.cancel')}
        variant="danger"
        onConfirm={() => void handlePurge()}
        onCancel={() => setConfirmPurge(false)}
      />
    </div>
  );
}
