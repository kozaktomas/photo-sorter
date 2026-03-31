import { useRef, useEffect, useState, useMemo, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownContent } from '../utils/markdown';
import { getThumbnailUrl } from '../api/client';
import type { PageFormat, PageSlot } from '../types';

// Physical page dimensions in mm (A4 landscape-ish book page)
const PAGE_WIDTH_MM = 297;
const PAGE_HEIGHT_MM = 210;
const MARGIN_INSIDE_MM = 20;
const MARGIN_OUTSIDE_MM = 12;
const HEADER_MM = 4;
const FOOTER_MM = 8;
const CANVAS_HEIGHT_MM = 172;
const CONTENT_WIDTH_MM = PAGE_WIDTH_MM - MARGIN_INSIDE_MM - MARGIN_OUTSIDE_MM; // 265
const COLUMN_GUTTER_MM = 4;
const ROW_GAP_MM = 4;

const HALF_CANVAS = (CANVAS_HEIGHT_MM - ROW_GAP_MM) / 2;

interface SlotRect {
  x: number; // mm from content left
  y: number; // mm from canvas top
  w: number; // mm
  h: number; // mm
}

function getSlotRects(format: PageFormat, splitPosition: number | null): SlotRect[] {
  const availW = CONTENT_WIDTH_MM - COLUMN_GUTTER_MM;
  const sp = splitPosition ?? (format === '2l_1p' ? 2 / 3 : format === '1p_2l' ? 1 / 3 : 0.5);
  const leftW = availW * sp;
  const rightW = availW * (1 - sp);
  const rightX = leftW + COLUMN_GUTTER_MM;

  switch (format) {
    case '1_fullscreen':
      return [{ x: 0, y: 0, w: CONTENT_WIDTH_MM, h: CANVAS_HEIGHT_MM }];
    case '2_portrait':
      return [
        { x: 0, y: 0, w: leftW, h: CANVAS_HEIGHT_MM },
        { x: rightX, y: 0, w: rightW, h: CANVAS_HEIGHT_MM },
      ];
    case '4_landscape':
      return [
        { x: 0, y: 0, w: leftW, h: HALF_CANVAS },
        { x: rightX, y: 0, w: rightW, h: HALF_CANVAS },
        { x: 0, y: HALF_CANVAS + ROW_GAP_MM, w: leftW, h: HALF_CANVAS },
        { x: rightX, y: HALF_CANVAS + ROW_GAP_MM, w: rightW, h: HALF_CANVAS },
      ];
    case '2l_1p':
      return [
        { x: 0, y: 0, w: leftW, h: HALF_CANVAS },
        { x: 0, y: HALF_CANVAS + ROW_GAP_MM, w: leftW, h: HALF_CANVAS },
        { x: rightX, y: 0, w: rightW, h: CANVAS_HEIGHT_MM },
      ];
    case '1p_2l':
      return [
        { x: 0, y: 0, w: leftW, h: CANVAS_HEIGHT_MM },
        { x: rightX, y: 0, w: rightW, h: HALF_CANVAS },
        { x: rightX, y: HALF_CANVAS + ROW_GAP_MM, w: rightW, h: HALF_CANVAS },
      ];
  }
}

interface PageLayoutPreviewProps {
  format: PageFormat;
  activeSlotIndex: number;
  slots: PageSlot[];
  splitPosition: number | null;
  liveText: string;
  previewWidth?: number;
}

export function PageLayoutPreview({
  format,
  activeSlotIndex,
  slots,
  splitPosition,
  liveText,
  previewWidth = 300,
}: PageLayoutPreviewProps) {
  const { t } = useTranslation('pages');
  const textSlotRef = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);

  const scale = previewWidth / PAGE_WIDTH_MM;
  const pageHeight = PAGE_HEIGHT_MM * scale;
  const canvasLeft = MARGIN_INSIDE_MM * scale;
  const canvasTop = HEADER_MM * scale;

  const slotRects = useMemo(() => getSlotRects(format, splitPosition), [format, splitPosition]);

  // Detect overflow
  useEffect(() => {
    const el = textSlotRef.current;
    if (!el) { setOverflow(false); return; }
    setOverflow(el.scrollHeight > el.clientHeight + 1);
  }, [liveText, format, splitPosition]);

  return (
    <div>
      <p className="text-xs text-slate-500 mb-2">{t('books.editor.pagePreview')}</p>
      <div
        className="relative bg-white rounded shadow-md mx-auto"
        style={{ width: previewWidth, height: pageHeight }}
      >
        {/* Canvas area */}
        {slotRects.map((rect, idx) => {
          const isActive = idx === activeSlotIndex;
          const slot = slots.find(s => s.slot_index === idx);
          const photoUid = slot?.photo_uid ?? '';
          const textContent = slot?.text_content ?? '';

          const left = canvasLeft + rect.x * scale;
          const top = canvasTop + rect.y * scale;
          const width = rect.w * scale;
          const height = rect.h * scale;
          const textFontSize = `${Math.max(4, 7 * scale)}px`;

          let content: ReactNode;
          if (isActive && liveText.trim()) {
            content = (
              <div
                className="w-full h-full p-[2px] font-serif text-black overflow-hidden"
                style={{ fontSize: textFontSize, lineHeight: 1.3 }}
              >
                <MarkdownContent content={liveText} className="page-preview-text" />
              </div>
            );
          } else if (photoUid && !isActive) {
            content = (
              <img
                src={getThumbnailUrl(photoUid, 'tile_224')}
                alt=""
                className="w-full h-full object-cover"
                draggable={false}
              />
            );
          } else if (textContent.trim() && !isActive) {
            content = (
              <div
                className="w-full h-full p-[2px] font-serif text-slate-400 overflow-hidden"
                style={{ fontSize: textFontSize, lineHeight: 1.3 }}
              >
                <MarkdownContent content={textContent} className="page-preview-text" />
              </div>
            );
          } else {
            content = <div className="w-full h-full bg-slate-100" />;
          }

          return (
            <div
              key={idx}
              ref={isActive ? textSlotRef : undefined}
              className={`absolute overflow-hidden ${
                isActive
                  ? overflow
                    ? 'border-2 border-red-500 ring-1 ring-red-500/50'
                    : 'border-2 border-rose-400 ring-1 ring-rose-400/30'
                  : 'border border-slate-200'
              }`}
              style={{ left, top, width, height }}
            >
              {content}
            </div>
          );
        })}

        {/* Header zone indicator */}
        <div
          className="absolute left-0 right-0 bg-slate-50"
          style={{ top: 0, height: HEADER_MM * scale }}
        />
        {/* Footer zone indicator */}
        <div
          className="absolute left-0 right-0 bg-slate-50"
          style={{ bottom: 0, height: FOOTER_MM * scale }}
        />
      </div>

      {overflow && (
        <p className="text-xs text-red-400 mt-1.5 text-center">
          {t('books.editor.textOverflow')}
        </p>
      )}
    </div>
  );
}
