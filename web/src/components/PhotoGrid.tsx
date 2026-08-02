import { useEffect, useRef } from 'react';
import { VirtuosoGrid, type VirtuosoGridHandle } from 'react-virtuoso';
import { Loader2 } from 'lucide-react';
import { PhotoCardLink, PhotoCard } from './PhotoCard';
import type { Photo } from '../types';

interface PhotoGridProps {
  photos: Photo[];
  onPhotoClick?: (photo: Photo) => void;
  selectable?: boolean;
  selectedPhotos?: Set<string>;
  // Receives the click UID and the underlying mouse event so the caller can
  // route shift/ctrl/meta clicks (range select, additive toggle) without the
  // grid needing to know about anchors.
  onSelectionChange?: (uid: string, event?: React.MouseEvent) => void;
  // UID of the keyboard-focused card (drawn with a yellow ring). When set,
  // the grid scrolls that card into view as the focus moves.
  focusedUid?: string | null;
  // Per-card hover quick-actions toolbar (favorite / archive / add-to-album).
  // When enabled, callers should supply onArchived to drop the archived card
  // from their photo list; onFavoriteChanged is optional (the toolbar tracks
  // an internal optimistic copy of the favorite flag either way).
  enableQuickActions?: boolean;
  onArchived?: (uid: string) => void;
  onFavoriteChanged?: (uid: string, favorite: boolean) => void;
  // Infinite-scroll plumbing for large views. When `hasMore` is true and the
  // viewport approaches the bottom of the rendered set, the grid invokes
  // `onEndReached` to request the next page. `isLoadingMore` toggles a
  // spinner in the footer so the user gets feedback during the fetch.
  onEndReached?: () => void;
  hasMore?: boolean;
  isLoadingMore?: boolean;
}

const GRID_CLASS =
  'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2';

function FocusRing({ focused, children }: { focused: boolean; children: React.ReactNode }) {
  return (
    <div
      className={
        focused
          ? 'rounded-lg ring-2 ring-amber-400 ring-offset-2 ring-offset-slate-900'
          : ''
      }
    >
      {children}
    </div>
  );
}

function GridFooter({ loading }: { loading: boolean }) {
  if (!loading) return null;
  return (
    <div className="flex items-center justify-center py-6 text-slate-400">
      <Loader2 className="h-5 w-5 animate-spin mr-2" />
      <span className="text-sm">Loading more…</span>
    </div>
  );
}

export function PhotoGrid({
  photos,
  onPhotoClick,
  selectable,
  selectedPhotos,
  onSelectionChange,
  focusedUid,
  enableQuickActions,
  onArchived,
  onFavoriteChanged,
  onEndReached,
  hasMore,
  isLoadingMore,
}: PhotoGridProps) {
  const gridRef = useRef<VirtuosoGridHandle>(null);

  // Scroll the focused card into view via Virtuoso's imperative API. With
  // virtualization, the target wrapper may not be mounted yet, so a DOM
  // scrollIntoView would be a no-op for off-screen items.
  useEffect(() => {
    if (!focusedUid) return;
    const index = photos.findIndex((p) => p.uid === focusedUid);
    if (index < 0) return;
    gridRef.current?.scrollToIndex({ index, behavior: 'smooth' });
  }, [focusedUid, photos]);

  // Array.isArray rather than a bare length check: a caller that accidentally
  // hands over an API envelope instead of the unwrapped list has an undefined
  // .length, which would slip past `=== 0` and mount VirtuosoGrid over a
  // non-array — computeItemKey then dereferences undefined and takes the whole
  // page down with it.
  if (!Array.isArray(photos) || photos.length === 0) {
    return (
      <div className="text-center py-12 text-slate-400">
        No photos found
      </div>
    );
  }

  const renderItem = (photo: Photo) => {
    const focused = focusedUid === photo.uid;

    if (selectable && selectedPhotos && onSelectionChange) {
      return (
        <FocusRing focused={focused}>
          <PhotoCard
            photoUid={photo.uid}
            selectable
            selected={selectedPhotos.has(photo.uid)}
            onSelectionChange={(_, event) => onSelectionChange(photo.uid, event)}
            favorite={photo.favorite}
          />
        </FocusRing>
      );
    }

    if (onPhotoClick) {
      return (
        <FocusRing focused={focused}>
          <PhotoCard
            photoUid={photo.uid}
            onClick={() => onPhotoClick(photo)}
            enableQuickActions={enableQuickActions}
            favorite={photo.favorite}
            onArchived={onArchived}
            onFavoriteChanged={onFavoriteChanged}
          />
        </FocusRing>
      );
    }

    return (
      <FocusRing focused={focused}>
        <PhotoCardLink photoUid={photo.uid} favorite={photo.favorite} />
      </FocusRing>
    );
  };

  // The grid renders inside a window-scrolled host so the existing pages keep
  // controlling overall scroll position (and saveCache/window.scrollY-based
  // restoration keeps working). VirtuosoGrid plugs Tailwind's CSS grid into
  // its inner list wrapper, so the responsive breakpoints continue to define
  // the column count.
  return (
    <VirtuosoGrid
      ref={gridRef}
      useWindowScroll
      data={photos}
      computeItemKey={(_index, photo) => photo.uid}
      listClassName={GRID_CLASS}
      endReached={hasMore && onEndReached ? () => onEndReached() : undefined}
      increaseViewportBy={{ top: 400, bottom: 1200 }}
      itemContent={(_index, photo) => renderItem(photo)}
      components={{
        Footer: () => <GridFooter loading={Boolean(hasMore && isLoadingMore)} />,
      }}
    />
  );
}
