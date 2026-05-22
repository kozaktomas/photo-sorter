import { useEffect, useState, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Download, Lock, X, ChevronLeft, ChevronRight } from 'lucide-react';
import {
  getPublicShare,
  verifyPublicShare,
  listPublicSharePhotos,
  getPublicShareDownloadUrl,
} from '../../api/client';
import type { PublicShareInfo, PublicSharePhoto } from '../../types';

type Stage =
  | { kind: 'loading' }
  | { kind: 'error'; status: number; message: string }
  | { kind: 'password'; info: PublicShareInfo }
  | { kind: 'gallery'; info: PublicShareInfo };

interface ExtendedError {
  status?: number;
  message?: string;
}

function isApiError(err: unknown): err is ExtendedError {
  return typeof err === 'object' && err !== null;
}

export function PublicSharePage() {
  const { slug = '' } = useParams<{ slug: string }>();
  const { t, i18n } = useTranslation(['pages', 'common']);
  const [stage, setStage] = useState<Stage>({ kind: 'loading' });
  const [photos, setPhotos] = useState<PublicSharePhoto[]>([]);
  const [photosLoading, setPhotosLoading] = useState(false);
  const [photosError, setPhotosError] = useState<string | null>(null);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const [passwordInput, setPasswordInput] = useState('');
  const [verifying, setVerifying] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);

  const loadInfo = useCallback(async () => {
    try {
      const info = await getPublicShare(slug);
      if (info.has_password && !info.album) {
        setStage({ kind: 'password', info });
      } else {
        setStage({ kind: 'gallery', info });
      }
    } catch (err) {
      if (isApiError(err)) {
        const status = typeof err.status === 'number' ? err.status : 0;
        setStage({
          kind: 'error',
          status,
          message: err.message ?? t('pages:share.public.unknownError'),
        });
      } else {
        setStage({
          kind: 'error',
          status: 0,
          message: String(err),
        });
      }
    }
  }, [slug, t]);

  useEffect(() => {
    if (!slug) {
      setStage({
        kind: 'error',
        status: 404,
        message: t('pages:share.public.notFound'),
      });
      return;
    }
    void loadInfo();
  }, [slug, loadInfo, t]);

  useEffect(() => {
    if (stage.kind !== 'gallery') return;
    let cancelled = false;
    setPhotosLoading(true);
    setPhotosError(null);
    listPublicSharePhotos(slug, { limit: 500 })
      .then((data) => {
        if (!cancelled) setPhotos(data.photos);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setPhotosError(err instanceof Error ? err.message : String(err));
        }
      })
      .finally(() => {
        if (!cancelled) setPhotosLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [stage, slug]);

  useEffect(() => {
    if (lightboxIndex === null) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setLightboxIndex(null);
      if (e.key === 'ArrowLeft' && lightboxIndex !== null) {
        setLightboxIndex(Math.max(0, lightboxIndex - 1));
      }
      if (e.key === 'ArrowRight' && lightboxIndex !== null) {
        setLightboxIndex(Math.min(photos.length - 1, lightboxIndex + 1));
      }
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [lightboxIndex, photos.length]);

  const handleVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!passwordInput) return;
    setVerifying(true);
    setPasswordError(null);
    try {
      await verifyPublicShare(slug, passwordInput);
      setPasswordInput('');
      await loadInfo();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setPasswordError(message);
    } finally {
      setVerifying(false);
    }
  };

  if (stage.kind === 'loading') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-900 text-slate-400">
        {t('common:status.loading')}
      </div>
    );
  }

  if (stage.kind === 'error') {
    return <ErrorPage status={stage.status} message={stage.message} />;
  }

  if (stage.kind === 'password') {
    return (
      <PasswordGate
        expiresAt={stage.info.expires_at}
        onSubmit={handleVerify}
        passwordInput={passwordInput}
        setPasswordInput={setPasswordInput}
        verifying={verifying}
        error={passwordError}
        locale={i18n.language}
      />
    );
  }

  const album = stage.info.album;
  if (!album) {
    return <ErrorPage status={500} message={t('pages:share.public.unknownError')} />;
  }
  const currentPhoto = lightboxIndex !== null ? photos[lightboxIndex] : null;

  return (
    <div className="min-h-screen bg-slate-900 text-slate-100">
      <header className="border-b border-slate-800 px-6 py-4">
        <h1 className="text-2xl font-bold">{album.title}</h1>
        <p className="text-sm text-slate-400 mt-1">
          {t('pages:share.public.photoCount', { count: album.photo_count })}
          {stage.info.expires_at && (
            <span className="ml-3 text-amber-300">
              {t('pages:share.public.expiresOn', {
                date: new Date(stage.info.expires_at).toLocaleString(i18n.language),
              })}
            </span>
          )}
        </p>
      </header>

      <main className="px-6 py-6">
        {photosLoading && (
          <div className="text-slate-400 text-sm">{t('common:status.loading')}</div>
        )}
        {photosError && (
          <div className="mb-4 px-3 py-2 rounded bg-red-900/30 border border-red-800 text-red-200 text-sm">
            {photosError}
          </div>
        )}
        {!photosLoading && photos.length === 0 && !photosError && (
          <div className="text-slate-500 italic">{t('pages:share.public.noPhotos')}</div>
        )}
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-2">
          {photos.map((p, idx) => (
            <button
              key={p.uid}
              onClick={() => setLightboxIndex(idx)}
              className="aspect-square overflow-hidden rounded bg-slate-800 hover:opacity-90 transition-opacity"
            >
              <img
                src={p.thumb_url}
                alt={p.title || p.uid}
                loading="lazy"
                className="w-full h-full object-cover"
              />
            </button>
          ))}
        </div>
      </main>

      {currentPhoto && (
        <Lightbox
          photo={currentPhoto}
          slug={slug}
          canPrev={lightboxIndex !== null && lightboxIndex > 0}
          canNext={lightboxIndex !== null && lightboxIndex < photos.length - 1}
          onClose={() => setLightboxIndex(null)}
          onPrev={() => setLightboxIndex((i) => (i === null ? null : Math.max(0, i - 1)))}
          onNext={() => setLightboxIndex((i) =>
            i === null ? null : Math.min(photos.length - 1, i + 1)
          )}
        />
      )}
    </div>
  );
}

