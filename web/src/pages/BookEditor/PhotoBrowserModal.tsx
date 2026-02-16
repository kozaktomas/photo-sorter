import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Plus, CheckSquare, Square } from 'lucide-react';
import { getPhotos, addSectionPhotos, getThumbnailUrl } from '../../api/client';
import type { Photo } from '../../types';

interface Props {
  sectionId: string;
  existingUids: string[];
  onClose: () => void;
  onAdded: () => void;
}

export function PhotoBrowserModal({ sectionId, existingUids, onClose, onAdded }: Props) {
  const { t } = useTranslation('pages');
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(true);
  const pageSize = 60;

  const existingSet = new Set(existingUids);

  const loadPhotos = useCallback(async (reset: boolean) => {
    setLoading(true);
    try {
      const newOffset = reset ? 0 : offset;
      const data = await getPhotos({
        count: pageSize,
        offset: newOffset,
        q: query || undefined,
        order: 'newest',
      });
      if (reset) {
        setPhotos(data || []);
        setOffset(pageSize);
      } else {
        setPhotos(prev => [...prev, ...(data || [])]);
        setOffset(newOffset + pageSize);
      }
      setHasMore((data || []).length === pageSize);
    } catch { /* silent */ } finally {
      setLoading(false);
    }
  }, [offset, query]);

  useEffect(() => { void loadPhotos(true); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSearch = () => { void loadPhotos(true); };

  const toggleSelect = (uid: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  };

  const handleAdd = async () => {
    if (selected.size === 0) return;
    try {
      await addSectionPhotos(sectionId, Array.from(selected));
      onAdded();
    } catch { /* silent */ }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70">
      <div className="bg-slate-900 border border-slate-700 rounded-lg w-[90vw] max-w-5xl h-[80vh] flex flex-col">
        <div className="flex items-center justify-between p-4 border-b border-slate-700">
          <h2 className="text-lg font-semibold text-white">{t('books.editor.photoBrowser')}</h2>
          <div className="flex items-center gap-3">
            {selected.size > 0 && (
              <button
                onClick={handleAdd}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-rose-600 hover:bg-rose-700 text-white text-sm rounded transition-colors"
              >
                <Plus className="h-4 w-4" />
                {t('books.editor.addSelected')} ({selected.size})
              </button>
            )}
            <button onClick={onClose} className="text-slate-400 hover:text-white">
              <X className="h-5 w-5" />
            </button>
          </div>
        </div>

        <div className="flex gap-2 px-4 py-3">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            placeholder="Search photos..."
            className="flex-1 px-3 py-1.5 bg-slate-800 border border-slate-700 rounded text-sm text-white placeholder-slate-500 focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
          />
          <button
            onClick={handleSearch}
            className="px-3 py-1.5 bg-slate-700 hover:bg-slate-600 text-white text-sm rounded transition-colors"
          >
            Search
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-4 pb-4">
          <div className="grid grid-cols-4 md:grid-cols-6 lg:grid-cols-8 gap-2">
            {photos.map((photo) => {
              const isExisting = existingSet.has(photo.uid);
              const isSelected = selected.has(photo.uid);
              return (
                <div
                  key={photo.uid}
                  className={`relative cursor-pointer rounded overflow-hidden border-2 transition-colors ${
                    isExisting ? 'opacity-40 border-slate-700 cursor-not-allowed' :
                    isSelected ? 'border-rose-500' : 'border-transparent hover:border-slate-500'
                  }`}
                  onClick={() => !isExisting && toggleSelect(photo.uid)}
                >
                  <img
                    src={getThumbnailUrl(photo.uid, 'tile_100')}
                    alt=""
                    className="w-full aspect-square object-cover"
                    loading="lazy"
                  />
                  {!isExisting && (
                    <div className="absolute top-1 left-1">
                      {isSelected ? (
                        <CheckSquare className="h-4 w-4 text-rose-400" />
                      ) : (
                        <Square className="h-4 w-4 text-white/40" />
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          {loading && <div className="text-center py-4 text-slate-500">Loading...</div>}
          {hasMore && !loading && (
            <button
              onClick={() => loadPhotos(false)}
              className="w-full py-2 mt-3 text-sm text-slate-400 hover:text-white bg-slate-800 rounded transition-colors"
            >
              Load more
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
