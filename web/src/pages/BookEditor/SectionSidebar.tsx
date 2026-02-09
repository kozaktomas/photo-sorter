import { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { GripVertical, Plus, Trash2, Image, Pencil, Check, X } from 'lucide-react';
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
import { createSection, deleteSection, reorderSections, updateSection } from '../../api/client';
import type { BookSection } from '../../types';

interface Props {
  bookId: string;
  sections: BookSection[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  onRefresh: () => void;
}

function SortableItem({ section, isSelected, onSelect, onDelete, onRename }: {
  section: BookSection;
  isSelected: boolean;
  onSelect: () => void;
  onDelete: () => void;
  onRename: (title: string) => void;
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
              className="flex-1 min-w-0 px-1.5 py-0.5 bg-slate-900 border border-slate-600 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
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

export function SectionSidebar({ bookId, sections, selectedId, onSelect, onRefresh }: Props) {
  const { t } = useTranslation('pages');
  const [newTitle, setNewTitle] = useState('');

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleAdd = async () => {
    if (!newTitle.trim()) return;
    try {
      const section = await createSection(bookId, newTitle.trim());
      setNewTitle('');
      await onRefresh();
      onSelect(section.id);
    } catch { /* silent */ }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteSection(id);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleRename = async (id: string, title: string) => {
    try {
      await updateSection(id, title);
      onRefresh();
    } catch { /* silent */ }
  };

  const handleDragEnd = async (event: DragEndEvent) => {
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

  return (
    <div className="w-64 shrink-0 flex flex-col gap-2">
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={sections.map(s => s.id)} strategy={verticalListSortingStrategy}>
          {sections.map((section) => (
            <SortableItem
              key={section.id}
              section={section}
              isSelected={section.id === selectedId}
              onSelect={() => onSelect(section.id)}
              onDelete={() => handleDelete(section.id)}
              onRename={(title) => handleRename(section.id, title)}
            />
          ))}
        </SortableContext>
      </DndContext>

      <div className="flex gap-1 mt-2">
        <input
          type="text"
          value={newTitle}
          onChange={(e) => setNewTitle(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleAdd()}
          placeholder={t('books.editor.sectionTitle')}
          className="flex-1 px-2 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-rose-500"
        />
        <button
          onClick={handleAdd}
          disabled={!newTitle.trim()}
          className="px-2 py-1.5 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white rounded transition-colors"
        >
          <Plus className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
