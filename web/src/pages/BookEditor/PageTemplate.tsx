import { useState, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { PageSlotComponent } from './PageSlot';
import type { BookPage, SectionPhoto, PageFormat, PageStyle } from '../../types';
import { pageFormatSlotCount, getGridClasses, getGridColumnStyle, getSlotClasses, getSlotPhotoUid, getSlotTextContent, getSlotCrop, isMultiColumn, defaultSplitPosition } from '../../utils/pageFormats';

const ALL_PAGE_FORMATS: PageFormat[] = ['4_landscape', '2l_1p', '1p_2l', '2_portrait', '1_fullscreen'];

const formatKeyMap: Record<PageFormat, string> = {
  '4_landscape': '4Landscape',
  '2l_1p': '2l1p',
  '1p_2l': '1p2l',
  '2_portrait': '2Portrait',
  '1_fullscreen': '1Fullscreen',
};

interface Props {
  page: BookPage;
  onClearSlot: (slotIndex: number) => void;
  sectionPhotos?: SectionPhoto[];
  onEditDescription?: (photoUid: string) => void;
  onUpdatePageDescription?: (desc: string) => void;
  onChangeFormat?: (format: PageFormat) => void;
  onChangeStyle?: (style: PageStyle) => void;
  onEditText?: (slotIndex: number) => void;
  onAddText?: (slotIndex: number) => void;
  onEditCrop?: (slotIndex: number) => void;
  onChangeSplitPosition?: (split: number | null) => void;
}

export function PageTemplate({ page, onClearSlot, sectionPhotos, onEditDescription, onUpdatePageDescription, onChangeFormat, onChangeStyle, onEditText, onAddText, onEditCrop, onChangeSplitPosition }: Props) {
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

      {/* Format, style, and split selectors */}
      {(onChangeFormat || onChangeStyle) && (
        <div className="mb-2 flex items-center gap-4 flex-wrap">
          {onChangeFormat && (
            <div className="flex items-center gap-2">
              <label className="text-xs text-slate-400">{t('books.editor.pageFormat')}</label>
              <select
                value={page.format}
                onChange={(e) => onChangeFormat(e.target.value as PageFormat)}
                className="px-2 py-1 bg-slate-900 border border-slate-600 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
              >
                {ALL_PAGE_FORMATS.map(f => (
                  <option key={f} value={f}>{t(`books.editor.format${formatKeyMap[f]}`)}</option>
                ))}
              </select>
            </div>
          )}
          {onChangeStyle && (
            <div className="flex items-center gap-2">
              <label className="text-xs text-slate-400">{t('books.editor.pageStyle')}</label>
              <select
                value={page.style || 'modern'}
                onChange={(e) => onChangeStyle(e.target.value as PageStyle)}
                className="px-2 py-1 bg-slate-900 border border-slate-600 rounded text-sm text-white focus:outline-none focus:ring-1 focus:ring-rose-500"
              >
                <option value="modern">{t('books.editor.styleModern')}</option>
                <option value="archival">{t('books.editor.styleArchival')}</option>
              </select>
            </div>
          )}
          {onChangeSplitPosition && isMultiColumn(page.format) && (
            <div className="flex items-center gap-2">
              <label className="text-xs text-slate-400">{t('books.editor.splitPosition')}</label>
              <input
                type="range"
                min={20}
                max={80}
                value={Math.round((page.split_position ?? defaultSplitPosition(page.format)) * 100)}
                onChange={(e) => onChangeSplitPosition(parseInt(e.target.value) / 100)}
                className="w-24 h-1 accent-rose-500"
              />
              <span className="text-xs text-slate-500 w-8">
                {Math.round((page.split_position ?? defaultSplitPosition(page.format)) * 100)}%
              </span>
              {page.split_position !== null && (
                <button
                  onClick={() => onChangeSplitPosition(null)}
                  className="text-xs text-slate-500 hover:text-white transition-colors"
                >
                  {t('books.editor.resetSplit')}
                </button>
              )}
            </div>
          )}
        </div>
      )}

      <div
        className={`${gridClasses} gap-2 bg-slate-950 border border-slate-700 rounded-lg p-3`}
        style={{ aspectRatio: '297/210', ...getGridColumnStyle(page.format, page.split_position) }}
      >
        {Array.from({ length: slotCount }, (_, i) => {
          const uid = getSlotPhotoUid(page, i);
          const textContent = getSlotTextContent(page, i);
          const { cropX, cropY, cropScale } = getSlotCrop(page, i);
          const sp = uid ? photoLookup.get(uid) : undefined;
          return (
            <PageSlotComponent
              key={i}
              pageId={page.id}
              slotIndex={i}
              photoUid={uid}
              textContent={textContent}
              cropX={cropX}
              cropY={cropY}
              cropScale={cropScale}
              onClear={() => onClearSlot(i)}
              onEditCrop={uid && onEditCrop ? () => onEditCrop(i) : undefined}
              description={sp?.description ?? ''}
              note={sp?.note ?? ''}
              onEditDescription={uid && onEditDescription ? () => onEditDescription(uid) : undefined}
              onEditText={textContent && onEditText ? () => onEditText(i) : undefined}
              onAddText={!uid && !textContent && onAddText ? () => onAddText(i) : undefined}
              className={getSlotClasses(page.format, i)}
            />
          );
        })}
      </div>
    </div>
  );
}
