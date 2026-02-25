import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Copy, ExternalLink, Search, Check, Loader2, X, Eye, Star } from 'lucide-react';
import { getThumbnailUrl } from '../api/client';
import { copyToClipboard } from '../utils/clipboard';
import { LazyImage } from './LazyImage';
import { ACTION_LABELS, ACTION_BORDER_COLORS, ACTION_BG_COLORS } from '../constants/actions';
import type { MatchAction } from '../types';

export interface PhotoCardProps {
  photoUid: string;
  photoprismDomain?: string;
  // Match percentage (0-100), shown if provided
  matchPercent?: number;
  // Selection
  selectable?: boolean;
  selected?: boolean;
  onSelectionChange?: (selected: boolean) => void;
  // Click behavior
  onClick?: () => void;
  // Thumbnail size
  thumbnailSize?: 'tile_224' | 'tile_500' | 'fit_720';
  // Face-specific props
  bboxRel?: number[]; // [x, y, w, h] relative coordinates
  bboxPadding?: number; // extra padding as fraction of bbox size (e.g. 0.3 = 30%)
  action?: MatchAction;
  onApprove?: () => Promise<void>;
  onReject?: () => void;
  // Extra badge (e.g., match count for Expand)
  badge?: string;
  // Aspect ratio
  aspectRatio?: 'square' | 'auto';
}

