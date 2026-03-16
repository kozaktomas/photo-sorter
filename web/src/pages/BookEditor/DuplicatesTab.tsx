import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Loader2 } from 'lucide-react';
import { getThumbnailUrl, removeSectionPhotos } from '../../api/client';
import type { BookDetail, SectionPhoto } from '../../types';

interface DuplicatesTabProps {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
  loadSectionPhotos: (sectionId: string) => Promise<void>;
  onRefresh: () => Promise<void>;
}

interface DuplicateEntry {
  photoUid: string;
  sectionIds: string[];
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

  // Find duplicates: photos appearing in 2+ sections
  const duplicates: DuplicateEntry[] = useMemo(() => {
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
    const result: DuplicateEntry[] = [];
    for (const [photoUid, sectionIds] of photoSections) {
      if (sectionIds.length >= 2) {
        result.push({ photoUid, sectionIds });
      }
    }
    return result;
  }, [sectionPhotos]);

  const handleRemove = useCallback(async (photoUid: string, sectionId: string) => {
    const key = `${photoUid}-${sectionId}`;
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

  const allSectionsLoaded = book.sections.every(s => sectionPhotos[s.id]);

  if (loading || !allSectionsLoaded) {
    return (
      <div className="flex items-center justify-center py-16 text-slate-400">
        <Loader2 className="h-5 w-5 animate-spin mr-2" />
        {t('books.editor.duplicatesLoading')}
      </div>
    );
  }

  if (duplicates.length === 0) {
    return (
      <div className="text-center py-16 text-slate-400">
        {t('books.editor.duplicatesEmpty')}
      </div>
    );
  }

  return (
    <div>
      <p className="text-sm text-slate-400 mb-4">
        {t('books.editor.duplicatesCount', { count: duplicates.length })}
      </p>
      <div className="space-y-3">
        {duplicates.map(({ photoUid, sectionIds }) => (
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
                const key = `${photoUid}-${sectionId}`;
                const isRemoving = removingKey === key;
                return (
                  <div key={sectionId} className="flex items-center gap-2 text-sm">
                    <span className="text-slate-300 truncate">
                      {sectionNames[sectionId] || sectionId}
                    </span>
                    <button
                      onClick={() => void handleRemove(photoUid, sectionId)}
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
  );
}
