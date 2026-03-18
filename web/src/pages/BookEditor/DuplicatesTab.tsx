import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Loader2 } from 'lucide-react';
import { getThumbnailUrl, removeSectionPhotos, clearSlot } from '../../api/client';
import type { BookDetail, SectionPhoto } from '../../types';

interface DuplicatesTabProps {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => Promise<void>;
  onRefresh: () => Promise<void>;
}

interface SectionDuplicateEntry {
  photoUid: string;
  sectionIds: string[];
}

interface PageSlotOccurrence {
  pageId: string;
  sectionId: string;
  slotIndex: number;
  pageNumber: number;
}

interface PageDuplicateEntry {
  photoUid: string;
  occurrences: PageSlotOccurrence[];
}

export function DuplicatesTab({ book, sectionPhotos, loadSectionPhotos, onRefresh }: DuplicatesTabProps) {
  const { t } = useTranslation('pages');
  const [loading, setLoading] = useState(false);
  const [removingKey, setRemovingKey] = useState<string | null>(null);

  // Load photos for all sections on mount
  useEffect(() => {
    let cancelled = false;
    async function loadAll() {
      setLoading(true);
      await Promise.all(
        book.sections
          .filter(s => !sectionPhotos[s.id])
          .map(s => loadSectionPhotos(s.id))
      );
      if (!cancelled) setLoading(false);
    }
    void loadAll();
    return () => { cancelled = true; };
  }, [book.sections, loadSectionPhotos, sectionPhotos]);

  // Build section name lookup
  const sectionNames = useMemo(() => {
    const map: Record<string, string> = {};
    for (const s of book.sections) {
      map[s.id] = s.title;
    }
    return map;
  }, [book.sections]);

  // Find cross-section duplicates: photos appearing in 2+ sections
  const sectionDuplicates: SectionDuplicateEntry[] = useMemo(() => {
    const photoSections = new Map<string, string[]>();
    for (const [sectionId, photos] of Object.entries(sectionPhotos)) {
      for (const p of photos) {
        const existing = photoSections.get(p.photo_uid);
        if (existing) {
          existing.push(sectionId);
        } else {
          photoSections.set(p.photo_uid, [sectionId]);
        }
      }
    }
    const result: SectionDuplicateEntry[] = [];
    for (const [photoUid, sectionIds] of photoSections) {
      if (sectionIds.length >= 2) {
        result.push({ photoUid, sectionIds });
      }
    }
    return result;
  }, [sectionPhotos]);

  // Find cross-page duplicates: photos assigned to 2+ page slots
  const pageDuplicates: PageDuplicateEntry[] = useMemo(() => {
    const photoSlots = new Map<string, PageSlotOccurrence[]>();
    // Compute global page numbers
    const sortedPages = [...book.pages].sort((a, b) => a.sort_order - b.sort_order);
    for (let i = 0; i < sortedPages.length; i++) {
      const page = sortedPages[i];
      for (const slot of page.slots) {
        if (!slot.photo_uid) continue;
        const occurrence: PageSlotOccurrence = {
          pageId: page.id,
          sectionId: page.section_id,
          slotIndex: slot.slot_index,
          pageNumber: i + 1,
        };
        const existing = photoSlots.get(slot.photo_uid);
        if (existing) {
          existing.push(occurrence);
        } else {
          photoSlots.set(slot.photo_uid, [occurrence]);
        }
      }
    }
    const result: PageDuplicateEntry[] = [];
    for (const [photoUid, occurrences] of photoSlots) {
      if (occurrences.length >= 2) {
        result.push({ photoUid, occurrences });
      }
    }
    return result;
  }, [book.pages]);

  const handleRemoveFromSection = useCallback(async (photoUid: string, sectionId: string) => {
    const key = `section-${photoUid}-${sectionId}`;
    setRemovingKey(key);
    try {
      await removeSectionPhotos(sectionId, [photoUid]);
      await loadSectionPhotos(sectionId);
      void onRefresh();
    } catch (e) {
      console.error('Failed to remove photo from section:', e);
    }
    setRemovingKey(null);
  }, [loadSectionPhotos, onRefresh]);

  const handleRemoveFromSlot = useCallback(async (pageId: string, slotIndex: number) => {
    const key = `page-${pageId}-${slotIndex}`;
    setRemovingKey(key);
    try {
      await clearSlot(pageId, slotIndex);
      void onRefresh();
    } catch (e) {
      console.error('Failed to clear page slot:', e);
    }
    setRemovingKey(null);
  }, [onRefresh]);

  const allSectionsLoaded = book.sections.every(s => sectionPhotos[s.id]);

  if (loading || !allSectionsLoaded) {
    return (
      <div className="flex items-center justify-center py-16 text-slate-400">
        <Loader2 className="h-5 w-5 animate-spin mr-2" />
        {t('books.editor.duplicatesLoading')}
      </div>
    );
  }

  const hasSectionDupes = sectionDuplicates.length > 0;
  const hasPageDupes = pageDuplicates.length > 0;

  if (!hasSectionDupes && !hasPageDupes) {
    return (
      <div className="text-center py-16 text-slate-400">
        {t('books.editor.duplicatesEmpty')}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {hasSectionDupes && (
        <div>
          <h3 className="text-sm font-medium text-slate-400 mb-3">
            {t('books.editor.duplicatesSectionGroup')}
          </h3>
          <p className="text-sm text-slate-400 mb-4">
            {t('books.editor.duplicatesCount', { count: sectionDuplicates.length })}
          </p>
          <div className="space-y-3">
            {sectionDuplicates.map(({ photoUid, sectionIds }) => (
              <div
                key={photoUid}
                className="flex gap-4 bg-slate-800 border border-slate-700 rounded-lg p-3"
              >
                <img
                  src={getThumbnailUrl(photoUid, 'tile_100')}
                  alt=""
                  className="w-20 h-20 object-cover rounded flex-shrink-0"
                />
                <div className="flex-1 min-w-0 space-y-1.5">
                  {sectionIds.map(sectionId => {
                    const key = `section-${photoUid}-${sectionId}`;
                    const isRemoving = removingKey === key;
                    return (
                      <div key={sectionId} className="flex items-center gap-2 text-sm">
                        <span className="text-slate-300 truncate">
                          {sectionNames[sectionId] || sectionId}
                        </span>
                        <button
                          onClick={() => void handleRemoveFromSection(photoUid, sectionId)}
                          disabled={isRemoving}
                          className="text-slate-500 hover:text-red-400 transition-colors flex-shrink-0 disabled:opacity-40"
                          title={t('books.editor.duplicatesRemove')}
                        >
                          {isRemoving ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <X className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {hasPageDupes && (
        <div>
          <h3 className="text-sm font-medium text-slate-400 mb-3">
            {t('books.editor.duplicatesPageGroup')}
          </h3>
          <div className="space-y-3">
            {pageDuplicates.map(({ photoUid, occurrences }) => (
              <div
                key={photoUid}
                className="flex gap-4 bg-slate-800 border border-slate-700 rounded-lg p-3"
              >
                <img
                  src={getThumbnailUrl(photoUid, 'tile_100')}
                  alt=""
                  className="w-20 h-20 object-cover rounded flex-shrink-0"
                />
                <div className="flex-1 min-w-0 space-y-1.5">
                  {occurrences.map(occ => {
                    const key = `page-${occ.pageId}-${occ.slotIndex}`;
                    const isRemoving = removingKey === key;
                    return (
                      <div key={key} className="flex items-center gap-2 text-sm">
                        <span className="text-slate-300 truncate">
                          {t('books.editor.duplicatesPageLocation', {
                            section: sectionNames[occ.sectionId] || occ.sectionId,
                            page: occ.pageNumber,
                            slot: occ.slotIndex + 1,
                          })}
                        </span>
                        <button
                          onClick={() => void handleRemoveFromSlot(occ.pageId, occ.slotIndex)}
                          disabled={isRemoving}
                          className="text-slate-500 hover:text-red-400 transition-colors flex-shrink-0 disabled:opacity-40"
                          title={t('books.editor.duplicatesRemoveSlot')}
                        >
                          {isRemoving ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <X className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
