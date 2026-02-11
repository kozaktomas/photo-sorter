import { useState, useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { getThumbnailUrl } from '../../api/client';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import type { BookDetail, BookPage, SectionPhoto } from '../../types';
import { pageFormatLabelKey, pageFormatSlotCount, getGridClasses, getSlotClasses, getSlotPhotoUid } from '../../utils/pageFormats';

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  initialPageId?: string | null;
}

function PreviewPageSlot({ photoUid, description, note, className }: {
  photoUid: string;
  description: string;
  note: string;
  className?: string;
}) {
  const [orientation, setOrientation] = useState<'L' | 'P' | null>(null);

  return (
    <div className={`relative ${className ?? ''}`}>
      {photoUid ? (
        <div className="relative w-full h-full">
          <img
            src={getThumbnailUrl(photoUid, 'fit_720')}
            alt=""
            className="w-full h-full object-cover rounded"
            onLoad={(e) => {
              const img = e.currentTarget;
              setOrientation(img.naturalWidth >= img.naturalHeight ? 'L' : 'P');
            }}
          />
          <PhotoInfoOverlay
            description={description}
            note={note}
            orientation={orientation}
          />
        </div>
      ) : (
        <div className="w-full h-full bg-slate-800 rounded flex items-center justify-center text-slate-600 text-xs">
          Empty
        </div>
      )}
    </div>
  );
}

function PreviewPage({ page, pageNumber, sectionTitle, photoInfo }: {
  page: BookPage;
  pageNumber: number;
  sectionTitle?: string;
  photoInfo: Record<string, { description: string; note: string }>;
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
        style={{ aspectRatio: '4/3' }}
      >
        {Array.from({ length: slotCount }, (_, i) => {
          const uid = getSlotPhotoUid(page, i);
          const info = uid ? photoInfo[uid] : undefined;
          return (
            <PreviewPageSlot
              key={i}
              photoUid={uid}
              description={info?.description ?? ''}
              note={info?.note ?? ''}
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

  // Build photo info lookup from all section photos
  const photoInfo = useMemo(() => {
    const map: Record<string, { description: string; note: string }> = {};
    Object.values(sectionPhotos).forEach(photos => {
      photos.forEach(p => {
        if (p.description || p.note) {
          map[p.photo_uid] = {
            description: p.description || '',
            note: p.note || '',
          };
        }
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

  // Order pages section-by-section for consistent numbering (1..n)
  const orderedPages = useMemo(() => {
    const sectionOrder = new Map(book.sections.map((s, i) => [s.id, i]));
    return [...book.pages].sort((a, b) => {
      const sa = sectionOrder.get(a.section_id) ?? 9999;
      const sb = sectionOrder.get(b.section_id) ?? 9999;
      if (sa !== sb) return sa - sb;
      return a.sort_order - b.sort_order;
    });
  }, [book.pages, book.sections]);

  if (orderedPages.length === 0) {
    return (
      <div className="text-center text-slate-500 py-12">
        {t('books.editor.previewEmpty')}
      </div>
    );
  }

  // Track current section to show dividers
  let lastSectionId = '';

  return (
    <div className="w-[100vw] relative left-[50%] -ml-[50vw] px-4 sm:px-6 lg:px-8">
      {orderedPages.map((page, i) => {
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
              photoInfo={photoInfo}
            />
          </div>
        );
      })}
    </div>
  );
}