function ErrorPage({ status, message }: { status: number; message: string }) {
  const { t } = useTranslation(['pages']);
  let title = t('pages:share.public.errorTitle');
  if (status === 404) title = t('pages:share.public.notFound');
  if (status === 410) title = t('pages:share.public.expiredTitle');
  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-900 px-6">
      <div className="max-w-md text-center">
        <h1 className="text-2xl font-bold text-white mb-3">{title}</h1>
        <p className="text-slate-400">{message}</p>
      </div>
    </div>
  );
}

interface PasswordGateProps {
  expiresAt: string | null;
  onSubmit: (e: React.FormEvent) => void | Promise<void>;
  passwordInput: string;
  setPasswordInput: (s: string) => void;
  verifying: boolean;
  error: string | null;
  locale: string;
}

function PasswordGate(props: PasswordGateProps) {
  const { t } = useTranslation(['pages', 'common']);
  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-900 px-6">
      <form
        onSubmit={(e) => void props.onSubmit(e)}
        className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl p-6 max-w-sm w-full"
      >
        <div className="flex items-center gap-2 mb-3">
          <Lock className="h-5 w-5 text-amber-300" />
          <h1 className="text-lg font-semibold text-white">
            {t('pages:share.public.passwordRequired')}
          </h1>
        </div>
        <p className="text-sm text-slate-400 mb-4">
          {t('pages:share.public.passwordHelp')}
        </p>
        {props.expiresAt && (
          <p className="text-xs text-amber-300 mb-3">
            {t('pages:share.public.expiresOn', {
              date: new Date(props.expiresAt).toLocaleString(props.locale),
            })}
          </p>
        )}
        <input
          type="password"
          value={props.passwordInput}
          onChange={(e) => props.setPasswordInput(e.target.value)}
          autoFocus
          required
          minLength={1}
          className="w-full px-3 py-2 bg-slate-900 border border-slate-700 rounded text-white text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
        />
        {props.error && (
          <div className="mt-2 text-sm text-red-400">{props.error}</div>
        )}
        <button
          type="submit"
          disabled={props.verifying || !props.passwordInput}
          className="mt-4 w-full bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white px-4 py-2 rounded text-sm font-medium"
        >
          {props.verifying ? t('common:status.loading') : t('pages:share.public.unlock')}
        </button>
      </form>
    </div>
  );
}

interface LightboxProps {
  photo: PublicSharePhoto;
  slug: string;
  canPrev: boolean;
  canNext: boolean;
  onClose: () => void;
  onPrev: () => void;
  onNext: () => void;
}

function Lightbox(props: LightboxProps) {
  const { t } = useTranslation(['pages', 'common']);
  return (
    <div
      className="fixed inset-0 z-50 bg-black/95 flex items-center justify-center"
      onClick={props.onClose}
    >
      <button
        onClick={(e) => {
          e.stopPropagation();
          props.onClose();
        }}
        className="absolute top-4 right-4 text-white/80 hover:text-white p-2"
        aria-label={t('common:buttons.close', { defaultValue: 'Close' })}
      >
        <X className="h-6 w-6" />
      </button>

      {props.canPrev && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            props.onPrev();
          }}
          className="absolute left-4 top-1/2 -translate-y-1/2 text-white/80 hover:text-white p-2 bg-black/30 rounded-full"
          aria-label={t('pages:share.public.previous')}
        >
          <ChevronLeft className="h-6 w-6" />
        </button>
      )}
      {props.canNext && (
        <button
          onClick={(e) => {
            e.stopPropagation();
            props.onNext();
          }}
          className="absolute right-4 top-1/2 -translate-y-1/2 text-white/80 hover:text-white p-2 bg-black/30 rounded-full"
          aria-label={t('pages:share.public.next')}
        >
          <ChevronRight className="h-6 w-6" />
        </button>
      )}

      <div
        className="flex flex-col items-center max-h-screen max-w-screen-xl p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <img
          src={props.photo.thumb_url.replace(/fit_720$/, 'fit_3840')}
          alt={props.photo.title || props.photo.uid}
          className="max-h-[80vh] max-w-full object-contain"
        />
        <a
          href={getPublicShareDownloadUrl(props.slug, props.photo.uid)}
          className="mt-4 inline-flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded text-sm font-medium"
        >
          <Download className="h-4 w-4" />
          {t('pages:share.public.download')}
        </a>
      </div>
    </div>
  );
}

export default PublicSharePage;
