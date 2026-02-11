import { useState, useMemo, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { GripVertical, Plus, Trash2, ChevronDown, ChevronRight } from 'lucide-react';
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core';
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { createPage, deletePage, reorderPages } from '../../api/client';
import type { BookPage, BookSection, PageFormat } from '../../types';
import { pageFormatLabelKey, pageFormatSlotCount } from '../../types';

interface Props {
  bookId: string;
  pages: BookPage[];
  sections: BookSection[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
}

function SortablePageItem({ page, globalIndex, isSelected, onSelect, onDelete }: {
  page: BookPage;
  globalIndex: number;
  isSelected: boolean;
  onSelect: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation('pages');
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: page.id });
  const style = { transform: CSS.Transform.toString(transform), transition };
  const filledSlots = page.slots.filter(s => s.photo_uid).length;
  const totalSlots = pageFormatSlotCount(page.format);
  const isComplete = filledSlots === totalSlots && totalSlots > 0;

  let boxClass: string;
  if (isSelected) {
    boxClass = isComplete
      ? 'bg-emerald-500/20 border-2 border-emerald-400 ring-1 ring-emerald-400/30'
      : 'bg-rose-500/20 border-2 border-rose-400 ring-1 ring-rose-400/30';
  } else if (isComplete) {
    boxClass = 'bg-emerald-500/10 border border-emerald-500/30 hover:border-emerald-400/50';
  } else {
    boxClass = 'bg-slate-800 border border-slate-700 hover:border-slate-600';
  }

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center gap-2 p-2 rounded-md cursor-pointer transition-colors ${boxClass}`}
      onClick={onSelect}
    >
      <button {...attributes} {...listeners} className="text-slate-500 hover:text-slate-300 cursor-grab">
        <GripVertical className="h-4 w-4" />
      </button>
      <div className="flex-1 min-w-0">
        <div className="text-sm font-medium text-white">
          {t('books.editor.pageNumber', { number: globalIndex + 1 })}
        </div>
        <div className={`text-xs ${isComplete ? 'text-emerald-400' : 'text-slate-500'}`}>
          {t(pageFormatLabelKey(page.format))} · {filledSlots}/{totalSlots}
        </div>
      </div>
      <button
        onClick={(e) => { e.stopPropagation(); onDelete(); }}
        className="text-slate-500 hover:text-red-400 p-0.5"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

interface SectionGroup {
  section: BookSection;
  pages: BookPage[];
  globalIndices: number[];
}

export function PageSidebar({ bookId, pages, sections, selectedId, onSelect, onRefresh }: Props) {
  const { t } = useTranslation('pages');
  const [newFormat, setNewFormat] = useState<PageFormat>('4_landscape');
  const [newSectionId, setNewSectionId] = useState('');
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({});

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // Compute global page number map and section groups
  const { sectionGroups, pageNumberMap } = useMemo(() => {
    const numberMap = new Map<string, number>();
    pages.forEach((page, i) => numberMap.set(page.id, i));

    const groups: SectionGroup[] = sections.map(section => {
      const sectionPages = pages.filter(p => p.section_id === section.id);
      const indices = sectionPages.map(p => numberMap.get(p.id)!);
      return { section, pages: sectionPages, globalIndices: indices };
    });

    // Add pages without a section (shouldn't happen, but handle gracefully)
    const assignedIds = new Set(sections.map(s => s.id));
    const unassigned = pages.filter(p => !assignedIds.has(p.section_id || ''));
    if (unassigned.length > 0) {
      groups.push({
        section: { id: '__unassigned__', title: t('books.editor.noSection'), sort_order: 9999, photo_count: 0 },
        pages: unassigned,
        globalIndices: unassigned.map(p => numberMap.get(p.id)!),
      });
    }

    return { sectionGroups: groups, pageNumberMap: numberMap };
  }, [pages, sections, bookId, t]);

  // Auto-expand section when a page is selected but its section is collapsed
  useEffect(() => {
    if (!selectedId) return;
    const selectedPage = pages.find(p => p.id === selectedId);
    if (!selectedPage) return;
    const sectionId = selectedPage.section_id || '__unassigned__';
    if (collapsedSections[sectionId]) {
      setCollapsedSections(prev => ({ ...prev, [sectionId]: false }));
    }
  }, [selectedId]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggleSection = (sectionId: string) => {
    setCollapsedSections(prev => ({ ...prev, [sectionId]: !prev[sectionId] }));
  };

  const handleAdd = async () => {
    if (!newSectionId) return;
    try {
      const page = await createPage(bookId, newFormat, newSectionId);
      onRefresh();
      onSelect(page.id);
    } catch { /* silent */ }
  };

  const handleDelete = async (id: string) => {
    try {
      await deletePage(id);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const activePage = pages.find(p => p.id === active.id);
    const overPage = pages.find(p => p.id === over.id);
    if (!activePage || !overPage) return;

    // Block cross-section drag
    if (activePage.section_id !== overPage.section_id) return;

    const oldIndex = pages.findIndex(p => p.id === active.id);
    const newIndex = pages.findIndex(p => p.id === over.id);
    const reordered = arrayMove(pages, oldIndex, newIndex);
    try {
      await reorderPages(bookId, reordered.map(p => p.id));
      onRefresh();
    } catch { /* silent */ }
  };

  return (
    <div className="w-64 shrink-0 flex flex-col gap-2">
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        {sectionGroups.map(group => {
          const sectionId = group.section.id;
          const isCollapsed = collapsedSections[sectionId];
          const pageIds = group.pages.map(p => p.id);

          return (
            <div key={sectionId}>
              {/* Section header */}
              <button
                onClick={() => toggleSection(sectionId)}
                className="w-full flex items-center gap-1.5 px-1 py-1.5 text-left group"
              >
                {isCollapsed
                  ? <ChevronRight className="h-3.5 w-3.5 text-slate-500 shrink-0" />
                  : <ChevronDown className="h-3.5 w-3.5 text-slate-500 shrink-0" />
                }
                <span className="text-xs font-medium text-rose-400 truncate flex-1">
                  {group.section.title}
                </span>
                <span className="text-xs text-slate-500 shrink-0">
                  {t('books.editor.sectionPageCount', { count: group.pages.length })}
                </span>
              </button>

              {/* Pages (collapsible) */}
              {!isCollapsed && (
                <SortableContext items={pageIds} strategy={verticalListSortingStrategy}>
                  <div className="flex flex-col gap-1 ml-2">
                    {group.pages.map((page) => (
                      <SortablePageItem
                        key={page.id}
                        page={page}
                        globalIndex={pageNumberMap.get(page.id) ?? 0}
                        isSelected={page.id === selectedId}
                        onSelect={() => onSelect(page.id)}
                        onDelete={() => handleDelete(page.id)}
                      />
                    ))}
                  </div>
                </SortableContext>
              )}
            </div>
          );
        })}
      </DndContext>

      <div className="mt-2 space-y-1.5">
        <select
          value={newFormat}
          onChange={(e) => setNewFormat(e.target.value as PageFormat)}
          className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
        >
          <option value="4_landscape">{t('books.editor.format4Landscape')}</option>
          <option value="2l_1p">{t('books.editor.format2l1p')}</option>
          <option value="1p_2l">{t('books.editor.format1p2l')}</option>
          <option value="2_portrait">{t('books.editor.format2Portrait')}</option>
          <option value="1_fullscreen">{t('books.editor.format1Fullscreen')}</option>
        </select>
        <select
          value={newSectionId}
          onChange={(e) => setNewSectionId(e.target.value)}
          className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
        >
          <option value="" disabled>{t('books.editor.selectSection')}</option>
          {sections.map(s => (
            <option key={s.id} value={s.id}>{s.title}</option>
          ))}
        </select>
        <button
          onClick={handleAdd}
          disabled={!newSectionId}
          className={`w-full flex items-center justify-center gap-1.5 px-3 py-1.5 text-white text-sm rounded transition-colors ${
            newSectionId ? 'bg-rose-600 hover:bg-rose-700' : 'bg-slate-700 cursor-not-allowed opacity-50'
          }`}
        >
          <Plus className="h-4 w-4" />
          {t('books.editor.addPage')}
        </button>
      </div>
    </div>
  );
}
