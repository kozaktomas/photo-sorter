import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { BookOpen, FileText } from 'lucide-react';
import { getThumbnailUrl } from '../../api/client';
import { PhotoInfoOverlay } from './PhotoInfoOverlay';
import { MarkdownContent } from '../../utils/markdown';
import type { BookDetail, BookPage, SectionPhoto } from '../../types';
import { pageFormatLabelKey, pageFormatSlotCount, getGridClasses, getGridColumnStyle, getSlotClasses, getSlotPhotoUid, getSlotTextContent, getSlotCrop } from '../../utils/pageFormats';

interface Props {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => void;
  initialPageId?: string | null;
}

function PreviewPageSlot({ photoUid, textContent, description, note, cropX, cropY, cropScale, chapterColor, className }: {
  photoUid: string;
  textContent: string;
  description: string;
  note: string;
  cropX?: number;
  cropY?: number;
  cropScale?: number;
  chapterColor?: string;
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
            style={{
              objectPosition: `${(cropX ?? 0.5) * 100}% ${(cropY ?? 0.5) * 100}%`,
              ...(cropScale && cropScale < 1 ? { transform: `scale(${1 / cropScale})`, transformOrigin: `${(cropX ?? 0.5) * 100}% ${(cropY ?? 0.5) * 100}%` } : {}),
            }}
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
      ) : textContent ? (
        <div className="w-full h-full rounded p-4">
          <MarkdownContent content={textContent} chapterColor={chapterColor} />
        </div>
      ) : (
        <div className="w-full h-full bg-slate-800 rounded flex items-center justify-center text-slate-600 text-xs">
          Empty
        </div>
      )}
    </div>
  );
}

function PreviewPageContent({ page, photoInfo, chapterColor, className }: {
  page: BookPage;
  photoInfo: Record<string, { description: string; note: string }>;
  chapterColor?: string;
  className?: string;
}) {
  const slotCount = pageFormatSlotCount(page.format);
  const gridClasses = getGridClasses(page.format);

  return (
    <div
      className={`${gridClasses} gap-2 bg-slate-950 border border-slate-700 rounded-lg p-3 ${className ?? ''}`}
      style={{ aspectRatio: '297/210', ...getGridColumnStyle(page.format, page.split_position) }}
    >
      {Array.from({ length: slotCount }, (_, i) => {
        const uid = getSlotPhotoUid(page, i);
        const textContent = getSlotTextContent(page, i);
        const { cropX, cropY, cropScale } = getSlotCrop(page, i);
        const info = uid ? photoInfo[uid] : undefined;
        return (
          <PreviewPageSlot
            key={i}
            photoUid={uid}
            textContent={textContent}
            description={info?.description ?? ''}
            note={info?.note ?? ''}
            cropX={cropX}
            cropY={cropY}
            cropScale={cropScale}
            chapterColor={chapterColor}
            className={getSlotClasses(page.format, i)}
          />
        );
      })}
    </div>
  );
}

function PreviewPage({ page, pageNumber, sectionTitle, photoInfo, chapterColor }: {
  page: BookPage;
  pageNumber: number;
  sectionTitle?: string;
  photoInfo: Record<string, { description: string; note: string }>;
  chapterColor?: string;
}) {
  const { t } = useTranslation('pages');

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
      <PreviewPageContent page={page} photoInfo={photoInfo} chapterColor={chapterColor} />
    </div>
  );
}

interface Spread {
  left: { page: BookPage; number: number } | null;
  right: { page: BookPage; number: number } | null;
  sectionDivider?: string;
}

