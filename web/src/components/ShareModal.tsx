import { useEffect, useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Copy, Trash2, Lock, Clock, Link as LinkIcon, X } from 'lucide-react';
import { Button } from './Button';
import { ConfirmDialog } from './ConfirmDialog';
import { useToast } from './Toast';
import { useCopyToClipboard } from '../hooks/useCopyToClipboard';
import {
  createShareLink,
  listShareLinks,
  revokeShareLink,
} from '../api/client';
import type { ShareLink } from '../types';

interface ShareModalProps {
  open: boolean;
  albumUid: string;
  albumTitle: string;
  onClose: () => void;
}

function suggestSlugFromTitle(title: string): string {
  return title
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64);
}

function buildAbsoluteUrl(slug: string): string {
  return `${window.location.origin}/share/${slug}`;
}

function formatExpiration(iso: string | null, locale: string): string {
  if (!iso) return '';
  try {
    return new Date(iso).toLocaleString(locale);
  } catch {
    return iso;
  }
}

export function ShareModal({ open, albumUid, albumTitle, onClose }: ShareModalProps) {
  const { t, i18n } = useTranslation(['pages', 'common']);
  const toast = useToast();
  const copy = useCopyToClipboard();
  const [links, setLinks] = useState<ShareLink[]>([]);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingRevoke, setPendingRevoke] = useState<string | null>(null);

  const [slug, setSlug] = useState('');
  const [password, setPassword] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [useExpiration, setUseExpiration] = useState(false);

  const reset = useCallback(() => {
    setSlug(suggestSlugFromTitle(albumTitle));
    setPassword('');
    setExpiresAt('');
    setUseExpiration(false);
    setError(null);
  }, [albumTitle]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listShareLinks(albumUid);
      setLinks(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [albumUid]);

  useEffect(() => {
    if (!open) return;
    reset();
    void load();
  }, [open, reset, load]);

  useEffect(() => {
    if (!open) return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, onClose]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError(null);
    try {
      const expiresIso = useExpiration && expiresAt
        ? new Date(expiresAt).toISOString()
        : '';
      await createShareLink(albumUid, {
        slug: slug.trim() || undefined,
        password: password || undefined,
        expires_at: expiresIso || undefined,
      });
      toast.success(t('common:toasts.share.minted'));
      reset();
      await load();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      // Form-level errors that block creation stay inline — toasts are for
      // results, not validation feedback.
      setError(msg);
      toast.error(t('common:toasts.share.mintFailed'));
    } finally {
      setCreating(false);
    }
  };

  const confirmRevoke = async () => {
    const slug = pendingRevoke;
    setPendingRevoke(null);
    if (!slug) return;
    try {
      await revokeShareLink(slug);
      toast.success(t('common:toasts.share.revoked'));
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('common:toasts.share.revokeFailed'));
    }
  };

  const handleCopy = (link: ShareLink) => {
    const url = buildAbsoluteUrl(link.slug);
    void copy(url, t('common:toasts.share.urlCopied'));
  };

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="share-modal-title"
    >
      <div className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl max-w-2xl w-full mx-4 p-6 max-h-[90vh] overflow-y-auto">
        <div className="flex items-start justify-between mb-4">
          <div>
            <h2 id="share-modal-title" className="text-lg font-semibold text-white">
              {t('pages:share.title')}
            </h2>
            <p className="text-slate-400 text-sm mt-1">
              {t('pages:share.subtitle', { albumTitle })}
            </p>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-white"
            aria-label={t('common:buttons.close', { defaultValue: 'Close' })}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {error && (
          <div className="mb-4 px-3 py-2 rounded bg-red-900/30 border border-red-800 text-red-200 text-sm">
            {error}
          </div>
        )}

        {/* Existing links */}
        <div className="mb-6">
          <h3 className="text-sm font-semibold text-slate-300 mb-2">
            {t('pages:share.existingLinks')}
          </h3>
          {loading ? (
            <div className="text-slate-400 text-sm">{t('common:status.loading')}</div>
          ) : links.length === 0 ? (
            <div className="text-slate-500 text-sm italic">{t('pages:share.noLinks')}</div>
          ) : (
            <ul className="space-y-2">
              {links.map((link) => {
                const url = buildAbsoluteUrl(link.slug);
                return (
                  <li
                    key={link.slug}
                    className="border border-slate-700 rounded p-3 bg-slate-900/50"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 text-sm text-slate-200 min-w-0">
                        <LinkIcon className="h-4 w-4 text-slate-400 shrink-0" />
                        <span className="font-mono truncate" title={url}>
                          {url}
                        </span>
                      </div>
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => handleCopy(link)}
                          className="text-slate-400 hover:text-white p-1 rounded"
                          title={t('pages:share.copyUrl')}
                        >
                          <Copy className="h-4 w-4" />
                        </button>
                        <button
                          onClick={() => setPendingRevoke(link.slug)}
                          className="text-red-400 hover:text-red-300 p-1 rounded"
                          title={t('pages:share.revoke')}
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-3 mt-2 text-xs text-slate-400">
                      {link.has_password && (
                        <span className="inline-flex items-center gap-1 text-amber-300">
                          <Lock className="h-3 w-3" /> {t('pages:share.passwordProtected')}
                        </span>
                      )}
                      {link.expires_at && (
                        <span className="inline-flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {t('pages:share.expiresOn', {
                            date: formatExpiration(link.expires_at, i18n.language),
                          })}
                        </span>
                      )}
                      {!link.expires_at && (
                        <span className="inline-flex items-center gap-1">
                          <Clock className="h-3 w-3" /> {t('pages:share.noExpiration')}
                        </span>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        {/* Create form */}
        <form onSubmit={(e) => void handleCreate(e)} className="space-y-3 border-t border-slate-700 pt-4">
          <h3 className="text-sm font-semibold text-slate-300">
            {t('pages:share.createNew')}
          </h3>

          <div>
            <label className="block text-xs text-slate-400 mb-1">
              {t('pages:share.slugLabel')}
            </label>
            <input
              type="text"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder={t('pages:share.slugPlaceholder')}
              pattern="^[a-z0-9-]{3,64}$"
              className="w-full px-3 py-2 bg-slate-900 border border-slate-700 rounded text-white text-sm font-mono focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            />
            <p className="text-xs text-slate-500 mt-1">{t('pages:share.slugHint')}</p>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">
              {t('pages:share.passwordLabel')}
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t('pages:share.passwordPlaceholder')}
              className="w-full px-3 py-2 bg-slate-900 border border-slate-700 rounded text-white text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            />
          </div>

          <div>
            <label className="inline-flex items-center gap-2 text-xs text-slate-400">
              <input
                type="checkbox"
                checked={useExpiration}
                onChange={(e) => setUseExpiration(e.target.checked)}
                className="rounded border-slate-600 bg-slate-800"
              />
              {t('pages:share.setExpiration')}
            </label>
            {useExpiration && (
              <input
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                className="mt-1 w-full px-3 py-2 bg-slate-900 border border-slate-700 rounded text-white text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
              />
            )}
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="secondary" size="sm" onClick={onClose}>
              {t('common:buttons.close', { defaultValue: 'Close' })}
            </Button>
            <Button type="submit" size="sm" isLoading={creating}>
              {t('pages:share.create')}
            </Button>
          </div>
        </form>
      </div>

      <ConfirmDialog
        open={pendingRevoke !== null}
        title={t('pages:share.revoke')}
        message={t('pages:share.confirmRevoke')}
        confirmLabel={t('pages:share.revoke')}
        cancelLabel={t('common:buttons.cancel')}
        variant="danger"
        onConfirm={() => void confirmRevoke()}
        onCancel={() => setPendingRevoke(null)}
      />
    </div>
  );
}
