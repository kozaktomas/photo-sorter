import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '../../components/Button';
import { FormInput } from '../../components/FormInput';
import { FormSelect } from '../../components/FormSelect';
import { Alert } from '../../components/Alert';
import { createUser, updateUser } from '../../api/client';
import type { UserAccount } from '../../api/client';

interface EditUserDialogProps {
  open: boolean;
  user: UserAccount | null;
  onClose: () => void;
  onSaved: () => void;
}

interface FormState {
  username: string;
  display_name: string;
  email: string;
  role: string;
  password: string;
}

const ROLE_OPTIONS = ['admin', 'editor', 'viewer'] as const;

function initialState(user: UserAccount | null): FormState {
  return {
    username: user?.username ?? '',
    display_name: user?.display_name ?? '',
    email: user?.email ?? '',
    role: user?.role ?? 'viewer',
    password: '',
  };
}

export function EditUserDialog({ open, user, onClose, onSaved }: EditUserDialogProps) {
  const { t } = useTranslation(['pages', 'common']);
  const isEdit = user !== null;
  const [form, setForm] = useState<FormState>(initialState(user));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setForm(initialState(user));
      setError(null);
    }
  }, [open, user]);

  useEffect(() => {
    if (!open) return;
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!isEdit) {
      if (form.password.length < 8) {
        setError(t('pages:settings.users.passwordTooShort'));
        return;
      }
    }

    setSaving(true);
    try {
      if (isEdit && user) {
        await updateUser(user.uid, {
          display_name: form.display_name,
          email: form.email,
          role: form.role,
        });
      } else {
        await createUser({
          username: form.username,
          password: form.password,
          display_name: form.display_name,
          email: form.email,
          role: form.role,
        });
      }
      onSaved();
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('pages:settings.users.saveFailed');
      setError(msg);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={handleBackdropClick}
      role="dialog"
      aria-modal="true"
      aria-labelledby="edit-user-title"
    >
      <div className="bg-slate-800 border border-slate-700 rounded-lg shadow-xl max-w-md w-full mx-4 p-6">
        <h2 id="edit-user-title" className="text-lg font-semibold text-white mb-4">
          {isEdit ? t('pages:settings.users.editTitle') : t('pages:settings.users.createTitle')}
        </h2>

        <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
          <FormInput
            label={t('pages:settings.users.username')}
            id="user-username"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
            disabled={isEdit || saving}
            required
            autoFocus={!isEdit}
            autoComplete="off"
          />
          {!isEdit && (
            <p className="-mt-2 text-xs text-slate-500">{t('pages:settings.users.usernameHelp')}</p>
          )}

          <FormInput
            label={t('pages:settings.users.displayName')}
            id="user-display-name"
            value={form.display_name}
            onChange={(e) => setForm({ ...form, display_name: e.target.value })}
            disabled={saving}
            autoComplete="off"
          />

          <FormInput
            label={t('pages:settings.users.email')}
            id="user-email"
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            disabled={saving}
            autoComplete="off"
          />

          <FormSelect
            label={t('pages:settings.users.role')}
            id="user-role"
            value={form.role}
            onChange={(e) => setForm({ ...form, role: e.target.value })}
            disabled={saving}
          >
            {ROLE_OPTIONS.map((r) => (
              <option key={r} value={r}>
                {t(`pages:settings.users.roles${r.charAt(0).toUpperCase()}${r.slice(1)}`)}
              </option>
            ))}
          </FormSelect>

          {!isEdit && (
            <FormInput
              label={t('pages:settings.users.password')}
              id="user-password"
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              disabled={saving}
              required
              autoComplete="new-password"
            />
          )}

          {error && <Alert variant="error">{error}</Alert>}

          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" size="sm" onClick={onClose} disabled={saving}>
              {t('pages:settings.users.cancel')}
            </Button>
            <Button type="submit" variant="primary" size="sm" isLoading={saving}>
              {isEdit ? t('pages:settings.users.save') : t('pages:settings.users.create')}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
