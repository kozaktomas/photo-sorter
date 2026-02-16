import { useState, useEffect, useCallback, useMemo, useRef, type Dispatch, type SetStateAction, type PointerEvent as ReactPointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { DndContext, DragOverlay, KeyboardSensor, PointerSensor, pointerWithin, useSensor, useSensors, type DragEndEvent, type DragStartEvent, type Modifier } from '@dnd-kit/core';
import { arrayMove, sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import { Type, Heading1, Heading2, Bold, Italic, List, ListOrdered } from 'lucide-react';
import { assignSlot, assignTextSlot, clearSlot, swapSlots, updatePage, updateSlotCrop, reorderPages, getThumbnailUrl } from '../../api/client';
import { MarkdownContent } from '../../utils/markdown';
import { PageSidebar } from './PageSidebar';
import { PageTemplate } from './PageTemplate';
import { UnassignedPool } from './UnassignedPool';
import { PhotoDescriptionDialog } from './PhotoDescriptionDialog';
import type { BookDetail, SectionPhoto, PageFormat, PageStyle } from '../../types';
import { pageFormatSlotCount } from '../../types';
import { getSlotAspectRatio } from '../../utils/pageFormats';

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

// Insert markdown syntax at cursor position (or wrap selection)
function insertMarkdown(textarea: HTMLTextAreaElement, prefix: string, suffix: string, setValue: (v: string) => void, block?: boolean) {
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;
  const val = textarea.value;
  const selected = val.substring(start, end);

  let newText: string;
  let cursorPos: number;

  if (block) {
    // Block-level: insert at line start
    const lineStart = val.lastIndexOf('\n', start - 1) + 1;
    const before = val.substring(0, lineStart);
    const after = val.substring(lineStart);
    newText = before + prefix + after;
    cursorPos = start + prefix.length;
  } else if (selected) {
    newText = val.substring(0, start) + prefix + selected + suffix + val.substring(end);
    cursorPos = start + prefix.length + selected.length + suffix.length;
  } else {
    newText = val.substring(0, start) + prefix + suffix + val.substring(end);
    cursorPos = start + prefix.length;
  }

  setValue(newText);
  // Restore cursor position after React re-render
  requestAnimationFrame(() => {
    textarea.focus();
    textarea.setSelectionRange(cursorPos, cursorPos);
  });
}

// Inline text slot editing dialog with markdown toolbar and preview
function TextSlotDialog({ text, onSave, onClose }: { text: string; onSave: (text: string) => void; onClose: () => void }) {
  const { t } = useTranslation('pages');
  const [value, setValue] = useState(text);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const toolbarButtons = [
    { icon: Heading1, title: 'H1', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '# ', '', setValue, true) },
    { icon: Heading2, title: 'H2', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '## ', '', setValue, true) },
    { icon: Bold, title: 'Bold', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '**', '**', setValue) },
    { icon: Italic, title: 'Italic', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '*', '*', setValue) },
    { icon: List, title: 'UL', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '- ', '', setValue, true) },
    { icon: ListOrdered, title: 'OL', action: () => textareaRef.current && insertMarkdown(textareaRef.current, '1. ', '', setValue, true) },
  ];

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-slate-800 rounded-lg p-6 w-full max-w-3xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">{t('books.editor.textSlotTitle')}</h3>
        <div className="flex gap-1 mb-2">
          {toolbarButtons.map(btn => (
            <button
              key={btn.title}
              onClick={btn.action}
              onPointerDown={e => e.preventDefault()}
              className="p-1.5 rounded hover:bg-slate-700 text-slate-400 hover:text-white transition-colors"
              title={btn.title}
            >
              <btn.icon className="h-4 w-4" />
            </button>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-4">
          <textarea
            ref={textareaRef}
            value={value}
            onChange={e => setValue(e.target.value)}
            className="w-full h-48 px-3 py-2 bg-slate-900 border border-slate-600 rounded text-sm text-white font-mono focus:outline-none focus:ring-1 focus:ring-rose-500 resize-none"
            placeholder={t('books.editor.textPlaceholder')}
            autoFocus
          />
          <div className="h-48 overflow-auto bg-slate-900 border border-slate-600 rounded p-3">
            <p className="text-xs text-slate-500 mb-2">{t('books.editor.markdownPreview')}</p>
            {value.trim() ? (
              <MarkdownContent content={value} />
            ) : (
              <p className="text-slate-600 text-sm italic">{t('books.editor.textPlaceholder')}</p>
            )}
          </div>
        </div>
        <p className="text-xs text-slate-500 mt-2">{t('books.editor.markdownHelp')}</p>
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-slate-300 hover:text-white transition-colors"
          >
            {t('books.editor.closeModal')}
          </button>
          <button
            onClick={() => onSave(value)}
            disabled={!value.trim()}
            className="px-4 py-2 text-sm bg-rose-600 hover:bg-rose-700 text-white rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {t('books.editor.saveButton')}
          </button>
        </div>
      </div>
    </div>
  );
}

// Crop adjustment dialog with visual crop box overlay
function CropDialog({ photoUid, cropX, cropY, cropScale: initialScale, format, slotIndex, splitPosition, onSave, onClose }: {
  photoUid: string; cropX: number; cropY: number; cropScale: number;
  format: PageFormat; slotIndex: number; splitPosition?: number | null;
  onSave: (x: number, y: number, scale: number) => void; onClose: () => void;
}) {
  const { t } = useTranslation('pages');
  const [x, setX] = useState(cropX);
  const [y, setY] = useState(cropY);
  const [scale, setScale] = useState(initialScale);
  const [naturalSize, setNaturalSize] = useState<{ w: number; h: number } | null>(null);
  const [containerSize, setContainerSize] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
  const containerRef = useRef<HTMLDivElement>(null);
  const draggingRef = useRef<{ startX: number; startY: number; startCropX: number; startCropY: number } | null>(null);

  const slotAR = getSlotAspectRatio(format, slotIndex, splitPosition);

  // Measure container with ResizeObserver
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const rect = entries[0].contentRect;
      setContainerSize({ w: rect.width, h: rect.height });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Compute image display rect within the container (object-contain positioning)
  const layout = useMemo(() => {
    if (!naturalSize || containerSize.w === 0 || containerSize.h === 0) return null;
    const photoAR = naturalSize.w / naturalSize.h;
    const containerAR = containerSize.w / containerSize.h;

    let displayW: number, displayH: number, offsetX: number, offsetY: number;
    if (photoAR > containerAR) {
      displayW = containerSize.w;
      displayH = containerSize.w / photoAR;
      offsetX = 0;
      offsetY = (containerSize.h - displayH) / 2;
    } else {
      displayH = containerSize.h;
      displayW = containerSize.h * photoAR;
      offsetX = (containerSize.w - displayW) / 2;
      offsetY = 0;
    }

    // Maximum crop box (scale=1.0): fills one axis completely
    let maxBoxW: number, maxBoxH: number;
    if (photoAR > slotAR) {
      maxBoxH = displayH;
      maxBoxW = displayH * slotAR;
    } else {
      maxBoxW = displayW;
      maxBoxH = displayW / slotAR;
    }

    return { displayW, displayH, offsetX, offsetY, maxBoxW, maxBoxH, photoAR };
  }, [naturalSize, containerSize, slotAR]);

  // Compute crop box position from x,y,scale values
  const cropBoxStyle = useMemo(() => {
    if (!layout) return null;
    const { offsetX, offsetY, displayW, displayH, maxBoxW, maxBoxH } = layout;
    const boxW = maxBoxW * scale;
    const boxH = maxBoxH * scale;
    const overflowX = displayW - boxW;
    const overflowY = displayH - boxH;
    const left = offsetX + x * overflowX;
    const top = offsetY + y * overflowY;
    return { left, top, width: boxW, height: boxH, overflowX, overflowY };
  }, [layout, x, y, scale]);

  const handlePointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    draggingRef.current = { startX: e.clientX, startY: e.clientY, startCropX: x, startCropY: y };
  }, [x, y]);

  const handlePointerMove = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current || !cropBoxStyle) return;
    const { startX, startY, startCropX, startCropY } = draggingRef.current;
    const { overflowX, overflowY } = cropBoxStyle;
    // Use overflow for sensitivity, fall back to a reasonable default
    const sensX = overflowX > 1 ? overflowX : (layout?.displayW ?? 300);
    const sensY = overflowY > 1 ? overflowY : (layout?.displayH ?? 200);

    const dx = e.clientX - startX;
    setX(Math.max(0, Math.min(1, startCropX + dx / sensX)));
    const dy = e.clientY - startY;
    setY(Math.max(0, Math.min(1, startCropY + dy / sensY)));
  }, [cropBoxStyle, layout]);

  const handlePointerUp = useCallback(() => {
    draggingRef.current = null;
  }, []);

  // Mouse wheel to zoom
  const handleWheel = useCallback((e: React.WheelEvent) => {
    e.preventDefault();
    setScale(prev => Math.max(0.1, Math.min(1, prev + (e.deltaY > 0 ? 0.05 : -0.05))));
  }, []);

  // Dimming overlay divs around the crop box
  const dimOverlays = useMemo(() => {
    if (!cropBoxStyle) return null;
    const { left, top, width, height } = cropBoxStyle;
    const cw = containerSize.w;
    const ch = containerSize.h;
    return (
      <>
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top: 0, width: cw, height: top }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top: top + height, width: cw, height: ch - top - height }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: 0, top, width: left, height }} />
        <div className="absolute bg-black/60 pointer-events-none" style={{ left: left + width, top, width: cw - left - width, height }} />
      </>
    );
  }, [cropBoxStyle, containerSize]);

  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-slate-800 rounded-lg p-6 w-full max-w-2xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">{t('books.editor.cropTitle')}</h3>
        <div
          ref={containerRef}
          className="relative w-full rounded overflow-hidden mb-4 bg-slate-900 select-none"
          style={{ aspectRatio: '16 / 10' }}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onWheel={handleWheel}
        >
          <img
            src={getThumbnailUrl(photoUid, 'fit_1920')}
            alt=""
            className="w-full h-full object-contain"
            draggable={false}
            onLoad={e => {
              const img = e.currentTarget;
              setNaturalSize({ w: img.naturalWidth, h: img.naturalHeight });
            }}
          />
          {dimOverlays}
          {cropBoxStyle && (
            <div
              className="absolute border-2 border-white/90 cursor-move"
              style={{
                left: cropBoxStyle.left,
                top: cropBoxStyle.top,
                width: cropBoxStyle.width,
                height: cropBoxStyle.height,
              }}
              onPointerDown={handlePointerDown}
            />
          )}
        </div>
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropHorizontal')}</label>
            <input
              type="range"
              min={0}
              max={100}
              value={Math.round(x * 100)}
              onChange={(e) => setX(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(x * 100)}%</span>
          </div>
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropVertical')}</label>
            <input
              type="range"
              min={0}
              max={100}
              value={Math.round(y * 100)}
              onChange={(e) => setY(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(y * 100)}%</span>
          </div>
          <div className="flex items-center gap-3">
            <label className="text-sm text-slate-400 w-24">{t('books.editor.cropZoom')}</label>
            <input
              type="range"
              min={10}
              max={100}
              value={Math.round(scale * 100)}
              onChange={(e) => setScale(parseInt(e.target.value) / 100)}
              className="flex-1 h-1 accent-rose-500"
            />
            <span className="text-xs text-slate-500 w-8">{Math.round(scale * 100)}%</span>
          </div>
        </div>
        <p className="text-xs text-slate-500 mt-2">{t('books.editor.cropDragHint')}</p>
        <div className="flex justify-between mt-4">
          <button
            onClick={() => { setX(0.5); setY(0.5); setScale(1); }}
            className="px-4 py-2 text-sm text-slate-400 hover:text-white transition-colors"
          >
            {t('books.editor.resetCrop')}
          </button>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm text-slate-300 hover:text-white transition-colors"
            >
              {t('books.editor.closeModal')}
            </button>
            <button
              onClick={() => onSave(x, y, scale)}
              className="px-4 py-2 text-sm bg-rose-600 hover:bg-rose-700 text-white rounded transition-colors"
            >
              {t('books.editor.saveButton')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export function PagesTab({ book, setBook, sectionPhotos, loadSectionPhotos, onRefresh, initialPageId, onPageSelect }: Props) {
  const { t } = useTranslation('pages');
  const defaultPageId = initialPageId && book.pages.find(p => p.id === initialPageId)
    ? initialPageId
    : (book.pages.length > 0 ? book.pages[0].id : null);
  const [selectedId, setSelectedId] = useState<string | null>(defaultPageId);
  const [activePhotoUid, setActivePhotoUid] = useState<string | null>(null);
  const [activeTextContent, setActiveTextContent] = useState<string | null>(null);
  const [isPhotoDrag, setIsPhotoDrag] = useState(false);
  const [editingPhoto, setEditingPhoto] = useState<{ sectionId: string; photoUid: string } | null>(null);
  const [editingTextSlot, setEditingTextSlot] = useState<{ slotIndex: number; text: string } | null>(null);
  const [editingCrop, setEditingCrop] = useState<{ slotIndex: number; photoUid: string; cropX: number; cropY: number; cropScale: number; format: PageFormat; splitPosition?: number | null } | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

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
      setActiveTextContent(null);
      setIsPhotoDrag(true);
    } else if (data?.textContent) {
      setActiveTextContent(data.textContent as string);
      setActivePhotoUid(null);
      setIsPhotoDrag(true);
    } else {
      setIsPhotoDrag(false);
    }
  }, []);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    setActivePhotoUid(null);
    setActiveTextContent(null);
    setIsPhotoDrag(false);
    const { active, over } = event;
    if (!over) return;

    const activeData = active.data.current as Record<string, unknown> | undefined;
    const overData = over.data.current as Record<string, unknown> | undefined;
    if (!activeData || !overData) return;

    // Case 1: Page reorder (both active and over are page-reorder items)
    if (activeData.type === 'page-reorder' && overData.type === 'page-reorder') {
      if (active.id === over.id) return;
      const activePage = book.pages.find(p => p.id === activeData.pageId);
      const overPage = book.pages.find(p => p.id === overData.pageId);
      if (!activePage || !overPage) return;
      // Block cross-section drag
      if (activePage.section_id !== overPage.section_id) return;
      const oldIndex = book.pages.findIndex(p => p.id === activeData.pageId);
      const newIndex = book.pages.findIndex(p => p.id === overData.pageId);
      const reordered = arrayMove(book.pages, oldIndex, newIndex);
      try {
        await reorderPages(book.id, reordered.map(p => p.id));
        onRefresh();
      } catch { /* silent */ }
      return;
    }

    // Case 2 & 3: Photo/text drag operations
    const photoUid = activeData.photoUid as string | undefined;
    const dragTextContent = activeData.textContent as string | undefined;
    if (!photoUid && !dragTextContent) return;

    // Case 2: Photo dropped on a sidebar page
    if (overData.type === 'page-reorder') {
      if (!photoUid) return; // Only photo drags to sidebar pages
      const targetPageId = overData.pageId as string;
      const targetPage = book.pages.find(p => p.id === targetPageId);
      if (!targetPage) return;

      const totalSlots = pageFormatSlotCount(targetPage.format);
      // Check if photo already on target page
      if (targetPage.slots.some(s => s.photo_uid === photoUid)) return;
      // Find first empty slot
      const filledIndices = new Set(targetPage.slots.filter(s => s.photo_uid || s.text_content).map(s => s.slot_index));
      let emptySlotIndex = -1;
      for (let i = 0; i < totalSlots; i++) {
        if (!filledIndices.has(i)) { emptySlotIndex = i; break; }
      }
      if (emptySlotIndex === -1) return; // Page full

      const sourcePageId = activeData.sourcePageId as string | undefined;
      const sourceSlotIndex = activeData.sourceSlotIndex as number | undefined;
      const isFromSlot = sourcePageId !== undefined && sourceSlotIndex !== undefined;

      // Optimistic update
      setBook(prev => {
        if (!prev) return prev;
        const pages = prev.pages.map(p => ({ ...p, slots: p.slots.map(s => ({ ...s })) }));
        if (isFromSlot) {
          const srcPage = pages.find(p => p.id === sourcePageId);
          const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
          if (srcSlot) { srcSlot.photo_uid = ''; srcSlot.text_content = ''; }
        }
        const tgtPage = pages.find(p => p.id === targetPageId);
        if (tgtPage) {
          const tgtSlot = tgtPage.slots.find(s => s.slot_index === emptySlotIndex);
          if (tgtSlot) {
            tgtSlot.photo_uid = photoUid;
            tgtSlot.text_content = '';
          } else {
            tgtPage.slots.push({ slot_index: emptySlotIndex, photo_uid: photoUid, text_content: '', crop_x: 0.5, crop_y: 0.5, crop_scale: 1.0 });
          }
        }
        return { ...prev, pages };
      });

      try {
        if (isFromSlot) {
          await clearSlot(sourcePageId, sourceSlotIndex);
        }
        await assignSlot(targetPageId, emptySlotIndex, photoUid);
        onRefresh();
      } catch {
        onRefresh();
      }
      return;
    }

    // Case 3: Photo/text dropped on a slot (existing logic)
    if (!selectedPage) return;

    const targetPageId = overData.pageId as string;
    const targetSlotIndex = overData.slotIndex as number;
    const targetPhotoUid = overData.photoUid as string | undefined;
    const targetTextContent = overData.textContent as string | undefined;
    const targetHasContent = !!targetPhotoUid || !!targetTextContent;

    // Check if dragging from a slot (has source slot info)
    const sourcePageId = activeData.sourcePageId as string | undefined;
    const sourceSlotIndex = activeData.sourceSlotIndex as number | undefined;
    const isFromSlot = sourcePageId !== undefined && sourceSlotIndex !== undefined;

    // Don't drop on the same slot
    if (isFromSlot && sourcePageId === targetPageId && sourceSlotIndex === targetSlotIndex) return;

    // Optimistic update: swap/move photos/text in local state immediately
    setBook(prev => {
      if (!prev) return prev;
      const pages = prev.pages.map(p => ({ ...p, slots: p.slots.map(s => ({ ...s })) }));
      if (isFromSlot && targetHasContent && sourcePageId === targetPageId) {
        // Same-page swap
        const page = pages.find(p => p.id === sourcePageId);
        if (page) {
          const srcSlot = page.slots.find(s => s.slot_index === sourceSlotIndex);
          const tgtSlot = page.slots.find(s => s.slot_index === targetSlotIndex);
          if (srcSlot && tgtSlot) {
            [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
            [srcSlot.text_content, tgtSlot.text_content] = [tgtSlot.text_content, srcSlot.text_content];
          }
        }
      } else if (isFromSlot && targetHasContent) {
        // Cross-page swap
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          [srcSlot.photo_uid, tgtSlot.photo_uid] = [tgtSlot.photo_uid, srcSlot.photo_uid];
          [srcSlot.text_content, tgtSlot.text_content] = [tgtSlot.text_content, srcSlot.text_content];
        }
      } else if (isFromSlot) {
        // Move to empty slot
        const srcPage = pages.find(p => p.id === sourcePageId);
        const tgtPage = pages.find(p => p.id === targetPageId);
        const srcSlot = srcPage?.slots.find(s => s.slot_index === sourceSlotIndex);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (srcSlot && tgtSlot) {
          tgtSlot.photo_uid = srcSlot.photo_uid;
          tgtSlot.text_content = srcSlot.text_content;
          srcSlot.photo_uid = '';
          srcSlot.text_content = '';
        }
      } else {
        // From unassigned pool (always photo)
        const tgtPage = pages.find(p => p.id === targetPageId);
        const tgtSlot = tgtPage?.slots.find(s => s.slot_index === targetSlotIndex);
        if (tgtSlot && photoUid) tgtSlot.photo_uid = photoUid;
      }
      return { ...prev, pages };
    });

    try {
      if (isFromSlot && targetHasContent && sourcePageId === targetPageId) {
        // Swap: both slots on the same page — atomic swap
        await swapSlots(sourcePageId, sourceSlotIndex, targetSlotIndex);
      } else if (isFromSlot && targetHasContent) {
        // Swap across pages — assign each to the other's slot
        const assignments: Promise<void>[] = [];
        if (photoUid) {
          assignments.push(assignSlot(targetPageId, targetSlotIndex, photoUid));
        } else if (dragTextContent) {
          assignments.push(assignTextSlot(targetPageId, targetSlotIndex, dragTextContent));
        }
        if (targetPhotoUid) {
          assignments.push(assignSlot(sourcePageId, sourceSlotIndex, targetPhotoUid));
        } else if (targetTextContent) {
          assignments.push(assignTextSlot(sourcePageId, sourceSlotIndex, targetTextContent));
        }
        await Promise.all(assignments);
      } else if (isFromSlot) {
        // Move: source slot has content, target is empty — clear old first to avoid unique constraint
        await clearSlot(sourcePageId, sourceSlotIndex);
        if (photoUid) {
          await assignSlot(targetPageId, targetSlotIndex, photoUid);
        } else if (dragTextContent) {
          await assignTextSlot(targetPageId, targetSlotIndex, dragTextContent);
        }
      } else {
        // From unassigned pool — just assign
        if (photoUid) {
          await assignSlot(targetPageId, targetSlotIndex, photoUid);
        }
      }
      onRefresh();
    } catch {
      // Revert on error
      onRefresh();
    }
  }, [book, selectedPage, setBook, onRefresh]);

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

  const handleChangeFormat = useCallback(async (format: PageFormat) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { format });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleChangeStyle = useCallback(async (style: PageStyle) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { style });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

  const handleDescSaved = useCallback(() => {
    if (editingPhoto) {
      loadSectionPhotos(editingPhoto.sectionId);
    }
    setEditingPhoto(null);
  }, [editingPhoto, loadSectionPhotos]);

  const handleAddText = useCallback((slotIndex: number) => {
    setEditingTextSlot({ slotIndex, text: '' });
  }, []);

  const handleEditText = useCallback((slotIndex: number) => {
    if (!selectedPage) return;
    const slot = selectedPage.slots.find(s => s.slot_index === slotIndex);
    setEditingTextSlot({ slotIndex, text: slot?.text_content || '' });
  }, [selectedPage]);

  const handleSaveText = useCallback(async (text: string) => {
    if (!selectedPage || editingTextSlot === null) return;
    try {
      await assignTextSlot(selectedPage.id, editingTextSlot.slotIndex, text);
      onRefresh();
    } catch { /* silent */ }
    setEditingTextSlot(null);
  }, [selectedPage, editingTextSlot, onRefresh]);

  const handleEditCrop = useCallback((slotIndex: number) => {
    if (!selectedPage) return;
    const slot = selectedPage.slots.find(s => s.slot_index === slotIndex);
    if (!slot?.photo_uid) return;
    setEditingCrop({ slotIndex, photoUid: slot.photo_uid, cropX: slot.crop_x ?? 0.5, cropY: slot.crop_y ?? 0.5, cropScale: slot.crop_scale ?? 1.0, format: selectedPage.format, splitPosition: selectedPage.split_position });
  }, [selectedPage]);

  const handleSaveCrop = useCallback(async (cropX: number, cropY: number, cropScale: number) => {
    if (!selectedPage || !editingCrop) return;
    try {
      await updateSlotCrop(selectedPage.id, editingCrop.slotIndex, cropX, cropY, cropScale);
      onRefresh();
    } catch { /* silent */ }
    setEditingCrop(null);
  }, [selectedPage, editingCrop, onRefresh]);

  const handleChangeSplitPosition = useCallback(async (split: number | null) => {
    if (!selectedPage) return;
    try {
      await updatePage(selectedPage.id, { split_position: split });
      onRefresh();
    } catch { /* silent */ }
  }, [selectedPage, onRefresh]);

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
    <DndContext sensors={sensors} collisionDetection={pointerWithin} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
      <div className="flex gap-4">
        <PageSidebar
          bookId={book.id}
          pages={book.pages}
          sections={book.sections}
          selectedId={selectedId}
          onSelect={setSelectedId}
          onRefresh={onRefresh}
          isPhotoDragActive={isPhotoDrag}
        />
        <div className="flex-1 space-y-4">
          {selectedPage && (
            <>
              <PageTemplate
                page={selectedPage}
                onClearSlot={handleClearSlot}
                sectionPhotos={currentSectionPhotos}
                onEditDescription={handleEditDescription}
                onUpdatePageDescription={handleUpdatePageDescription}
                onChangeFormat={handleChangeFormat}
                onChangeStyle={handleChangeStyle}
                onEditText={handleEditText}
                onAddText={handleAddText}
                onEditCrop={handleEditCrop}
                onChangeSplitPosition={handleChangeSplitPosition}
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
            {activeTextContent && (
              <div className="w-16 h-16 rounded shadow-lg overflow-hidden opacity-80 bg-slate-700 flex items-center justify-center">
                <Type className="h-6 w-6 text-slate-300" />
              </div>
            )}
          </DragOverlay>
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

        {editingTextSlot !== null && (
          <TextSlotDialog
            text={editingTextSlot.text}
            onSave={handleSaveText}
            onClose={() => setEditingTextSlot(null)}
          />
        )}

        {editingCrop && (
          <CropDialog
            photoUid={editingCrop.photoUid}
            cropX={editingCrop.cropX}
            cropY={editingCrop.cropY}
            cropScale={editingCrop.cropScale}
            format={editingCrop.format}
            slotIndex={editingCrop.slotIndex}
            splitPosition={editingCrop.splitPosition}
            onSave={handleSaveCrop}
            onClose={() => setEditingCrop(null)}
          />
        )}
      </div>
    </DndContext>
  );
}
