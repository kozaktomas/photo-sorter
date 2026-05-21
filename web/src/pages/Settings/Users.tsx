import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Pencil, Key, Lock, LockOpen, Trash2, UserPlus } from 'lucide-react';
import { Button } from '../../components/Button';
import { Card, CardContent } from '../../components/Card';
import { Alert } from '../../components/Alert';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { FormInput } from '../../components/FormInput';
import { LoadingState } from '../../components/LoadingState';
import { useAuth } from '../../hooks/useAuth';
import {
  listUsers,
  deleteUser,
  setUserDisabled,
  setUserPassword,
} from '../../api/client';
import type { UserAccount } from '../../api/client';
import { EditUserDialog } from './EditUserDialog';

type PendingConfirm =
  | { kind: 'disable'; user: UserAccount }
  | { kind: 'enable'; user: UserAccount }
  | { kind: 'delete'; user: UserAccount };

function formatLastLogin(iso: string | null, locale: string, fallback: string): string {
  if (!iso) return fallback;
  try {
    return new Date(iso).toLocaleString(locale);
  } catch {
    return iso;
  }
}

interface SetPasswordDialogProps {
  user: UserAccount | null;
  onClose: () => void;
  onSuccess: () => void;
}

function SetPasswordDialog({ user, onClose, onSuccess }: SetPasswordDialogProps) {
  const { t } = useTranslation(['pages']);
  const [password, setPassword] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (user) {
      setPassword('');
      setError(null);
    }
  }, [user]);

  useEffect(() => {
    if (!user) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [user, onClose]);

  if (!user) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < 8) {
      setError(t('pages:settings.users.passwordTooShort'));
      return;
    }
    setSaving(true);
    try {
      await setUserPassword(user.uid, password);
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('pages:settings.users.saveFailed'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      role="dialog"
      aria-modal="true"
      aria-labelledby="set-password-title"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl max-w-sm w-full mx-4 p-6">
        <h2 id="set-password-title" className="text-lg font-semibold text-white mb-4">
          {t('pages:settings.users.setPasswordTitle', { username: user.username })}
        </h2>
        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
          <FormInput
            label={t('pages:settings.users.newPassword')}
            id="set-password-input"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoFocus
            required
            autoComplete="new-password"
            disabled={saving}
          />
          {error && <Alert variant="error">{error}</Alert>}
          <div className="flex justify-end gap-3">
            <Button type="button" variant="secondary" size="sm" onClick={onClose} disabled={saving}>
              {t('pages:settings.users.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" isLoading={saving}>
              {t('pages:settings.users.save')}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

export function UsersTab() {
  const { t, i18n } = useTranslation(['pages', 'common']);
  const { user: currentUser } = useAuth();
  const [users, setUsers] = useState<UserAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<UserAccount | null>(null);
  const [pwdTarget, setPwdTarget] = useState<UserAccount | null>(null);
  const [confirm, setConfirm] = useState<PendingConfirm | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listUsers();
      setUsers(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('pages:settings.users.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const adminCount = useMemo(
    () => users.filter((u) => u.role === 'admin' && !u.disabled).length,
    [users],
  );

  const openCreate = () => {
    setEditTarget(null);
    setEditOpen(true);
  };

  const openEdit = (u: UserAccount) => {
    setEditTarget(u);
    setEditOpen(true);
  };

  const closeEdit = () => setEditOpen(false);

  const handleEditSaved = () => {
    setEditOpen(false);
    void reload();
  };

  const handlePasswordSuccess = () => {
    setPwdTarget(null);
    setActionSuccess(t('pages:settings.users.passwordSetSuccess'));
    setActionError(null);
  };

  const runConfirm = async () => {
    if (!confirm) return;
    setActionError(null);
    try {
      if (confirm.kind === 'delete') {
        await deleteUser(confirm.user.uid);
      } else if (confirm.kind === 'disable') {
        await setUserDisabled(confirm.user.uid, true);
      } else {
        await setUserDisabled(confirm.user.uid, false);
      }
      setConfirm(null);
      await reload();
    } catch (err) {
      const msg =
        confirm.kind === 'delete'
          ? t('pages:settings.users.deleteFailed')
          : t('pages:settings.users.saveFailed');
      setActionError(err instanceof Error ? err.message : msg);
      setConfirm(null);
    }
  };

  const confirmTitle = (): string => {
    if (!confirm) return '';
    if (confirm.kind === 'disable') return t('pages:settings.users.confirmDisableTitle');
    if (confirm.kind === 'enable') return t('pages:settings.users.confirmEnableTitle');
    return t('pages:settings.users.confirmDeleteTitle');
  };

  const confirmMessage = (): string => {
    if (!confirm) return '';
    if (confirm.kind === 'disable') {
      return t('pages:settings.users.confirmDisableMessage', { username: confirm.user.username });
    }
    if (confirm.kind === 'enable') {
      return t('pages:settings.users.confirmEnableMessage', { username: confirm.user.username });
    }
    return t('pages:settings.users.confirmDeleteMessage', { username: confirm.user.username });
  };

  const confirmVariant: 'danger' | 'primary' = confirm?.kind === 'delete' ? 'danger' : 'primary';
  const confirmLabel = (): string => {
    if (!confirm) return '';
    if (confirm.kind === 'disable') return t('pages:settings.users.disable');
    if (confirm.kind === 'enable') return t('pages:settings.users.enable');
    return t('pages:settings.users.delete');
  };

  const isCurrent = (u: UserAccount) => currentUser?.uid === u.uid;
  const isLastAdmin = (u: UserAccount) =>
    u.role === 'admin' && !u.disabled && adminCount <= 1;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-white">{t('pages:settings.users.title')}</h2>
        <Button variant="primary" size="sm" onClick={openCreate}>
          <UserPlus className="h-4 w-4 mr-2" />
          {t('pages:settings.users.newUser')}
        </Button>
      </div>

      {actionError && <Alert variant="error">{actionError}</Alert>}
      {actionSuccess && <Alert variant="success">{actionSuccess}</Alert>}

      <Card>
        <CardContent className="p-0">
          <LoadingState
            isLoading={loading}
            error={error}
            isEmpty={!loading && !error && users.length === 0}
            emptyTitle={t('pages:settings.users.noUsers')}
          >
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-slate-900/50 text-slate-300">
                  <tr>
                    <th className="text-left px-4 py-3 font-medium">{t('pages:settings.users.username')}</th>
                    <th className="text-left px-4 py-3 font-medium">{t('pages:settings.users.displayName')}</th>
                    <th className="text-left px-4 py-3 font-medium">{t('pages:settings.users.role')}</th>
                    <th className="text-left px-4 py-3 font-medium">{t('pages:settings.users.disabled')}</th>
                    <th className="text-left px-4 py-3 font-medium">{t('pages:settings.users.lastLogin')}</th>
                    <th className="text-right px-4 py-3 font-medium">{t('pages:settings.users.actions')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-700">
                  {users.map((u) => {
                    const deleteDisabled = isCurrent(u) || isLastAdmin(u);
                    const deleteTitle = isLastAdmin(u)
                      ? t('pages:settings.users.lastAdminWarning')
                      : t('pages:settings.users.delete');
                    return (
                      <tr key={u.uid} className="text-slate-200">
                        <td className="px-4 py-3 font-mono">{u.username}</td>
                        <td className="px-4 py-3">{u.display_name || '—'}</td>
                        <td className="px-4 py-3">
                          {t(
                            `pages:settings.users.roles${u.role.charAt(0).toUpperCase()}${u.role.slice(1)}`,
                            { defaultValue: u.role },
                          )}
                        </td>
                        <td className="px-4 py-3">
                          {u.disabled ? (
                            <span className="inline-block text-xs px-2 py-0.5 rounded-full bg-red-500/10 text-red-400 border border-red-500/20">
                              {t('pages:settings.users.disabled')}
                            </span>
                          ) : (
                            <span className="inline-block text-xs px-2 py-0.5 rounded-full bg-green-500/10 text-green-400 border border-green-500/20">
                              {t('pages:settings.users.active')}
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-slate-400">
                          {formatLastLogin(
                            u.last_login_at,
                            i18n.language,
                            t('pages:settings.users.neverLoggedIn'),
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center justify-end gap-1">
                            <button
                              type="button"
                              onClick={() => openEdit(u)}
                              title={t('pages:settings.users.edit')}
                              className="p-1.5 rounded text-slate-300 hover:bg-slate-700 hover:text-white"
                            >
                              <Pencil className="h-4 w-4" />
                            </button>
                            <button
                              type="button"
                              onClick={() => setPwdTarget(u)}
                              title={t('pages:settings.users.setPassword')}
                              className="p-1.5 rounded text-slate-300 hover:bg-slate-700 hover:text-white"
                            >
                              <Key className="h-4 w-4" />
                            </button>
                            <button
                              type="button"
                              onClick={() =>
                                setConfirm({
                                  kind: u.disabled ? 'enable' : 'disable',
                                  user: u,
                                })
                              }
                              title={
                                u.disabled
                                  ? t('pages:settings.users.enable')
                                  : t('pages:settings.users.disable')
                              }
                              className="p-1.5 rounded text-slate-300 hover:bg-slate-700 hover:text-white"
                            >
                              {u.disabled ? <LockOpen className="h-4 w-4" /> : <Lock className="h-4 w-4" />}
                            </button>
                            <button
                              type="button"
                              onClick={() => setConfirm({ kind: 'delete', user: u })}
                              disabled={deleteDisabled}
                              title={deleteTitle}
                              className="p-1.5 rounded text-red-400 hover:bg-red-500/10 hover:text-red-300 disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:bg-transparent"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </LoadingState>
        </CardContent>
      </Card>

      <EditUserDialog
        open={editOpen}
        user={editTarget}
        onClose={closeEdit}
        onSaved={handleEditSaved}
      />

      <SetPasswordDialog
        user={pwdTarget}
        onClose={() => setPwdTarget(null)}
        onSuccess={handlePasswordSuccess}
      />

      <ConfirmDialog
        open={confirm !== null}
        title={confirmTitle()}
        message={confirmMessage()}
        confirmLabel={confirmLabel()}
        cancelLabel={t('pages:settings.users.cancel')}
        variant={confirmVariant}
        onConfirm={() => void runConfirm()}
        onCancel={() => setConfirm(null)}
      />
    </div>
  );
}
