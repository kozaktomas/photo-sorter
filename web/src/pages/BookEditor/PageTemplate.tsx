import { useState, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { PageSlotComponent } from './PageSlot';
import type { BookPage, PageFormat, SectionPhoto } from '../../types';
import { pageFormatSlotCount } from '../../types';

interface Props {
  page: BookPage;
  onClearSlot: (slotIndex: number) => void;
  sectionPhotos?: SectionPhoto[];
  onEditDescription?: (photoUid: string) => void;
  onUpdatePageDescription?: (desc: string) => void;
}

function getSlotPhotoUid(page: BookPage, slotIndex: number): string {
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  return slot?.photo_uid || '';
}

function getGridClasses(format: PageFormat): string {
  switch (format) {
    case '4_landscape':
      return 'grid grid-cols-2 grid-rows-2';
    case '2l_1p':
      return 'grid grid-cols-[2fr_1fr] grid-rows-2';
    case '1p_2l':
      return 'grid grid-cols-[1fr_2fr] grid-rows-2';
    case '2_portrait':
      return 'grid grid-cols-2 grid-rows-1';
    case '1_fullscreen':
      return 'grid grid-cols-1 grid-rows-1';
  }
}

function getSlotClasses(format: PageFormat, slotIndex: number): string {
  if (format === '2l_1p') {
    // slots 0,1 are landscape stacked vertically on the left
    // slot 2 is portrait on the right (full height)
    if (slotIndex === 2) return 'col-start-2 row-start-1 row-span-2';
    return '';
  }
  if (format === '1p_2l') {
    // slot 0 is portrait on the left (full height)
    // slots 1,2 are landscape stacked vertically on the right
    if (slotIndex === 0) return 'row-span-2';
    return '';
  }
  return '';
}

export function PageTemplate({ page, onClearSlot, sectionPhotos, onEditDescription, onUpdatePageDescription }: Props) {
  const { t } = useTranslation('pages');
  const slotCount = pageFormatSlotCount(page.format);
  const gridClasses = getGridClasses(page.format);

  // Build lookup from photo uid to section photo
  const photoLookup = useMemo(() => {
    const map = new Map<string, SectionPhoto>();
    sectionPhotos?.forEach(sp => map.set(sp.photo_uid, sp));
    return map;
  }, [sectionPhotos]);

  // Inline page description editing
  const [editingPageDesc, setEditingPageDesc] = useState(false);
  const [pageDescText, setPageDescText] = useState(page.description || '');

  const handlePageDescBlur = useCallback(() => {
    setEditingPageDesc(false);
    if (onUpdatePageDescription && pageDescText !== (page.description || '')) {
      onUpdatePageDescription(pageDescText);
    }
  }, [onUpdatePageDescription, pageDescText, page.description]);

  const handleStartEdit = useCallback(() => {
    setPageDescText(page.description || '');
    setEditingPageDesc(true);
  }, [page.description]);

  return (
    <div>
      {/* Page description */}
      {onUpdatePageDescription && (
        <div className="mb-2">
          {editingPageDesc ? (
            <input
              value={pageDescText}
              onChange={(e) => setPageDescText(e.target.value)}
              onBlur={handlePageDescBlur}
              onKeyDown={(e) => { if (e.key === 'Enter') handlePageDescBlur(); if (e.key === 'Escape') setEditingPageDesc(false); }}
              className="w-full px-2 py-1 bg-slate-900 border border-slate-600 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
              placeholder={t('books.editor.pageDescriptionPlaceholder')}
              autoFocus
            />
          ) : (
            <div
              onClick={handleStartEdit}
              className={`text-sm px-2 py-1 rounded cursor-text ${
                page.description ? 'text-slate-300 italic' : 'text-slate-600 italic'
              }`}
            >
              {page.description || t('books.editor.pageDescriptionPlaceholder')}
            </div>
          )}
        </div>
      )}

      <div
        className={`${gridClasses} gap-2 bg-slate-950 border border-slate-700 rounded-lg p-3`}
        style={{ aspectRatio: '4/3' }}
      >
        {Array.from({ length: slotCount }, (_, i) => {
          const uid = getSlotPhotoUid(page, i);
          const sp = uid ? photoLookup.get(uid) : undefined;
          return (
            <PageSlotComponent
              key={i}
              pageId={page.id}
              slotIndex={i}
              photoUid={uid}
              onClear={() => onClearSlot(i)}
              hasDescription={!!sp?.description}
              hasNote={!!sp?.note}
              onEditDescription={uid && onEditDescription ? () => onEditDescription(uid) : undefined}
              className={getSlotClasses(page.format, i)}
            />
          );
        })}
      </div>
    </div>
  );
}
