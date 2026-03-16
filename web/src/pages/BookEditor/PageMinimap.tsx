import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { getThumbnailUrl } from '../../api/client';
import { pageFormatSlotCount } from '../../types';
import type { BookDetail, BookPage } from '../../types';
import { Type } from 'lucide-react';

// Mini layout renderers for each page format
// Each card is ~80x56 to match A4 aspect ratio (297/210 ≈ 1.414)

function SlotMini({ photoUid, textContent }: { photoUid: string; textContent: string }) {
  if (photoUid) {
    return (
      <img
        src={getThumbnailUrl(photoUid, 'tile_50')}
        alt=""
        className="w-full h-full object-cover"
      />
    );
  }
  if (textContent) {
    return (
      <div className="w-full h-full bg-slate-700 flex items-center justify-center">
        <Type className="h-2 w-2 text-slate-400" />
      </div>
    );
  }
  return <div className="w-full h-full border border-dashed border-slate-600 rounded-[1px]" />;
}

function getSlotData(page: BookPage, index: number) {
  const slot = page.slots.find(s => s.slot_index === index);
  return { photoUid: slot?.photo_uid ?? '', textContent: slot?.text_content ?? '' };
}

function MiniLayout({ page }: { page: BookPage }) {
  const format = page.format;
  const gap = 'gap-[1px]';

  switch (format) {
    case '4_landscape':
      return (
        <div className={`grid grid-cols-2 grid-rows-2 w-full h-full ${gap}`}>
          {[0, 1, 2, 3].map(i => (
            <div key={i} className="overflow-hidden rounded-[1px]">
              <SlotMini {...getSlotData(page, i)} />
            </div>
          ))}
        </div>
      );
    case '2l_1p':
      return (
        <div className={`grid grid-cols-[2fr_1fr] grid-rows-2 w-full h-full ${gap}`}>
          <div className="overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 0)} /></div>
          <div className="row-span-2 overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 2)} /></div>
          <div className="overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 1)} /></div>
        </div>
      );
    case '1p_2l':
      return (
        <div className={`grid grid-cols-[1fr_2fr] grid-rows-2 w-full h-full ${gap}`}>
          <div className="row-span-2 overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 0)} /></div>
          <div className="overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 1)} /></div>
          <div className="overflow-hidden rounded-[1px]"><SlotMini {...getSlotData(page, 2)} /></div>
        </div>
      );
    case '2_portrait':
      return (
        <div className={`grid grid-cols-2 w-full h-full ${gap}`}>
          {[0, 1].map(i => (
            <div key={i} className="overflow-hidden rounded-[1px]">
              <SlotMini {...getSlotData(page, i)} />
            </div>
          ))}
        </div>
      );
    case '1_fullscreen':
      return (
        <div className="w-full h-full overflow-hidden rounded-[1px]">
          <SlotMini {...getSlotData(page, 0)} />
        </div>
      );
  }
}

function isPageComplete(page: BookPage): boolean {
  const total = pageFormatSlotCount(page.format);
  const filled = page.slots.filter(s => s.photo_uid || s.text_content).length;
  return filled >= total;
}

function isPagePartiallyFilled(page: BookPage): boolean {
  const filled = page.slots.filter(s => s.photo_uid || s.text_content).length;
  return filled > 0 && !isPageComplete(page);
}

interface Props {
  book: BookDetail;
  selectedId: string | null;
  onSelect: (pageId: string) => void;
}

export function PageMinimap({ book, selectedId, onSelect }: Props) {
  const { t } = useTranslation('pages');

  // Group pages by section
  const groupedPages = useMemo(() => {
    const groups: { sectionId: string; sectionTitle: string; pages: BookPage[] }[] = [];
    const sectionMap = new Map(book.sections.map(s => [s.id, s.title]));

    // Gather unique section IDs in page order
    const seenSections = new Set<string>();
    for (const page of book.pages) {
      const sid = page.section_id || '';
      if (!seenSections.has(sid)) {
        seenSections.add(sid);
        groups.push({
          sectionId: sid,
          sectionTitle: sectionMap.get(sid) ?? t('books.editor.noSection'),
          pages: [],
        });
      }
    }

    // Assign pages to groups
    const groupIndex = new Map(groups.map((g, i) => [g.sectionId, i]));
    for (const page of book.pages) {
      const idx = groupIndex.get(page.section_id || '');
      if (idx !== undefined) groups[idx].pages.push(page);
    }

    return groups;
  }, [book.pages, book.sections, t]);

  return (
    <div className="bg-slate-900 border border-slate-700 rounded-lg p-3 max-h-[200px] overflow-y-auto">
      <div className="space-y-2">
        {groupedPages.map(group => (
          <div key={group.sectionId}>
            <div className="text-[10px] text-slate-500 uppercase tracking-wider mb-1 truncate">
              {group.sectionTitle}
            </div>
            <div className="flex flex-wrap gap-2">
              {group.pages.map(page => {
                const isSelected = page.id === selectedId;
                const complete = isPageComplete(page);
                const partial = isPagePartiallyFilled(page);

                return (
                  <button
                    key={page.id}
                    onClick={() => onSelect(page.id)}
                    className={`relative w-[80px] h-[56px] rounded overflow-hidden flex-shrink-0 transition-all p-[2px] ${
                      isSelected
                        ? 'ring-2 ring-rose-500 ring-offset-1 ring-offset-slate-900'
                        : complete
                          ? 'ring-1 ring-emerald-600/50'
                          : 'ring-1 ring-slate-700 hover:ring-slate-500'
                    }`}
                    title={t('books.editor.pageNumber', { number: book.pages.indexOf(page) + 1 })}
                  >
                    <MiniLayout page={page} />
                    {partial && (
                      <div className="absolute top-0.5 right-0.5 w-1.5 h-1.5 rounded-full bg-amber-500" />
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