function SpreadView({ orderedPages, sectionMap, sectionColorMap, photoInfo }: {
  orderedPages: BookPage[];
  sectionMap: Record<string, string>;
  sectionColorMap: Record<string, string>;
  photoInfo: Record<string, { description: string; note: string }>;
}) {
  const { t } = useTranslation('pages');

  const spreads = useMemo(() => {
    const result: Spread[] = [];
    let lastSectionId = '';

    // First page is always alone on the right (recto)
    for (let i = 0; i < orderedPages.length; ) {
      const page = orderedPages[i];
      const sectionChanged = page.section_id && page.section_id !== lastSectionId;
      const sectionTitle = sectionChanged ? (sectionMap[page.section_id] || 'Untitled Section') : undefined;
      if (page.section_id) lastSectionId = page.section_id;

      if (i === 0) {
        // First page: blank left, page on right
        result.push({
          left: null,
          right: { page, number: 1 },
          sectionDivider: sectionTitle,
        });
        i++;
      } else {
        // Pair pages: [i, i+1]
        const leftPage = page;
        const leftNumber = i + 1;
        const rightPage = orderedPages[i + 1] ?? null;
        const rightNumber = i + 2;

        // Check if right page triggers a section divider
        let rightSectionDivider: string | undefined;
        if (rightPage) {
          const rightChanged = rightPage.section_id && rightPage.section_id !== lastSectionId;
          if (rightChanged) {
            rightSectionDivider = sectionMap[rightPage.section_id] || 'Untitled Section';
          }
          if (rightPage.section_id) lastSectionId = rightPage.section_id;
        }

        result.push({
          left: { page: leftPage, number: leftNumber },
          right: rightPage ? { page: rightPage, number: rightNumber } : null,
          sectionDivider: sectionTitle || rightSectionDivider,
        });

        i += rightPage ? 2 : 1;
      }
    }

    return result;
  }, [orderedPages, sectionMap]);

  return (
    <>
      {spreads.map((spread, si) => (
        <div key={si}>
          {spread.sectionDivider && (
            <div className="flex items-center gap-3 mb-4 mt-6">
              <div className="h-px flex-1 bg-rose-500/30" />
              <span className="text-sm font-medium text-rose-400">
                {spread.sectionDivider}
              </span>
              <div className="h-px flex-1 bg-rose-500/30" />
            </div>
          )}
          <div className="mb-6">
            <div className="flex items-center gap-2 mb-2">
              {spread.left && (
                <>
                  <span className="text-sm font-medium text-slate-400">
                    Page {spread.left.number}
                  </span>
                  <span className="text-xs text-slate-600">{t(pageFormatLabelKey(spread.left.page.format))}</span>
                </>
              )}
              {spread.left && spread.right && (
                <span className="text-slate-600 mx-1">–</span>
              )}
              {spread.right && (
                <>
                  <span className="text-sm font-medium text-slate-400">
                    Page {spread.right.number}
                  </span>
                  <span className="text-xs text-slate-600">{t(pageFormatLabelKey(spread.right.page.format))}</span>
                </>
              )}
            </div>
            <div
              className="flex bg-slate-950 border border-slate-700 rounded-lg overflow-hidden"
              style={{ aspectRatio: `${297 * 2}/210` }}
            >
              {/* Left page (verso) */}
              {spread.left ? (
                <div className="flex-1 pr-0.5" id={`preview-page-${spread.left.page.id}`}>
                  <PreviewPageContent page={spread.left.page} photoInfo={photoInfo} chapterColor={sectionColorMap[spread.left.page.section_id]} className="h-full rounded-none border-0" />
                </div>
              ) : (
                <div className="flex-1 bg-slate-900 flex items-center justify-center text-slate-700 text-sm pr-0.5">
                  {t('books.editor.blankPage')}
                </div>
              )}
              {/* Spine */}
              <div className="w-px bg-slate-600" />
              {/* Right page (recto) */}
              {spread.right ? (
                <div className="flex-1 pl-0.5" id={`preview-page-${spread.right.page.id}`}>
                  <PreviewPageContent page={spread.right.page} photoInfo={photoInfo} chapterColor={sectionColorMap[spread.right.page.section_id]} className="h-full rounded-none border-0" />
                </div>
              ) : (
                <div className="flex-1 bg-slate-900 flex items-center justify-center text-slate-700 text-sm pl-0.5">
                  {t('books.editor.blankPage')}
                </div>
              )}
            </div>
          </div>
        </div>
      ))}
    </>
  );
}

export function PreviewTab({ book, sectionPhotos, loadSectionPhotos, initialPageId }: Props) {
  const { t } = useTranslation('pages');

  const storageKey = `book-spread-${book.id}`;
  const [spreadMode, setSpreadMode] = useState(() => {
    try { return localStorage.getItem(storageKey) === 'true'; } catch { return false; }
  });
  const toggleSpread = useCallback(() => {
    setSpreadMode(prev => {
      const next = !prev;
      try { localStorage.setItem(storageKey, String(next)); } catch { /* ignore */ }
      return next;
    });
  }, [storageKey]);

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

  // Group pages by section for display (with chapter prefix)
  const sectionMap = useMemo(() => {
    const chapterMap: Record<string, string> = {};
    book.chapters.forEach(ch => { chapterMap[ch.id] = ch.title; });
    const map: Record<string, string> = {};
    book.sections.forEach(s => {
      const chapterTitle = s.chapter_id ? chapterMap[s.chapter_id] : undefined;
      map[s.id] = chapterTitle ? `${chapterTitle} | ${s.title}` : s.title;
    });
    return map;
  }, [book.sections, book.chapters]);

  // Map section_id → chapter color for text slot rendering
  const sectionColorMap = useMemo(() => {
    const chapterColorMap: Record<string, string> = {};
    book.chapters.forEach(ch => { if (ch.color) chapterColorMap[ch.id] = ch.color; });
    const map: Record<string, string> = {};
    book.sections.forEach(s => {
      if (s.chapter_id && chapterColorMap[s.chapter_id]) {
        map[s.id] = chapterColorMap[s.chapter_id];
      }
    });
    return map;
  }, [book.sections, book.chapters]);

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

  return (
    <div className="w-[100vw] relative left-[50%] -ml-[50vw] px-4 sm:px-6 lg:px-8">
      <div className="flex justify-end mb-4">
        <button
          onClick={toggleSpread}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-600 transition-colors"
          title={spreadMode ? t('books.editor.singlePageView') : t('books.editor.spreadView')}
        >
          {spreadMode ? (
            <><FileText className="w-4 h-4" />{t('books.editor.singlePageView')}</>
          ) : (
            <><BookOpen className="w-4 h-4" />{t('books.editor.spreadView')}</>
          )}
        </button>
      </div>

      {spreadMode ? (
        <SpreadView orderedPages={orderedPages} sectionMap={sectionMap} sectionColorMap={sectionColorMap} photoInfo={photoInfo} />
      ) : (
        <SinglePageView orderedPages={orderedPages} sectionMap={sectionMap} sectionColorMap={sectionColorMap} photoInfo={photoInfo} />
      )}
    </div>
  );
}

function SinglePageView({ orderedPages, sectionMap, sectionColorMap, photoInfo }: {
  orderedPages: BookPage[];
  sectionMap: Record<string, string>;
  sectionColorMap: Record<string, string>;
  photoInfo: Record<string, { description: string; note: string }>;
}) {
  let lastSectionId = '';

  return (
    <>
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
              chapterColor={sectionColorMap[page.section_id]}
            />
          </div>
        );
      })}
    </>
  );
}
