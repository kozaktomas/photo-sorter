import { useTranslation } from 'react-i18next';
import { X, FolderPlus, Tag, Star, FolderMinus } from 'lucide-react';
import { Button } from './Button';
import type { ActionMessage, UsePhotoSelectionReturn } from '../hooks/usePhotoSelection';

interface BulkActionBarProps {
  selection: UsePhotoSelectionReturn;
  datalistId?: string;
  showFavorite?: boolean;
  showRemoveFromAlbum?: boolean;
  albumUidForRemoval?: string;
  onRemoveSuccess?: () => void;
}

export function BulkActionBar({
  selection,
  datalistId = 'bulk-label-suggestions',
  showFavorite = false,
  showRemoveFromAlbum = false,
  albumUidForRemoval,
  onRemoveSuccess,
}: BulkActionBarProps) {
  const { t } = useTranslation(['pages', 'common']);

  if (selection.selectedPhotos.size === 0) return null;

  const handleRemoveFromAlbum = async () => {
    if (!albumUidForRemoval) return;
    await selection.handleRemoveFromAlbum(albumUidForRemoval);
    onRemoveSuccess?.();
  };

  return (
    <div className="p-4 bg-blue-500/10 border border-blue-500/20 rounded-lg">
      <div className="flex flex-wrap items-center gap-4">
        <span className="text-blue-400 font-medium">
          {t('common:units.selected', { count: selection.selectedPhotos.size })}
        </span>

        {/* Add to Album */}
        <div className="flex items-center gap-2">
          <select
            value={selection.selectedAlbum}
            onChange={(e) => selection.setSelectedAlbum(e.target.value)}
            className="px-3 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('pages:similar.selectAlbum')}</option>
            {selection.albums.map((album) => (
              <option key={album.uid} value={album.uid}>
                {album.title}
              </option>
            ))}
          </select>
          <Button
            size="sm"
            onClick={selection.handleAddToAlbum}
            disabled={!selection.selectedAlbum || selection.isAddingToAlbum}
            isLoading={selection.isAddingToAlbum}
          >
            <FolderPlus className="h-3 w-3 mr-1" />
            {t('common:buttons.addToAlbum')}
          </Button>
        </div>

        {/* Add Label */}
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={selection.labelInput}
            onChange={(e) => selection.setLabelInput(e.target.value)}
            placeholder={t('pages:similar.enterLabel')}
            list={datalistId}
            className="px-3 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
          />
          <datalist id={datalistId}>
            {selection.labels.map((label) => (
              <option key={label.uid} value={label.name} />
            ))}
          </datalist>
          <Button
            size="sm"
            onClick={selection.handleAddLabel}
            disabled={!selection.labelInput.trim() || selection.isAddingLabel}
            isLoading={selection.isAddingLabel}
          >
            <Tag className="h-3 w-3 mr-1" />
            {t('common:buttons.addLabel')}
          </Button>
        </div>

        {/* Favorite toggle */}
        {showFavorite && (
          <Button
            size="sm"
            variant="secondary"
            onClick={() => selection.handleBatchEdit({ favorite: true })}
            disabled={selection.isBatchEditing}
            isLoading={selection.isBatchEditing}
          >
            <Star className="h-3 w-3 mr-1" />
            {t('common:buttons.setFavorite')}
          </Button>
        )}

        {/* Remove from Album */}
        {showRemoveFromAlbum && albumUidForRemoval && (
          <Button
            size="sm"
            variant="danger"
            onClick={handleRemoveFromAlbum}
            disabled={selection.isRemovingFromAlbum}
            isLoading={selection.isRemovingFromAlbum}
          >
            <FolderMinus className="h-3 w-3 mr-1" />
            {t('common:buttons.removeFromAlbum')}
          </Button>
        )}

        {/* Clear selection */}
        <button
          onClick={selection.deselectAll}
          className="ml-auto text-slate-400 hover:text-white transition-colors"
          title={t('common:buttons.deselect')}
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Action message */}
      {selection.actionMessage && (
        <ActionMessageDisplay message={selection.actionMessage} />
      )}
    </div>
  );
}

function ActionMessageDisplay({ message }: { message: ActionMessage }) {
  return (
    <div
      className={`mt-3 text-sm ${
        message.type === 'success' ? 'text-green-400' : 'text-red-400'
      }`}
    >
      {message.text}
    </div>
  );
}
