import { useRef, useEffect, useState, useMemo, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownContent } from '../utils/markdown';
import { getThumbnailUrl } from '../api/client';
import { PAGE_DIMENSIONS } from '../constants/bookTypography';
import { getSlotRects } from '../utils/pageFormats';
import type { PageFormat, PageSlot } from '../types';

const {
  pageWidth: PAGE_WIDTH_MM,
  pageHeight: PAGE_HEIGHT_MM,
  marginInside: MARGIN_INSIDE_MM,
  headerHeight: HEADER_MM,
  footerHeight: FOOTER_MM,
} = PAGE_DIMENSIONS;

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
