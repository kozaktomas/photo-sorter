import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Camera, CheckCircle, AlertCircle } from 'lucide-react';
import { FormSelect } from '../../components/FormSelect';
import { getAlbumPhotos, getAlbums, getThumbnailUrl, uploadPhotos } from '../../api/client';
import type { Album } from '../../types';

const LS_KEY = 'capture_default_album';
const MAX_RECENT = 3;

type Toast = { kind: 'success' | 'error'; message: string } | null;

export function CapturePage() {
  const { t } = useTranslation(['pages']);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [albums, setAlbums] = useState<Album[]>([]);
  const [selectedAlbum, setSelectedAlbum] = useState<string>('');
  const [recent, setRecent] = useState<string[]>([]);
  const [uploading, setUploading] = useState(false);
  const [toast, setToast] = useState<Toast>(null);

  // Load albums on mount + restore last selection from localStorage.
  useEffect(() => {
    let cancelled = false;
    void getAlbums({ count: 1000, order: 'name' }).then(list => {
      if (cancelled) return;
      setAlbums(list);
      const stored = localStorage.getItem(LS_KEY) ?? '';
      // Only use the stored album if it still exists in the response.
      if (stored && list.some(a => a.uid === stored)) {
        setSelectedAlbum(stored);
      } else if (list[0]) {
        setSelectedAlbum(list[0].uid);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  // Persist album choice whenever it changes (only if non-empty).
  useEffect(() => {
    if (selectedAlbum) {
      localStorage.setItem(LS_KEY, selectedAlbum);
    }
  }, [selectedAlbum]);

  // Auto-dismiss toast after a short delay.
  useEffect(() => {
    if (!toast) return;
    const id = window.setTimeout(() => setToast(null), 2500);
    return () => window.clearTimeout(id);
  }, [toast]);

  const handleShoot = useCallback(() => {
    if (!selectedAlbum || uploading) return;
    fileInputRef.current?.click();
  }, [selectedAlbum, uploading]);

  const handleFileChange = useCallback(async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    // Allow choosing the same file twice in a row by resetting the input.
    e.target.value = '';
    if (!selectedAlbum) return;

    setUploading(true);
    setToast(null);
    try {
      const result = await uploadPhotos(selectedAlbum, files);
      // The /api/v1/upload endpoint reports uploaded count but not photo UIDs.
      // For the recent strip we fall back to refreshing the album thumbs the
      // next time the page mounts; in this single shot view we use the
      // returned count as a sanity check.
      if (result.uploaded > 0) {
        // The upload endpoint does not echo new UIDs, so we fetch the most
        // recent album photos as a best-effort recent strip. Cheap: capped at
        // MAX_RECENT entries.
        try {
          const photos = await getAlbumPhotos(selectedAlbum, { count: MAX_RECENT });
          setRecent(photos.map(p => p.uid));
        } catch {
          // Recent strip is best-effort — never block the success toast.
        }
        setToast({ kind: 'success', message: t('pages:capture.uploaded') });
      } else {
        setToast({ kind: 'error', message: t('pages:capture.upload_failed') });
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('pages:capture.upload_failed');
      setToast({ kind: 'error', message: msg });
    } finally {
      setUploading(false);
    }
  }, [selectedAlbum, t]);

  return (
    <div className="min-h-[calc(100vh-8rem)] flex flex-col items-center justify-start gap-6 max-w-md mx-auto w-full">
      <h1 className="text-xl font-semibold text-white flex items-center gap-2 self-start">
        <Camera className="h-5 w-5 text-emerald-400" />
        {t('pages:capture.title')}
      </h1>

      <div className="w-full">
        <FormSelect
          label={t('pages:capture.select_album')}
          value={selectedAlbum}
          onChange={e => setSelectedAlbum(e.target.value)}
          disabled={uploading || albums.length === 0}
        >
          {albums.length === 0 && <option value="">…</option>}
          {albums.map(a => (
            <option key={a.uid} value={a.uid}>{a.title}</option>
          ))}
        </FormSelect>
      </div>

      <button
        type="button"
        onClick={handleShoot}
        disabled={!selectedAlbum || uploading}
        aria-label={t('pages:capture.shoot')}
        className="mt-4 h-40 w-40 rounded-full bg-emerald-600 text-white flex items-center justify-center shadow-2xl ring-4 ring-emerald-500/30 active:scale-95 disabled:opacity-50 disabled:active:scale-100 transition-transform focus:outline-none focus-visible:ring-4 focus-visible:ring-emerald-300"
      >
        {uploading ? (
          <span className="h-10 w-10 border-4 border-white/40 border-t-white rounded-full animate-spin" />
        ) : (
          <Camera className="h-16 w-16" />
        )}
      </button>

      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        capture="environment"
        className="hidden"
        onChange={(e) => { void handleFileChange(e); }}
      />

      {toast && (
        <div
          role="status"
          aria-live="polite"
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium ${
            toast.kind === 'success'
              ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30'
              : 'bg-red-500/15 text-red-300 border border-red-500/30'
          }`}
        >
          {toast.kind === 'success' ? (
            <CheckCircle className="h-4 w-4" />
          ) : (
            <AlertCircle className="h-4 w-4" />
          )}
          {toast.message}
        </div>
      )}

      {recent.length > 0 && (
        <div className="w-full mt-4">
          <h2 className="text-xs uppercase tracking-wide text-slate-400 mb-2">
            {t('pages:capture.recent')}
          </h2>
          <div className="grid grid-cols-3 gap-2">
            {recent.slice(0, MAX_RECENT).map(uid => (
              <a
                key={uid}
                href={`/photos/${uid}`}
                className="aspect-square rounded-lg overflow-hidden bg-slate-700 hover:ring-2 hover:ring-emerald-400 transition-all"
              >
                <img
                  src={getThumbnailUrl(uid, 'fit_720')}
                  alt=""
                  className="w-full h-full object-cover"
                  loading="lazy"
                />
              </a>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
