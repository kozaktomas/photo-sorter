import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  KeyboardSensor,
  useSensor,
  useSensors,
  closestCenter,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
  type CollisionDetection,
} from '@dnd-kit/core';
import { sortableKeyboardCoordinates, arrayMove } from '@dnd-kit/sortable';
import { useBookKeyboardNav } from '../../hooks/useBookKeyboardNav';
import { SectionSidebar } from './SectionSidebar';
import { SectionPhotoPool } from './SectionPhotoPool';
import {
  removeSectionPhotos, addSectionPhotos,
  reorderSections, reorderChapters,
  getThumbnailUrl,
} from '../../api/client';
import type { BookDetail, SectionPhoto } from '../../types';

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
  initialSectionId?: string | null;
}

export function SectionsTab({ book, sectionPhotos, loadSectionPhotos, onRefresh, initialSectionId }: Props) {
  const { t } = useTranslation('pages');
  const [selectedId, setSelectedId] = useState<string | null>(
    (initialSectionId && book.sections.find(s => s.id === initialSectionId)) ? initialSectionId :
    book.sections.length > 0 ? book.sections[0].id : null
  );

  // Cross-section photo drag state
  const [activeDragPhotos, setActiveDragPhotos] = useState<string[]>([]);
  const [dragSourceSectionId, setDragSourceSectionId] = useState<string | null>(null);
  const [overSectionId, setOverSectionId] = useState<string | null>(null);
  const isPhotoDragging = activeDragPhotos.length > 0;

  // Load photos when selection changes
  useEffect(() => {
    if (selectedId && !sectionPhotos[selectedId]) {
      loadSectionPhotos(selectedId);
    }
  }, [selectedId, sectionPhotos, loadSectionPhotos]);

  // Update selection if sections change
  useEffect(() => {
    if (selectedId && !book.sections.find(s => s.id === selectedId)) {
      setSelectedId(book.sections.length > 0 ? book.sections[0].id : null);
    }
  }, [book.sections, selectedId]);

  // Keyboard navigation: W/S = prev/next section, E/D = prev/next chapter
  useBookKeyboardNav({
    items: book.sections,
    selectedId,
    onSelect: setSelectedId,
    getId: section => section.id,
    getChapterId: section => section.chapter_id || '',
    chapters: book.chapters || [],
  });

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // Photo drags match section targets; sortable drags match same type
  const customCollision: CollisionDetection = useCallback((args) => {
    const activeId = String(args.active.id);
    const collisions = closestCenter(args);
    if (activeId.startsWith('photo-')) {
      return collisions.filter(c => String(c.id).startsWith('section-'));
    }
    const prefix = activeId.startsWith('chapter-') ? 'chapter-' : 'section-';
    return collisions.filter(c => String(c.id).startsWith(prefix));
  }, []);

  const handleDragStart = (event: DragStartEvent) => {
    const data = event.active.data.current;
    if (data?.type === 'photo') {
      setActiveDragPhotos(data.selectedUids as string[]);
      setDragSourceSectionId(data.sourceSectionId as string);
    }
  };

  const handleDragOver = (event: DragOverEvent) => {
    if (!String(event.active.id).startsWith('photo-')) return;
    const overId = event.over ? String(event.over.id) : null;
    if (overId?.startsWith('section-')) {
      setOverSectionId(overId.replace('section-', ''));
    } else {
      setOverSectionId(null);
    }
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    const activeId = String(active.id);

    // Photo drop on section
    if (activeId.startsWith('photo-')) {
      if (over && dragSourceSectionId) {
        const targetId = String(over.id);
        if (targetId.startsWith('section-')) {
          const targetSectionId = targetId.replace('section-', '');
          if (targetSectionId !== dragSourceSectionId) {
            try {
              await removeSectionPhotos(dragSourceSectionId, activeDragPhotos);
              await addSectionPhotos(targetSectionId, activeDragPhotos);
              loadSectionPhotos(targetSectionId);
              loadSectionPhotos(dragSourceSectionId);
              onRefresh();
            } catch { /* silent */ }
          }
        }
      }
      setActiveDragPhotos([]);
      setDragSourceSectionId(null);
      setOverSectionId(null);
      return;
    }

    // Section/chapter reorder
    if (!over || active.id === over.id) return;
    const overId = String(over.id);
    const chapters = book.chapters || [];

    if (activeId.startsWith('chapter-') && overId.startsWith('chapter-')) {
      const activeChId = activeId.replace('chapter-', '');
      const overChId = overId.replace('chapter-', '');
      const oldIndex = chapters.findIndex(c => c.id === activeChId);
      const newIndex = chapters.findIndex(c => c.id === overChId);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(chapters, oldIndex, newIndex);
      try {
        await reorderChapters(book.id, reordered.map(c => c.id));
        onRefresh();
      } catch { /* silent */ }
    } else if (activeId.startsWith('section-') && overId.startsWith('section-')) {
      const activeSectionId = activeId.replace('section-', '');
      const overSectionIdStr = overId.replace('section-', '');
      const activeSection = book.sections.find(s => s.id === activeSectionId);
      if (!activeSection) return;
      const chapterId = activeSection.chapter_id || null;
      const groupSections = book.sections.filter(s => (s.chapter_id || null) === chapterId);
      const oldIndex = groupSections.findIndex(s => s.id === activeSectionId);
      const newIndex = groupSections.findIndex(s => s.id === overSectionIdStr);
      if (oldIndex === -1 || newIndex === -1) return;
      const reorderedGroup = arrayMove(groupSections, oldIndex, newIndex);
      const reordered: typeof book.sections = [];
      let groupInserted = false;
      for (const s of book.sections) {
        if ((s.chapter_id || null) === chapterId) {
          if (!groupInserted) {
            reordered.push(...reorderedGroup);
            groupInserted = true;
          }
        } else {
          reordered.push(s);
        }
      }
      try {
        await reorderSections(book.id, reordered.map(s => s.id));
        onRefresh();
      } catch { /* silent */ }
    }
  };

  const handleDragCancel = () => {
    setActiveDragPhotos([]);
    setDragSourceSectionId(null);
    setOverSectionId(null);
  };

  const noSections = book.sections.length === 0 && !selectedId;

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={customCollision}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <div className="flex gap-4">
        <SectionSidebar
          bookId={book.id}
          chapters={book.chapters || []}
          sections={book.sections}
          pages={book.pages || []}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
          isPhotoDragging={isPhotoDragging}
          dragSourceSectionId={dragSourceSectionId}
          overSectionId={overSectionId}
        />
        {noSections ? (
          <div className="flex-1 text-center text-slate-500 py-12">
            {t('books.editor.noSections')}
          </div>
        ) : selectedId ? (
          <SectionPhotoPool
            sectionId={selectedId}
            photos={sectionPhotos[selectedId] || []}
            onRefresh={onRefresh}
            onReloadPhotos={() => loadSectionPhotos(selectedId)}
          />
        ) : null}
      </div>
      <DragOverlay dropAnimation={null}>
        {isPhotoDragging && (
          <div className="relative">
            <img
              src={getThumbnailUrl(activeDragPhotos[0], 'tile_50')}
              alt=""
              className="w-12 h-12 rounded object-cover opacity-80"
            />
            {activeDragPhotos.length > 1 && (
              <span className="absolute -top-2 -right-2 bg-rose-500 text-white text-xs font-bold rounded-full w-5 h-5 flex items-center justify-center">
                {activeDragPhotos.length}
              </span>
            )}
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}
