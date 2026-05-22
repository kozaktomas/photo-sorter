import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { X, ExternalLink, Loader2 } from 'lucide-react';
import { getPhoto, getThumbnailUrl } from '../../api/client';
import type { Photo } from '../../types';

interface BrowseSidePanelProps {
  photoUIDs: string[];
  onClose: () => void;
}

// MAX_PRELOAD limits how many photo cards we materialise at once. The
// cluster click can yield thousands of UIDs; rendering even a fraction of
// them with thumbnails would flood the network and the DOM. 50 hits a
// reasonable middle ground — the user can scroll within the panel and the
// "View on Photos page" link takes them somewhere designed for paging.
const MAX_PRELOAD = 50;

interface PhotoState {
  uid: string;
  photo?: Photo;
  error?: string;
}

export function BrowseSidePanel({ photoUIDs, onClose }: BrowseSidePanelProps) {
  const { t } = useTranslation(['pages', 'common']);
  const visible = photoUIDs.slice(0, MAX_PRELOAD);
  const overflow = photoUIDs.length - visible.length;

  const [photos, setPhotos] = useState<PhotoState[]>([]);

  useEffect(() => {
    let cancelled = false;
    setPhotos(visible.map(uid => ({ uid })));
    void (async () => {
      // Fan out the photo fetches in small concurrent waves. Stale closures
      // are filtered out via the `cancelled` flag so a quick re-click of a
      // different cluster doesn't paint old photos into the new panel.
      for (let i = 0; i < visible.length; i += 6) {
        const batch = visible.slice(i, i + 6);
        const results = await Promise.allSettled(batch.map(uid => getPhoto(uid)));
        if (cancelled) return;
        setPhotos(prev => {
          const next = [...prev];
          for (let j = 0; j < batch.length; j++) {
            const slotIdx = i + j;
            if (slotIdx >= next.length) continue;
            const r = results[j];
            if (r.status === 'fulfilled') {
              next[slotIdx] = { uid: batch[j], photo: r.value };
            } else {
              next[slotIdx] = { uid: batch[j], error: String(r.reason) };
            }
          }
          return next;
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [photoUIDs.join(',')]);

  return (
    <aside className="bg-slate-800 border-l border-slate-700 flex flex-col h-full w-full md:w-80 lg:w-96 shrink-0 overflow-hidden">
      <header className="flex items-center justify-between px-4 py-3 border-b border-slate-700 shrink-0">
        <div>
          <h2 className="text-sm font-semibold text-white">
            {t('browse.clusterPhotos', { count: photoUIDs.length })}
          </h2>
          {overflow > 0 && (
            <p className="text-xs text-slate-400 mt-0.5">
              {t('browse.matchingPhotos', { count: photoUIDs.length })}
            </p>
          )}
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('browse.closePanel')}
          className="p-1 rounded text-slate-300 hover:text-white hover:bg-slate-700"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-3 space-y-2">
        {photos.map(({ uid, photo, error }) => (
          <Link
            key={uid}
            to={`/photos/${uid}`}
            className="flex items-center gap-3 rounded-md bg-slate-900/60 hover:bg-slate-900 border border-slate-700 p-2 transition-colors"
          >
            <div className="w-16 h-16 shrink-0 bg-slate-800 rounded overflow-hidden flex items-center justify-center">
              {photo ? (
                <img
                  src={getThumbnailUrl(uid, 'tile_224')}
                  alt={photo.title || photo.file_name || uid}
                  className="object-cover w-full h-full"
                  loading="lazy"
                />
              ) : error ? (
                <X className="h-4 w-4 text-red-400" />
              ) : (
                <Loader2 className="h-4 w-4 text-slate-500 animate-spin" />
              )}
            </div>
            <div className="min-w-0 flex-1">
              <div className="text-sm text-white truncate">
                {photo?.title || photo?.file_name || uid}
              </div>
              <div className="text-xs text-slate-400 truncate">
                {photo?.taken_at?.slice(0, 10) || ''}
              </div>
            </div>
            <ExternalLink className="h-4 w-4 text-slate-500 shrink-0" />
          </Link>
        ))}
        {overflow > 0 && (
          <div className="text-xs text-slate-400 text-center pt-2">
            + {overflow.toLocaleString()}…
          </div>
        )}
      </div>
    </aside>
  );
}
