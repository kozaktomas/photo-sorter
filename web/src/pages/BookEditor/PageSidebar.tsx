import { useState, useMemo, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { GripVertical, Plus, Trash2, ChevronDown, ChevronRight, Type } from 'lucide-react';
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { useDndContext } from '@dnd-kit/core';
import { CSS } from '@dnd-kit/utilities';
import { createPage, deletePage, getThumbnailUrl } from '../../api/client';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import type { BookChapter, BookPage, BookSection, PageFormat } from '../../types';
import { pageFormatLabelKey, pageFormatSlotCount } from '../../types';

interface Props {
  bookId: string;
  pages: BookPage[];
  chapters: BookChapter[];
  sections: BookSection[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
  isPhotoDragActive?: boolean;
}

function SlotThumbnail({ slot }: { slot?: { photo_uid: string; text_content: string } }) {
  if (slot?.photo_uid) {
    return (
      <img
        src={getThumbnailUrl(slot.photo_uid, 'tile_50')}
        className="w-5 h-5 rounded-sm object-cover bg-slate-700"
        loading="lazy"
        onError={(e) => { (e.target as HTMLImageElement).style.visibility = 'hidden'; }}
      />
    );
  }
  if (slot?.text_content) {
    return (
      <div className="w-5 h-5 rounded-sm bg-slate-600 flex items-center justify-center">
        <Type className="w-3 h-3 text-slate-400" />
      </div>
    );
  }
  return (
    <div className="w-5 h-5 rounded-sm border border-dashed border-slate-600" />
  );
}

function SortablePageItem({ page, globalIndex, isSelected, onSelect, onDelete, isPhotoDragActive, scrollRef }: {
  page: BookPage;
  globalIndex: number;
  isSelected: boolean;
  onSelect: () => void;
  onDelete: () => void;
  isPhotoDragActive?: boolean;
  scrollRef?: (el: HTMLDivElement | null) => void;
}) {
  const { t } = useTranslation('pages');
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({
    id: page.id,
    data: { type: 'page-reorder', pageId: page.id, sectionId: page.section_id },
  });
  const combinedRef = useCallback((el: HTMLDivElement | null) => {
    setNodeRef(el);
    scrollRef?.(el);
  }, [setNodeRef, scrollRef]);
  const { over } = useDndContext();
  const isOver = over?.id === page.id;
  const style = { transform: CSS.Transform.toString(transform), transition };
  const filledSlots = page.slots.filter(s => s.photo_uid || s.text_content).length;
  const totalSlots = pageFormatSlotCount(page.format);
  const isComplete = filledSlots === totalSlots && totalSlots > 0;
  const hasEmptySlots = filledSlots < totalSlots;

  // Photo drag hover styling
  const isPhotoHover = isOver && isPhotoDragActive;

  let boxClass: string;
  if (isPhotoHover && hasEmptySlots) {
    boxClass = 'bg-rose-500/20 border-2 border-rose-400 ring-1 ring-rose-400/30';
  } else if (isPhotoHover && !hasEmptySlots) {
    boxClass = 'bg-slate-800/50 border border-slate-600 opacity-50';
  } else if (isSelected) {
    boxClass = isComplete
      ? 'bg-emerald-500/20 border-2 border-emerald-400 ring-1 ring-emerald-400/30'
      : 'bg-rose-500/20 border-2 border-rose-400 ring-1 ring-rose-400/30';
  } else if (isComplete) {
    boxClass = 'bg-emerald-500/10 border border-emerald-500/30 hover:border-emerald-400/50';
  } else {
    boxClass = 'bg-slate-800 border border-slate-700 hover:border-slate-600';
  }

  // Build slot array padded to totalSlots
  const slots = Array.from({ length: totalSlots }, (_, i) =>
    page.slots.find(s => s.slot_index === i)
  );

  return (
    <div
      ref={combinedRef}
      style={style}
      className={`flex items-center gap-1.5 p-1.5 rounded-md cursor-pointer transition-colors ${boxClass}`}
      onClick={onSelect}
      title={`${t(pageFormatLabelKey(page.format))} · ${filledSlots}/${totalSlots}`}
    >
      <button {...attributes} {...listeners} className="text-slate-500 hover:text-slate-300 cursor-grab shrink-0">
        <GripVertical className="h-4 w-4" />
      </button>
      <span className="text-xs font-medium text-slate-400 w-4 text-center shrink-0">{globalIndex + 1}</span>
      <div className="flex items-center gap-[3px] flex-1 min-w-0">
        {slots.map((slot, i) => (
          <SlotThumbnail key={i} slot={slot} />
        ))}
      </div>
      <button
        onClick={(e) => { e.stopPropagation(); onDelete(); }}
        className="text-slate-500 hover:text-red-400 p-0.5 shrink-0"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

const PAGE_FORMATS: PageFormat[] = ['4_landscape', '2l_1p', '1p_2l', '2_portrait', '1_fullscreen'];
const FORMAT_LABEL_KEYS: Record<PageFormat, string> = {
  '4_landscape': 'books.editor.format4Landscape',
  '2l_1p': 'books.editor.format2l1p',
  '1p_2l': 'books.editor.format1p2l',
  '2_portrait': 'books.editor.format2Portrait',
  '1_fullscreen': 'books.editor.format1Fullscreen',
};

function QuickAddButton({ bookId, sectionId, openSectionId, onToggle, onCreated }: {
  bookId: string;
  sectionId: string;
  openSectionId: string | null;
  onToggle: (id: string | null) => void;
  onCreated: (pageId: string) => void;
}) {
  const { t } = useTranslation('pages');
  const popoverRef = useRef<HTMLDivElement>(null);
  const isOpen = openSectionId === sectionId;

  // Close on outside click
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        onToggle(null);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [isOpen, onToggle]);

  // Close on Escape
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onToggle(null);
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [isOpen, onToggle]);

  const handleSelect = async (format: PageFormat) => {
    onToggle(null);
    try {
      const page = await createPage(bookId, format, sectionId);
      onCreated(page.id);
    } catch { /* silent */ }
  };

  return (
    <div className="relative" ref={popoverRef}>
      <button
        onClick={(e) => { e.stopPropagation(); onToggle(isOpen ? null : sectionId); }}
        className="text-slate-500 hover:text-rose-400 transition-colors shrink-0"
        title={t('books.editor.addPage')}
      >
        <Plus className="h-4 w-4" />
      </button>
      {isOpen && (
        <div className="absolute left-0 top-full mt-1 z-50 bg-slate-800 border border-slate-600 rounded-md shadow-lg py-1 min-w-[180px]">
          {PAGE_FORMATS.map(format => (
            <button
              key={format}
              onClick={(e) => { e.stopPropagation(); void handleSelect(format); }}
              className="w-full text-left px-3 py-1.5 text-sm text-slate-300 hover:bg-slate-700 hover:text-white transition-colors"
            >
              {t(FORMAT_LABEL_KEYS[format])}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

interface SectionGroup {
  section: BookSection;
  pages: BookPage[];
  globalIndices: number[];
}

export function PageSidebar({ bookId, pages, chapters, sections, selectedId, onSelect, onRefresh, isPhotoDragActive }: Props) {
  const { t } = useTranslation('pages');
  const [newFormat, setNewFormat] = useState<PageFormat>('4_landscape');
  const [newSectionId, setNewSectionId] = useState('');
  const [openQuickAdd, setOpenQuickAdd] = useState<string | null>(null);
  const handleQuickAddToggle = useCallback((id: string | null) => setOpenQuickAdd(id), []);
  const handleQuickAddCreated = useCallback((pageId: string) => {
    onRefresh();
    onSelect(pageId);
  }, [onRefresh, onSelect]);
  const selectedItemRefs = useRef<Map<string, HTMLDivElement>>(new Map());

  // Scroll selected item into view
  useEffect(() => {
    if (!selectedId) return;
    const el = selectedItemRefs.current.get(selectedId);
    if (el) {
      el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedId]);
  const storageKey = `page-sidebar-collapsed:${bookId}`;
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      return stored ? JSON.parse(stored) as Record<string, boolean> : {};
    } catch {
      return {};
    }
  });

  // Persist collapse state to localStorage
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(collapsedSections));
    } catch { /* quota exceeded or private browsing */ }
  }, [collapsedSections, storageKey]);

  // Reset state when switching books
  useEffect(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      setCollapsedSections(stored ? JSON.parse(stored) as Record<string, boolean> : {});
    } catch {
      setCollapsedSections({});
    }
  }, [storageKey]);

  // Build chapter lookup for section display titles
  const chapterMap = useMemo(() => {
    const map: Record<string, string> = {};
    chapters.forEach(ch => { map[ch.id] = ch.title; });
    return map;
  }, [chapters]);

  const sectionDisplayTitle = (section: BookSection) => {
    const chapterTitle = section.chapter_id ? chapterMap[section.chapter_id] : undefined;
    return chapterTitle ? `${chapterTitle} | ${section.title}` : section.title;
  };

  // Compute global page number map and section groups
  // Pages are numbered sequentially (1..n) based on visual display order:
  // all pages in section 1, then all pages in section 2, etc.
  const { sectionGroups, pageNumberMap } = useMemo(() => {
    const groups: SectionGroup[] = sections.map(section => {
      const sectionPages = pages.filter(p => p.section_id === section.id);
      return { section, pages: sectionPages, globalIndices: [] };
    });

    // Add pages without a section (shouldn't happen, but handle gracefully)
    const assignedIds = new Set(sections.map(s => s.id));
    const unassigned = pages.filter(p => !assignedIds.has(p.section_id || ''));
    if (unassigned.length > 0) {
      groups.push({
        section: { id: '__unassigned__', chapter_id: '', title: t('books.editor.noSection'), sort_order: 9999, photo_count: 0 },
        pages: unassigned,
        globalIndices: [],
      });
    }

    // Number pages sequentially based on visual order (section by section)
    const numberMap = new Map<string, number>();
    let counter = 0;
    for (const group of groups) {
      group.globalIndices = group.pages.map(p => {
        numberMap.set(p.id, counter);
        return counter++;
      });
    }

    return { sectionGroups: groups, pageNumberMap: numberMap };
  }, [pages, sections, t]);

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

  const [deleteTargetId, setDeleteTargetId] = useState<string | null>(null);

  const handleDelete = (id: string) => {
    setDeleteTargetId(id);
  };

  const confirmDelete = async () => {
    if (!deleteTargetId) return;
    const id = deleteTargetId;
    setDeleteTargetId(null);
    try {
      await deletePage(id);
      onRefresh();
    } catch { /* silent */ }
  };

  return (
    <div className="w-64 shrink-0 flex flex-col gap-2 max-h-[calc(100vh-12rem)]">
      <div className="flex-1 overflow-y-auto min-h-0 flex flex-col gap-2 pr-1">
        {sectionGroups.map(group => {
          const sectionId = group.section.id;
          const isCollapsed = collapsedSections[sectionId];
          const pageIds = group.pages.map(p => p.id);

          return (
            <div key={sectionId}>
              {/* Section header */}
              <div
                onClick={() => toggleSection(sectionId)}
                className="w-full flex items-center gap-1.5 px-1 py-1.5 text-left group cursor-pointer"
              >
                {isCollapsed
                  ? <ChevronRight className="h-3.5 w-3.5 text-slate-500 shrink-0" />
                  : <ChevronDown className="h-3.5 w-3.5 text-slate-500 shrink-0" />
                }
                <span className="text-xs font-medium text-rose-400 truncate flex-1">
                  {sectionDisplayTitle(group.section)}
                </span>
                {sectionId !== '__unassigned__' && (
                  <QuickAddButton
                    bookId={bookId}
                    sectionId={sectionId}
                    openSectionId={openQuickAdd}
                    onToggle={handleQuickAddToggle}
                    onCreated={handleQuickAddCreated}
                  />
                )}
                <span className="text-xs text-slate-500 shrink-0">
                  {t('books.editor.sectionPageCount', { count: group.pages.length })}
                </span>
              </div>

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
                        isPhotoDragActive={isPhotoDragActive}
                        scrollRef={page.id === selectedId ? (el) => {
                          if (el) selectedItemRefs.current.set(page.id, el);
                          else selectedItemRefs.current.delete(page.id);
                        } : undefined}
                      />
                    ))}
                  </div>
                </SortableContext>
              )}
            </div>
          );
        })}
      </div>

      <div className="shrink-0 space-y-1.5">
        <select
          value={newFormat}
          onChange={(e) => setNewFormat(e.target.value as PageFormat)}
          className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
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
          className="w-full px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
        >
          <option value="" disabled>{t('books.editor.selectSection')}</option>
          {sections.map(s => (
            <option key={s.id} value={s.id}>{sectionDisplayTitle(s)}</option>
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

      <ConfirmDialog
        open={deleteTargetId !== null}
        title={t('common:buttons.delete')}
        message={t('books.editor.deletePageConfirm')}
        confirmLabel={t('common:buttons.delete')}
        variant="danger"
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTargetId(null)}
      />
    </div>
  );
}
