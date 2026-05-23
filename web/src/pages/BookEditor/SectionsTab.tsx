import { useState, useEffect, useCallback, type Dispatch, type SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import {
  DndContext,
  DragOverlay,
  MouseSensor,
  TouchSensor,
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
import { useToast } from '../../components/Toast';
import { SectionSidebar } from './SectionSidebar';
import { SectionPhotoPool } from './SectionPhotoPool';
import {
  removeSectionPhotos, addSectionPhotos,
  reorderSections, reorderChapters, updateSection,
  getThumbnailUrl,
} from '../../api/client';
import type { BookDetail, BookSection, SectionPhoto } from '../../types';

interface Props {
  book: BookDetail;
  setBook: Dispatch<SetStateAction<BookDetail | null>>;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  onRefresh: () => void;
  initialSectionId?: string | null;
}

// Reorder a single chapter group within the full section list so the API call
// can post a flat ordering. Returns the full new list.
function reorderWithinChapter(
  sections: BookSection[],
  chapterId: string,
  activeId: string,
  overId: string,
): BookSection[] | null {
  const group = sections.filter(s => (s.chapter_id || '') === (chapterId || ''));
  const oldIndex = group.findIndex(s => s.id === activeId);
  const newIndex = group.findIndex(s => s.id === overId);
  if (oldIndex === -1 || newIndex === -1) return null;
  const reorderedGroup = arrayMove(group, oldIndex, newIndex);
  const next: BookSection[] = [];
  let inserted = false;
  for (const s of sections) {
    if ((s.chapter_id || '') === (chapterId || '')) {
      if (!inserted) {
        next.push(...reorderedGroup);
        inserted = true;
      }
    } else {
      next.push(s);
    }
  }
  return next;
}

export function SectionsTab({ book, setBook, sectionPhotos, loadSectionPhotos, onRefresh, initialSectionId }: Props) {
  const { t } = useTranslation('pages');
  const toast = useToast();
  const [selectedId, setSelectedId] = useState<string | null>(
    (initialSectionId && book.sections.find(s => s.id === initialSectionId)) ? initialSectionId :
    book.sections.length > 0 ? book.sections[0].id : null
  );

  // Cross-section photo drag state
  const [activeDragPhotos, setActiveDragPhotos] = useState<string[]>([]);
  const [dragSourceSectionId, setDragSourceSectionId] = useState<string | null>(null);
  const [overSectionId, setOverSectionId] = useState<string | null>(null);
  const isPhotoDragging = activeDragPhotos.length > 0;

  // Active sortable drag (chapter or section) for the DragOverlay
  const [activeSortableId, setActiveSortableId] = useState<string | null>(null);
  // Chapter currently being hovered while a section is being dragged
  const [overChapterId, setOverChapterId] = useState<string | null>(null);

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

  // MouseSensor for desktop (small drag distance), TouchSensor for long-press on touch.
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // Collision detection per drag type:
  // - photo drags target section rows;
  // - chapter drags target chapter rows;
  // - section drags target other sections OR chapter rows (cross-chapter move).
  const customCollision: CollisionDetection = useCallback((args) => {
    const activeId = String(args.active.id);
    const collisions = closestCenter(args);
    if (activeId.startsWith('photo-')) {
      return collisions.filter(c => String(c.id).startsWith('section-'));
    }
    if (activeId.startsWith('chapter-')) {
      return collisions.filter(c => String(c.id).startsWith('chapter-'));
    }
    // section-* drag: accept section-* AND chapter-* drop targets
    return collisions.filter(c => {
      const id = String(c.id);
      return id.startsWith('section-') || id.startsWith('chapter-');
    });
  }, []);

  const handleDragStart = (event: DragStartEvent) => {
    const data = event.active.data.current;
    if (data?.type === 'photo') {
      setActiveDragPhotos(data.selectedUids as string[]);
      setDragSourceSectionId(data.sourceSectionId as string);
      return;
    }
    setActiveSortableId(String(event.active.id));
  };

  const handleDragOver = (event: DragOverEvent) => {
    const activeId = String(event.active.id);
    const overId = event.over ? String(event.over.id) : null;
    if (activeId.startsWith('photo-')) {
      if (overId?.startsWith('section-')) {
        setOverSectionId(overId.replace('section-', ''));
      } else {
        setOverSectionId(null);
      }
      return;
    }
    if (activeId.startsWith('section-')) {
      // Highlight a chapter only when we'd actually be moving the section into a new chapter.
      if (overId?.startsWith('chapter-')) {
        const targetChapterId = overId.replace('chapter-', '');
        const section = book.sections.find(s => s.id === activeId.replace('section-', ''));
        if (section && section.chapter_id !== targetChapterId) {
          setOverChapterId(targetChapterId);
          return;
        }
      }
      setOverChapterId(null);
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
            } catch {
              toast.error(t('books.editor.moveFailed'));
            }
          }
        }
      }
      setActiveDragPhotos([]);
      setDragSourceSectionId(null);
      setOverSectionId(null);
      return;
    }

    setActiveSortableId(null);
    setOverChapterId(null);

    if (!over) return;
    const overId = String(over.id);
    const chapters = book.chapters || [];

    // Chapter reorder
    if (activeId.startsWith('chapter-') && overId.startsWith('chapter-')) {
      if (active.id === over.id) return;
      const activeChId = activeId.replace('chapter-', '');
      const overChId = overId.replace('chapter-', '');
      const oldIndex = chapters.findIndex(c => c.id === activeChId);
      const newIndex = chapters.findIndex(c => c.id === overChId);
      if (oldIndex === -1 || newIndex === -1) return;
      const reordered = arrayMove(chapters, oldIndex, newIndex);
      const prevChapters = chapters;
      // Optimistic update
      setBook(prev => prev ? { ...prev, chapters: reordered } : prev);
      try {
        await reorderChapters(book.id, reordered.map(c => c.id));
        onRefresh();
      } catch {
        setBook(prev => prev ? { ...prev, chapters: prevChapters } : prev);
        toast.error(t('books.editor.reorderFailed'));
      }
      return;
    }

    if (!activeId.startsWith('section-')) return;
    const activeSectionId = activeId.replace('section-', '');
    const activeSection = book.sections.find(s => s.id === activeSectionId);
    if (!activeSection) return;

    // Section dropped on chapter row → reassign chapter
    if (overId.startsWith('chapter-')) {
      const targetChapterId = overId.replace('chapter-', '');
      if (activeSection.chapter_id === targetChapterId) return;
      const prevSections = book.sections;
      // Optimistic update — flip chapter_id locally
      setBook(prev => prev ? {
        ...prev,
        sections: prev.sections.map(s =>
          s.id === activeSectionId ? { ...s, chapter_id: targetChapterId } : s
        ),
      } : prev);
      try {
        await updateSection(activeSectionId, { chapter_id: targetChapterId });
        onRefresh();
      } catch {
        setBook(prev => prev ? { ...prev, sections: prevSections } : prev);
        toast.error(t('books.editor.moveFailed'));
      }
      return;
    }

    // Section dropped on another section → reorder within the same chapter group
    if (overId.startsWith('section-')) {
      if (active.id === over.id) return;
      const overSectionIdStr = overId.replace('section-', '');
      const overSection = book.sections.find(s => s.id === overSectionIdStr);
      if (!overSection) return;
      // Cross-chapter via section drop: reassign chapter_id to the target's chapter
      // (then append at end of that group — backend resequences sort_order).
      if ((activeSection.chapter_id || '') !== (overSection.chapter_id || '')) {
        const targetChapterId = overSection.chapter_id || '';
        const prevSections = book.sections;
        setBook(prev => prev ? {
          ...prev,
          sections: prev.sections.map(s =>
            s.id === activeSectionId ? { ...s, chapter_id: targetChapterId } : s
          ),
        } : prev);
        try {
          await updateSection(activeSectionId, { chapter_id: targetChapterId });
          onRefresh();
        } catch {
          setBook(prev => prev ? { ...prev, sections: prevSections } : prev);
          toast.error(t('books.editor.moveFailed'));
        }
        return;
      }
      // Same-chapter reorder
      const chapterId = activeSection.chapter_id || '';
      const reordered = reorderWithinChapter(book.sections, chapterId, activeSectionId, overSectionIdStr);
      if (!reordered) return;
      const prevSections = book.sections;
      setBook(prev => prev ? { ...prev, sections: reordered } : prev);
      try {
        await reorderSections(book.id, reordered.map(s => s.id));
        onRefresh();
      } catch {
        setBook(prev => prev ? { ...prev, sections: prevSections } : prev);
        toast.error(t('books.editor.reorderFailed'));
      }
    }
  };

  const handleDragCancel = () => {
    setActiveDragPhotos([]);
    setDragSourceSectionId(null);
    setOverSectionId(null);
    setActiveSortableId(null);
    setOverChapterId(null);
  };

  const noSections = book.sections.length === 0 && !selectedId;

  // Resolve the active dragged sortable for the overlay preview.
  const activeSection = activeSortableId?.startsWith('section-')
    ? book.sections.find(s => s.id === activeSortableId.replace('section-', ''))
    : null;
  const activeChapter = activeSortableId?.startsWith('chapter-')
    ? book.chapters.find(c => c.id === activeSortableId.replace('chapter-', ''))
    : null;

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
          overChapterId={overChapterId}
          activeSortableId={activeSortableId}
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
        {!isPhotoDragging && activeSection && (
          <div className="px-2 py-1.5 rounded-md bg-slate-800 border border-rose-400/60 shadow-lg text-sm text-white opacity-90 max-w-xs truncate">
            {activeSection.title}
          </div>
        )}
        {!isPhotoDragging && activeChapter && (
          <div className="px-2 py-1.5 rounded-md bg-slate-700 border border-rose-400/60 shadow-lg text-xs font-semibold text-slate-200 uppercase tracking-wide opacity-90 max-w-xs truncate">
            {activeChapter.title}
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}
