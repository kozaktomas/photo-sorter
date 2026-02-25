import { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { GripVertical, Plus, Trash2, Image, Pencil, Check, X, ChevronDown, ChevronRight, BookOpen } from 'lucide-react';
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
import {
  createSection, deleteSection, reorderSections, updateSection,
  createChapter, updateChapter, deleteChapter, reorderChapters,
} from '../../api/client';
import type { BookSection, BookChapter } from '../../types';

interface Props {
  bookId: string;
  chapters: BookChapter[];
  sections: BookSection[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
}

function SortableItem({ section, isSelected, onSelect, onDelete, onRename, onMoveToChapter, chapters }: {
  section: BookSection;
  isSelected: boolean;
  onSelect: () => void;
  onDelete: () => void;
  onRename: (title: string) => void;
  onMoveToChapter: (chapterId: string) => void;
  chapters: BookChapter[];
}) {
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: section.id });
  const style = { transform: CSS.Transform.toString(transform), transition };
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState(section.title);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) inputRef.current?.focus();
  }, [editing]);

  const handleSave = () => {
    const trimmed = editTitle.trim();
    if (trimmed && trimmed !== section.title) {
      onRename(trimmed);
    }
    setEditing(false);
  };

  const handleCancel = () => {
    setEditTitle(section.title);
    setEditing(false);
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center gap-2 p-2 rounded-md cursor-pointer transition-colors ${
        isSelected ? 'bg-rose-500/20 border border-rose-500/40' : 'bg-slate-800 border border-slate-700 hover:border-slate-600'
      }`}
      onClick={onSelect}
    >
      <button {...attributes} {...listeners} className="text-slate-500 hover:text-slate-300 cursor-grab">
        <GripVertical className="h-4 w-4" />
      </button>
      <div className="flex-1 min-w-0">
        {editing ? (
          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <input
              ref={inputRef}
              type="text"
              value={editTitle}
              onChange={(e) => setEditTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave();
                if (e.key === 'Escape') handleCancel();
              }}
              className="flex-1 min-w-0 px-1.5 py-0.5 bg-slate-900 border border-slate-600 rounded text-sm text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
            />
            <button onClick={handleSave} className="text-green-400 hover:text-green-300 p-0.5">
              <Check className="h-3.5 w-3.5" />
            </button>
            <button onClick={handleCancel} className="text-slate-400 hover:text-slate-300 p-0.5">
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-1 group/title">
            <div className="text-sm font-medium text-white truncate">{section.title}</div>
            <button
              onClick={(e) => { e.stopPropagation(); setEditTitle(section.title); setEditing(true); }}
              className="text-slate-600 hover:text-slate-300 p-0.5 opacity-0 group-hover/title:opacity-100 transition-opacity"
            >
              <Pencil className="h-3 w-3" />
            </button>
          </div>
        )}
        <div className="text-xs text-slate-500 flex items-center gap-1">
          <Image className="h-3 w-3" />
          {section.photo_count}
          {chapters.length > 0 && (
            <select
              value={section.chapter_id || ''}
              onChange={(e) => { e.stopPropagation(); onMoveToChapter(e.target.value); }}
              onClick={(e) => e.stopPropagation()}
              className="ml-1 bg-transparent border-none text-xs text-slate-500 hover:text-slate-300 cursor-pointer p-0 focus:outline-none"
              title="Move to chapter"
            >
              <option value="">—</option>
              {chapters.map(ch => (
                <option key={ch.id} value={ch.id}>{ch.title}</option>
              ))}
            </select>
          )}
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

function SortableChapter({ chapter, children, onDelete, onRename }: {
  chapter: BookChapter;
  children: React.ReactNode;
  onDelete: () => void;
  onRename: (title: string) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: `chapter-${chapter.id}` });
  const style = { transform: CSS.Transform.toString(transform), transition };
  const [collapsed, setCollapsed] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState(chapter.title);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) inputRef.current?.focus();
  }, [editing]);

  const handleSave = () => {
    const trimmed = editTitle.trim();
    if (trimmed && trimmed !== chapter.title) {
      onRename(trimmed);
    }
    setEditing(false);
  };

  const handleCancel = () => {
    setEditTitle(chapter.title);
    setEditing(false);
  };

  return (
    <div ref={setNodeRef} style={style}>
      <div className="flex items-center gap-1 p-1.5 rounded-md bg-slate-700/50 border border-slate-600">
        <button {...attributes} {...listeners} className="text-slate-500 hover:text-slate-300 cursor-grab">
          <GripVertical className="h-3.5 w-3.5" />
        </button>
        <button onClick={() => setCollapsed(!collapsed)} className="text-slate-400 hover:text-slate-200">
          {collapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
        </button>
        <BookOpen className="h-3.5 w-3.5 text-amber-500/70" />
        {editing ? (
          <div className="flex-1 flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <input
              ref={inputRef}
              type="text"
              value={editTitle}
              onChange={(e) => setEditTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave();
                if (e.key === 'Escape') handleCancel();
              }}
              className="flex-1 min-w-0 px-1.5 py-0.5 bg-slate-900 border border-slate-600 rounded text-xs text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
            />
            <button onClick={handleSave} className="text-green-400 hover:text-green-300 p-0.5">
              <Check className="h-3 w-3" />
            </button>
            <button onClick={handleCancel} className="text-slate-400 hover:text-slate-300 p-0.5">
              <X className="h-3 w-3" />
            </button>
          </div>
        ) : (
          <div className="flex-1 min-w-0 flex items-center gap-1 group/chtitle">
            <span className="text-xs font-semibold text-slate-300 uppercase tracking-wide truncate">{chapter.title}</span>
            <button
              onClick={() => { setEditTitle(chapter.title); setEditing(true); }}
              className="text-slate-600 hover:text-slate-300 p-0.5 opacity-0 group-hover/chtitle:opacity-100 transition-opacity"
            >
              <Pencil className="h-2.5 w-2.5" />
            </button>
          </div>
        )}
        <button
          onClick={onDelete}
          className="text-slate-500 hover:text-red-400 p-0.5"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      </div>
      {!collapsed && <div className="ml-3 mt-1 flex flex-col gap-1">{children}</div>}
    </div>
  );
}

function AddInput({ placeholder, onAdd }: { placeholder: string; onAdd: (title: string) => void }) {
  const [value, setValue] = useState('');
  const handleAdd = () => {
    if (!value.trim()) return;
    onAdd(value.trim());
    setValue('');
  };
  return (
    <div className="flex gap-1">
      <input
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
        placeholder={placeholder}
        className="flex-1 px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white placeholder-slate-500 focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
      />
      <button
        onClick={handleAdd}
        disabled={!value.trim()}
        className="px-2 py-1.5 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white rounded transition-colors"
      >
        <Plus className="h-4 w-4" />
      </button>
    </div>
  );
}

export function SectionSidebar({ bookId, chapters, sections, selectedId, onSelect, onRefresh }: Props) {
  const { t } = useTranslation('pages');

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleAddSection = async (title: string, chapterId?: string) => {
    try {
      const section = await createSection(bookId, title, chapterId);
      onRefresh();
      onSelect(section.id);
    } catch { /* silent */ }
  };

  const handleDeleteSection = async (id: string) => {
    try {
      await deleteSection(id);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleRenameSection = async (id: string, title: string) => {
    try {
      await updateSection(id, { title });
      onRefresh();
    } catch { /* silent */ }
  };

  const handleMoveToChapter = async (sectionId: string, chapterId: string) => {
    try {
      await updateSection(sectionId, { chapter_id: chapterId });
      onRefresh();
    } catch { /* silent */ }
  };

  const handleSectionDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = sections.findIndex(s => s.id === active.id);
    const newIndex = sections.findIndex(s => s.id === over.id);
    const reordered = arrayMove(sections, oldIndex, newIndex);
    try {
      await reorderSections(bookId, reordered.map(s => s.id));
      onRefresh();
    } catch { /* silent */ }
  };

  const handleAddChapter = async (title: string) => {
    try {
      await createChapter(bookId, title);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleDeleteChapter = async (id: string) => {
    if (!confirm(t('books.editor.deleteChapterConfirm'))) return;
    try {
      await deleteChapter(id);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleRenameChapter = async (id: string, title: string) => {
    try {
      await updateChapter(id, title);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleChapterDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const activeId = String(active.id).replace('chapter-', '');
    const overId = String(over.id).replace('chapter-', '');
    const oldIndex = chapters.findIndex(c => c.id === activeId);
    const newIndex = chapters.findIndex(c => c.id === overId);
    const reordered = arrayMove(chapters, oldIndex, newIndex);
    try {
      await reorderChapters(bookId, reordered.map(c => c.id));
      onRefresh();
    } catch { /* silent */ }
  };

  // Group sections by chapter
  const sectionsByChapter = new Map<string, BookSection[]>();
  const orphanSections: BookSection[] = [];
  for (const s of sections) {
    if (s.chapter_id) {
      const list = sectionsByChapter.get(s.chapter_id) || [];
      list.push(s);
      sectionsByChapter.set(s.chapter_id, list);
    } else {
      orphanSections.push(s);
    }
  }

  const hasChapters = chapters.length > 0;

  const renderSectionList = (sectionList: BookSection[]) => (
    <SortableContext items={sectionList.map(s => s.id)} strategy={verticalListSortingStrategy}>
      {sectionList.map((section) => (
        <SortableItem
          key={section.id}
          section={section}
          isSelected={section.id === selectedId}
          onSelect={() => onSelect(section.id)}
          onDelete={() => handleDeleteSection(section.id)}
          onRename={(title) => handleRenameSection(section.id, title)}
          onMoveToChapter={(chapterId) => handleMoveToChapter(section.id, chapterId)}
          chapters={chapters}
        />
      ))}
    </SortableContext>
  );

  return (
    <div className="w-64 shrink-0 flex flex-col gap-2">
      {/* Chapter add input */}
      <AddInput
        placeholder={t('books.editor.chapterTitle')}
        onAdd={handleAddChapter}
      />

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleSectionDragEnd}>
        {hasChapters && (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleChapterDragEnd}>
            <SortableContext items={chapters.map(c => `chapter-${c.id}`)} strategy={verticalListSortingStrategy}>
              {chapters.map((chapter) => {
                const chapterSections = sectionsByChapter.get(chapter.id) || [];
                return (
                  <SortableChapter
                    key={chapter.id}
                    chapter={chapter}
                    onDelete={() => handleDeleteChapter(chapter.id)}
                    onRename={(title) => handleRenameChapter(chapter.id, title)}
                  >
                    {renderSectionList(chapterSections)}
                    <AddInput
                      placeholder={t('books.editor.sectionTitle')}
                      onAdd={(title) => handleAddSection(title, chapter.id)}
                    />
                  </SortableChapter>
                );
              })}
            </SortableContext>
          </DndContext>
        )}

        {/* Orphan sections (no chapter) */}
        {(orphanSections.length > 0 || !hasChapters) && (
          <div>
            {hasChapters && orphanSections.length > 0 && (
              <div className="text-xs text-slate-500 uppercase tracking-wide px-1 py-1 mt-1">
                {t('books.editor.noChapter')}
              </div>
            )}
            {renderSectionList(orphanSections)}
            <div className="mt-1">
              <AddInput
                placeholder={t('books.editor.sectionTitle')}
                onAdd={(title) => handleAddSection(title)}
              />
            </div>
          </div>
        )}
      </DndContext>
    </div>
  );
}
