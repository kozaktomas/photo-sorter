import { useState, useEffect, useCallback, useMemo, type Dispatch, type SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import { DndContext, DragOverlay, PointerSensor, pointerWithin, useSensor, useSensors, type DragEndEvent, type DragStartEvent, type Modifier } from '@dnd-kit/core';
import { assignSlot, clearSlot, swapSlots, updatePage, getThumbnailUrl } from '../../api/client';
import { PageSidebar } from './PageSidebar';
import { PageTemplate } from './PageTemplate';
import { UnassignedPool } from './UnassignedPool';
import { PhotoDescriptionDialog } from './PhotoDescriptionDialog';
import type { BookDetail, SectionPhoto } from '../../types';

// Snap the DragOverlay center to the cursor so large source elements don't cause offset
const snapCenterToCursor: Modifier = ({ activatorEvent, activeNodeRect, draggingNodeRect, transform }) => {
  if (activatorEvent instanceof PointerEvent && activeNodeRect && draggingNodeRect) {
    const grabX = activatorEvent.clientX - activeNodeRect.left;
    const grabY = activatorEvent.clientY - activeNodeRect.top;
    return {
      ...transform,
      x: transform.x + grabX - draggingNodeRect.width / 2,
      y: transform.y + grabY - draggingNodeRect.height / 2,
    };
  }
  return transform;
};

interface Props {
  book: BookDetail;
  setBook: Dispatch<SetStateAction<BookDetail | null>>;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
  initialPageId?: string | null;
  onPageSelect?: (pageId: string | null) => void;
}

export function PagesTab({ book, setBook, sectionPhotos, loadSectionPhotos, onRefresh, initialPageId, onPageSelect }: Props) {
  const { t } = useTranslation('pages');
  const defaultPageId = initialPageId && book.pages.find(p => p.id === initialPageId)
    ? initialPageId
    : (book.pages.length > 0 ? book.pages[0].id : null);
  const [selectedId, setSelectedId] = useState<string | null>(defaultPageId);
  const [activePhotoUid, setActivePhotoUid] = useState<string | null>(null);
  const [editingPhoto, setEditingPhoto] = useState<{ sectionId: string; photoUid: string } | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  const selectedPage = book.pages.find(p => p.id === selectedId);

  // Load section photos for the selected page's section
  useEffect(() => {
    if (selectedPage?.section_id && !sectionPhotos[selectedPage.section_id]) {
      loadSectionPhotos(selectedPage.section_id);
    }
  }, [selectedPage, sectionPhotos, loadSectionPhotos]);

  // Also load all section photos on mount
  useEffect(() => {
    book.sections.forEach(s => {
      if (!sectionPhotos[s.id]) loadSectionPhotos(s.id);
    });
  }, [book.sections, sectionPhotos, loadSectionPhotos]);

  // Update selection if pages change
  useEffect(() => {
    if (selectedId && !book.pages.find(p => p.id === selectedId)) {
      setSelectedId(book.pages.length > 0 ? book.pages[0].id : null);
    }
  }, [book.pages, selectedId]);

  // Notify parent of page selection changes
  useEffect(() => {
    onPageSelect?.(selectedId);
  }, [selectedId, onPageSelect]);

  // Current section photos for selected page
  const currentSectionPhotos = useMemo(() => {
    if (!selectedPage?.section_id) return [];
    return sectionPhotos[selectedPage.section_id] || [];
  }, [selectedPage, sectionPhotos]);

  // Compute unassigned photos for the selected page's section
  const unassignedPhotos = useMemo(() => {
    if (!selectedPage?.section_id) return [];
    const photos = sectionPhotos[selectedPage.section_id] || [];
    // Gather all photo uids assigned to any page in this section
    const assignedUids = new Set<string>();
    book.pages.forEach(page => {
      if (page.section_id === selectedPage.section_id) {
        page.slots.forEach(s => {
          if (s.photo_uid) assignedUids.add(s.photo_uid);
        });
      }
    });
    return photos.filter(p => !assignedUids.has(p.photo_uid)).map(p => p.photo_uid);
  }, [selectedPage, sectionPhotos, book.pages]);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const data = event.active.data.current as Record<string, unknown> | undefined;
    if (data?.photoUid) {
      setActivePhotoUid(data.photoUid as string);
    }
  }, []);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    setActivePhotoUid(null);
    const { active, over } = event;
    if (!over || !selectedPage) return;

    const activeData = active.data.current as Record<string, unknown> | undefined;
    const targetData = over.data.current as Record<string, unknown> | undefined;
    const photoUid = activeData?.photoUid as string | undefined;
    if (!photoUid || !targetData) return;

    const targetPageId = targetData.pageId as string;
    const targetSlotIndex = targetData.slotIndex as number;
    const targetPhotoUid = targetData.photoUid as string | undefined;

    // Check if dragging from a slot (has source slot info)
    const sourcePageId = activeData?.sourcePageId as string | undefined;
    const sourceSlotIndex = activeData?.sourceSlotIndex as number | undefined;
    const isFromSlot = sourcePageId !== undefined && sourceSlotIndex !== undefined;

    // Don't drop on the same slot
    if (isFromSlot && sourcePageId === targetPageId && sourceSlotIndex === targetSlotIndex) return;

    // Optimistic update: swap/move photos in local state immediately so there's no
    // visual "snap back" while waiting for the API round-trip.
    setBook(prev => {
      if (!prev) return prev;
      const pages = prev.pages.map(p => ({ ...p, slots: p.slots.map(s => ({ ...s })) }));
      if (isFromSlot && targetPhotoUid && sourcePageId === targetPageId) {
        // Same-page swap
        const page = pages.find(p => p.id === sourcePageId);
        if (page) {
          const srcSlot = page.slots.find(s => s.slot_index === sourceSlotIndex);
          const tgtSlot = page.slots.find(s => s.slot_index === targetSlotIndex);
          if (srcSlot && tgtSlot) {
            [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
          }
        }
      } else if (isFromSlot && targetPhotoUid) {
        // Cross-page swap
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
        }
      } else if (isFromSlot) {
        // Move to empty slot
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          tgtSlot.photo_uid = srcSlot.photo_uid;
          srcSlot.photo_uid = '';
        }
      } else {
        // From unassigned pool
        const tgtPage = pages.find(p => p.id === targetPageId);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (tgtSlot) tgtSlot.photo_uid = photoUid;
      }
      return { ...prev, pages };
    });

    try {
      if (isFromSlot && targetPhotoUid && sourcePageId === targetPageId) {
        // Swap: both slots on the same page — atomic swap
        await swapSlots(sourcePageId, sourceSlotIndex, targetSlotIndex);
      } else if (isFromSlot && targetPhotoUid) {
        // Swap across pages — assign each to the other's slot
        await Promise.all([
          assignSlot(targetPageId, targetSlotIndex, photoUid),
          assignSlot(sourcePageId, sourceSlotIndex, targetPhotoUid),
        ]);
      } else if (isFromSlot) {
        // Move: source slot has photo, target is empty — clear old first to avoid unique constraint
        await clearSlot(sourcePageId, sourceSlotIndex);
        await assignSlot(targetPageId, targetSlotIndex, photoUid);
      } else {
        // From unassigned pool — just assign
        await assignSlot(targetPageId, targetSlotIndex, photoUid);
      }
      onRefresh();
    } catch {
      // Revert on error
      onRefresh();
    }
  }, [selectedPage, setBook, onRefresh]);

  const handleClearSlot = useCallback(async (slotIndex: number) => {
    if (!selectedPage) return;
    try {
      await clearSlot(selectedPage.id, slotIndex);
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleEditDescription = useCallback((photoUid: string) => {
    if (!selectedPage?.section_id) return;
    setEditingPhoto({ sectionId: selectedPage.section_id, photoUid });
  }, [selectedPage]);

  const handleUpdatePageDescription = useCallback(async (desc: string) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { description: desc });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleDescSaved = useCallback(() => {
    if (editingPhoto) {
      loadSectionPhotos(editingPhoto.sectionId);
    }
    setEditingPhoto(null);
  }, [editingPhoto, loadSectionPhotos]);

  if (book.pages.length === 0 && !selectedId) {
    return (
      <div className="flex gap-4">
        <PageSidebar
          bookId={book.id}
          pages={book.pages}
          sections={book.sections}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
        />
        <div className="flex-1 text-center text-slate-500 py-12">
          {t('books.editor.noPages')}
        </div>
      </div>
    );
  }

  // Find current editing photo data
  const editingPhotoData = editingPhoto
    ? currentSectionPhotos.find(sp => sp.photo_uid === editingPhoto.photoUid)
    : null;

  return (
    <div className="flex gap-4">
      <PageSidebar
        bookId={book.id}
        pages={book.pages}
        sections={book.sections}
        selectedId={selectedId}
        onSelect={setSelectedId}
        onRefresh={onRefresh}
      />
      <div className="flex-1 space-y-4">
        <DndContext sensors={sensors} collisionDetection={pointerWithin} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
          {selectedPage && (
            <>
              <PageTemplate
                page={selectedPage}
                onClearSlot={handleClearSlot}
                sectionPhotos={currentSectionPhotos}
                onEditDescription={handleEditDescription}
                onUpdatePageDescription={handleUpdatePageDescription}
              />
              <UnassignedPool
                photoUids={unassignedPhotos}
                sectionPhotos={currentSectionPhotos}
                onEditDescription={handleEditDescription}
              />
            </>
          )}
          <DragOverlay modifiers={[snapCenterToCursor]} dropAnimation={null}>
            {activePhotoUid && (
              <div className="w-16 h-16 rounded shadow-lg overflow-hidden opacity-80">
                <img
                  src={getThumbnailUrl(activePhotoUid, 'tile_100')}
                  alt=""
                  className="w-full h-full object-cover"
                />
              </div>
            )}
          </DragOverlay>
        </DndContext>
      </div>

      {editingPhoto && editingPhotoData && (
        <PhotoDescriptionDialog
          sectionId={editingPhoto.sectionId}
          photoUid={editingPhoto.photoUid}
          description={editingPhotoData.description}
          note={editingPhotoData.note}
          onSaved={handleDescSaved}
          onClose={() => setEditingPhoto(null)}
        />
      )}
    </div>
  );
}
