import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { getThumbnailUrl } from '../../api/client';
import type { BookDetail, BookPage, SectionPhoto, PageFormat } from '../../types';
import { pageFormatLabelKey, pageFormatSlotCount } from '../../types';

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  initialPageId?: string | null;
}

function getSlotPhotoUid(page: BookPage, slotIndex: number): string {
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  return slot?.photo_uid || '';
}

function PreviewPageSlot({ photoUid, description, className }: {
  photoUid: string;
  description: string;
  className?: string;
}) {
  return (
    <div className={`relative ${className || ''}`}>
      {photoUid ? (
        <div className="w-full h-full">
          <img
            src={getThumbnailUrl(photoUid, 'fit_720')}
            alt=""
            className="w-full h-full object-cover rounded"
          />
          {description && (
            <div className="absolute bottom-0 left-0 right-0 bg-black/60 text-white text-xs px-2 py-1 rounded-b">
              {description}
            </div>
          )}
        </div>
      ) : (
        <div className="w-full h-full bg-slate-800 rounded flex items-center justify-center text-slate-600 text-xs">
          Empty
        </div>
      )}
    </div>
  );
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
  if (format === '2l_1p' && slotIndex === 2) return 'col-start-2 row-start-1 row-span-2';
  if (format === '1p_2l' && slotIndex === 0) return 'row-span-2';
  return '';
}

function PreviewPage({ page, pageNumber, sectionTitle, descriptions }: {
  page: BookPage;
  pageNumber: number;
  sectionTitle?: string;
  descriptions: Record<string, string>;
}) {
  const { t } = useTranslation('pages');
  const slotCount = pageFormatSlotCount(page.format);
  const gridClasses = getGridClasses(page.format);

  return (
    <div className="mb-6">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-sm font-medium text-slate-400">
          Page {pageNumber}
        </span>
        <span className="text-xs text-slate-600">{t(pageFormatLabelKey(page.format))}</span>
        {sectionTitle && (
          <span className="text-xs text-rose-400/60">{sectionTitle}</span>
        )}
      </div>
      {page.description && (
        <p className="text-sm italic text-slate-300 mb-2">{page.description}</p>
      )}
      <div
        className={`${gridClasses} gap-2 bg-slate-950 border border-slate-700 rounded-lg p-3`}
        style={{ aspectRatio: '4/3', maxWidth: '700px' }}
      >
        {Array.from({ length: slotCount }, (_, i) => {
          const uid = getSlotPhotoUid(page, i);
          return (
            <PreviewPageSlot
              key={i}
              photoUid={uid}
              description={uid ? descriptions[uid] || '' : ''}
              className={getSlotClasses(page.format, i)}
            />
          );
        })}
      </div>
    </div>
  );
}

export function PreviewTab({ book, sectionPhotos, loadSectionPhotos, initialPageId }: Props) {
  const { t } = useTranslation('pages');

  // Load all section photos
  useEffect(() => {
    book.sections.forEach(s => {
      if (!sectionPhotos[s.id]) loadSectionPhotos(s.id);
    });
  }, [book.sections, sectionPhotos, loadSectionPhotos]);

  // Scroll to the target page on mount
  useEffect(() => {
    if (initialPageId) {
      const el = document.getElementById(`preview-page-${initialPageId}`);
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    }
  }, [initialPageId]);

  // Build description lookup from all section photos
  const descriptions = useMemo(() => {
    const map: Record<string, string> = {};
    Object.values(sectionPhotos).forEach(photos => {
      photos.forEach(p => {
        if (p.description) map[p.photo_uid] = p.description;
      });
    });
    return map;
  }, [sectionPhotos]);

  // Group pages by section for display
  const sectionMap = useMemo(() => {
    const map: Record<string, string> = {};
    book.sections.forEach(s => { map[s.id] = s.title; });
    return map;
  }, [book.sections]);

  if (book.pages.length === 0) {
    return (
      <div className="text-center text-slate-500 py-12">
        {t('books.editor.previewEmpty')}
      </div>
    );
  }

  // Track current section to show dividers
  let lastSectionId = '';

  return (
    <div>
      {book.pages.map((page, i) => {
        const showDivider = page.section_id && page.section_id !== lastSectionId;
        if (page.section_id) lastSectionId = page.section_id;

        return (
          <div key={page.id} id={`preview-page-${page.id}`}>
            {showDivider && (
              <div className="flex items-center gap-3 mb-4 mt-6">
                <div className="h-px flex-1 bg-rose-500/30" />
                <span className="text-sm font-medium text-rose-400">
                  {sectionMap[page.section_id] || 'Untitled Section'}
                </span>
                <div className="h-px flex-1 bg-rose-500/30" />
              </div>
            )}
            <PreviewPage
              page={page}
              pageNumber={i + 1}
              sectionTitle={sectionMap[page.section_id]}
              descriptions={descriptions}
            />
          </div>
        );
      })}
    </div>
  );
}