export function PhotoCard({
  photoUid,
  photoprismDomain,
  matchPercent,
  selectable = false,
  selected = false,
  onSelectionChange,
  onClick,
  thumbnailSize = 'tile_224',
  bboxRel,
  bboxPadding = 0,
  action,
  onApprove,
  onReject,
  badge,
  aspectRatio = 'square',
}: PhotoCardProps) {
  const navigate = useNavigate();
  const [isApproving, setIsApproving] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleCopyId = (e: React.MouseEvent) => {
    e.stopPropagation();
    void copyToClipboard(photoUid);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const handleOpenPhotoprism = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (photoprismDomain) {
      const url = `${photoprismDomain}/library/browse?view=cards&order=oldest&q=uid:${photoUid}`;
      window.open(url, '_blank');
    }
  };

  const handleOpenDetail = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey) {
      window.open(`/photos/${photoUid}`, '_blank');
    } else {
      void navigate(`/photos/${photoUid}`);
    }
  };

  const handleFindSimilar = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey) {
      window.open(`/similar?photo=${photoUid}`, '_blank');
    } else {
      void navigate(`/similar?photo=${photoUid}`);
    }
  };

  const handleApprove = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!onApprove || isApproving) return;
    setIsApproving(true);
    try {
      await onApprove();
    } finally {
      setIsApproving(false);
    }
  };

  const handleReject = (e: React.MouseEvent) => {
    e.stopPropagation();
    onReject?.();
  };

  const handleClick = () => {
    if (selectable && onSelectionChange) {
      onSelectionChange(!selected);
    } else if (onClick) {
      onClick();
    }
  };

  const showFaceActions = action && action !== 'already_done' && (onApprove ?? onReject);

  return (
    <div
      className={`group relative bg-slate-800 rounded-lg overflow-hidden cursor-pointer transition-all hover:ring-2 hover:ring-blue-500 ${
        selected ? 'ring-2 ring-blue-500' : ''
      }`}
      onClick={handleClick}
    >
      {/* Image */}
      <div className={`relative ${aspectRatio === 'square' ? 'aspect-square' : ''}`}>
        {thumbnailSize === 'tile_224' ? (
          <LazyImage
            src={getThumbnailUrl(photoUid, thumbnailSize)}
            alt={photoUid}
            className="w-full h-full"
          />
        ) : (
          <img
            src={getThumbnailUrl(photoUid, thumbnailSize)}
            alt={photoUid}
            className="w-full h-full object-cover"
            loading="lazy"
          />
        )}

        {/* Bounding box overlay for faces */}
        {bboxRel?.length === 4 && (() => {
          const padX = bboxRel[2] * bboxPadding;
          const padY = bboxRel[3] * bboxPadding;
          const x = Math.max(0, bboxRel[0] - padX);
          const y = Math.max(0, bboxRel[1] - padY);
          const w = Math.min(1 - x, bboxRel[2] + padX * 2);
          const h = Math.min(1 - y, bboxRel[3] + padY * 2);
          return (
            <div
              className={`absolute border-2 ${action ? ACTION_BORDER_COLORS[action] : 'border-orange-500'} pointer-events-none`}
              style={{
                left: `${x * 100}%`,
                top: `${y * 100}%`,
                width: `${w * 100}%`,
                height: `${h * 100}%`,
              }}
            />
          );
        })()}
      </div>

      {/* Selection checkbox */}
      {selectable && (
        <div
          className={`absolute top-2 left-2 w-5 h-5 rounded border-2 flex items-center justify-center transition-all ${
            selected
              ? 'bg-blue-500 border-blue-500'
              : 'border-white/50 bg-black/30 group-hover:border-white'
          }`}
        >
          {selected && <Check className="h-3 w-3 text-white" />}
        </div>
      )}

      {/* Action badge for faces */}
      {action && (
        <div
          className={`absolute top-2 right-2 ${ACTION_BG_COLORS[action]} text-white text-xs px-1.5 py-0.5 rounded font-medium`}
        >
          {ACTION_LABELS[action]}
        </div>
      )}

      {/* Extra badge (e.g., match count) */}
      {badge && !action && (
        <div className="absolute top-2 right-2 px-2 py-0.5 bg-green-600 text-white text-xs font-medium rounded">
          {badge}
        </div>
      )}

      {/* Bottom overlay with match percent and actions */}
      <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent p-2">
        <div className="flex items-center justify-between">
          {/* Match percent */}
          <div className="text-xs text-white">
            {matchPercent !== undefined ? `${matchPercent} % match` : ''}
          </div>

          {/* Action buttons - always visible on hover */}
          <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
            {/* Detail page */}
            <button
              onClick={handleOpenDetail}
              className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
              title="View details"
              aria-label="View details"
            >
              <Eye className="h-3 w-3" />
            </button>

            {/* Find similar */}
            <button
              onClick={handleFindSimilar}
              className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
              title="Find similar"
              aria-label="Find similar"
            >
              <Search className="h-3 w-3" />
            </button>

            {/* Copy ID */}
            <button
              onClick={handleCopyId}
              className={`p-1.5 rounded text-white transition-colors ${
                copied ? 'bg-green-600' : 'bg-black/60 hover:bg-black/80'
              }`}
              title={copied ? 'Copied!' : 'Copy photo ID'}
              aria-label={copied ? 'Copied!' : 'Copy photo ID'}
            >
              {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            </button>

            {/* PhotoPrism link */}
            {photoprismDomain && (
              <button
                onClick={handleOpenPhotoprism}
                className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
                title="Open in PhotoPrism"
                aria-label="Open in PhotoPrism"
              >
                <ExternalLink className="h-3 w-3" />
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Face approve/reject buttons */}
      {showFaceActions && (
        <div className="absolute bottom-8 right-2 flex gap-1">
          {onApprove && (
            <button
              onClick={handleApprove}
              disabled={isApproving}
              className="p-1.5 bg-green-600 hover:bg-green-500 disabled:bg-green-800 text-white rounded transition-colors"
              title="Approve"
              aria-label="Approve"
            >
              {isApproving ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Check className="h-4 w-4" />
              )}
            </button>
          )}
          {onReject && (
            <button
              onClick={handleReject}
              disabled={isApproving}
              className="p-1.5 bg-red-600 hover:bg-red-500 disabled:bg-red-800 text-white rounded transition-colors"
              title="Reject"
              aria-label="Reject"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// Simple link version for basic grids
export function PhotoCardLink({
  photoUid,
  photoprismDomain,
  favorite,
}: {
  photoUid: string;
  photoprismDomain?: string;
  favorite?: boolean;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopyId = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    void copyToClipboard(photoUid);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const handleOpenPhotoprism = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (photoprismDomain) {
      const url = `${photoprismDomain}/library/browse?view=cards&order=oldest&q=uid:${photoUid}`;
      window.open(url, '_blank');
    }
  };

  const handleFindSimilar = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey) {
      window.open(`/similar?photo=${photoUid}`, '_blank');
    } else {
      window.location.href = `/similar?photo=${photoUid}`;
    }
  };

  return (
    <Link
      to={`/photos/${photoUid}`}
      className="group relative aspect-square bg-slate-800 rounded-lg overflow-hidden cursor-pointer hover:ring-2 hover:ring-blue-500 transition-all block"
    >
      <LazyImage
        src={getThumbnailUrl(photoUid, 'tile_224')}
        alt={photoUid}
        className="w-full h-full"
      />

      {favorite && (
        <div className="absolute top-2 right-2 text-yellow-400">
          <Star className="h-4 w-4 fill-current" />
        </div>
      )}

      {/* Bottom overlay with actions */}
      <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent p-2 opacity-0 group-hover:opacity-100 transition-opacity">
        <div className="flex items-center justify-end gap-1">
          {/* Find similar */}
          <button
            onClick={handleFindSimilar}
            className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
            title="Find similar"
            aria-label="Find similar"
          >
            <Search className="h-3 w-3" />
          </button>

          {/* Copy ID */}
          <button
            onClick={handleCopyId}
            className={`p-1.5 rounded text-white transition-colors ${
              copied ? 'bg-green-600' : 'bg-black/60 hover:bg-black/80'
            }`}
            title={copied ? 'Copied!' : 'Copy photo ID'}
            aria-label={copied ? 'Copied!' : 'Copy photo ID'}
          >
            {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          </button>

          {/* PhotoPrism link */}
          {photoprismDomain && (
            <button
              onClick={handleOpenPhotoprism}
              className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
              title="Open in PhotoPrism"
              aria-label="Open in PhotoPrism"
            >
              <ExternalLink className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>
    </Link>
  );
}
