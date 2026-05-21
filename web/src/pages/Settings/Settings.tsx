import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Settings as SettingsIcon } from 'lucide-react';
import { PageHeader } from '../../components/PageHeader';
import { Card, CardContent } from '../../components/Card';
import { Alert } from '../../components/Alert';
import { Button } from '../../components/Button';
import { FormInput } from '../../components/FormInput';
import { LoadingState } from '../../components/LoadingState';
import { changeOwnPassword, getMe } from '../../api/client';
import type { UserAccount } from '../../api/client';
import { UsersTab } from './Users';

type TabId = 'users' | 'profile';

function ProfileTab({ user }: { user: UserAccount }) {
  const { t } = useTranslation(['pages']);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    if (next.length < 8) {
      setError(t('pages:settings.profile.passwordTooShort'));
      return;
    }
    if (next !== confirm) {
      setError(t('pages:settings.profile.passwordMismatch'));
      return;
    }
    setSubmitting(true);
    try {
      await changeOwnPassword(current, next);
      setSuccess(t('pages:settings.profile.passwordChanged'));
      setCurrent('');
      setNext('');
      setConfirm('');
    } catch (err) {
      setError(err instanceof Error ? err.message : t('pages:settings.profile.passwordFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const roleLabel = t(
    `pages:settings.users.roles${user.role.charAt(0).toUpperCase()}${user.role.slice(1)}`,
    { defaultValue: user.role },
  );

  return (
    <div className="space-y-6">
      <Card>
        <CardContent>
          <h2 className="text-lg font-semibold text-white mb-4">
            {t('pages:settings.profile.title')}
          </h2>
          <dl className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
            <div>
              <dt className="text-slate-400">{t('pages:settings.profile.username')}</dt>
              <dd className="text-white font-mono">{user.username}</dd>
            </div>
            <div>
              <dt className="text-slate-400">{t('pages:settings.profile.displayName')}</dt>
              <dd className="text-white">{user.display_name || '—'}</dd>
            </div>
            <div>
              <dt className="text-slate-400">{t('pages:settings.profile.role')}</dt>
              <dd className="text-white">{roleLabel}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <h3 className="text-base font-semibold text-white mb-4">
            {t('pages:settings.profile.changePassword')}
          </h3>
          <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4 max-w-md">
            <FormInput
              label={t('pages:settings.profile.currentPassword')}
              id="profile-current-password"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
              disabled={submitting}
              autoComplete="current-password"
            />
            <FormInput
              label={t('pages:settings.profile.newPassword')}
              id="profile-new-password"
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              required
              disabled={submitting}
              autoComplete="new-password"
            />
            <FormInput
              label={t('pages:settings.profile.confirmPassword')}
              id="profile-confirm-password"
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
              disabled={submitting}
              autoComplete="new-password"
            />
            {error && <Alert variant="error">{error}</Alert>}
            {success && <Alert variant="success">{success}</Alert>}
            <Button type="submit" variant="primary" size="sm" isLoading={submitting}>
              {t('pages:settings.profile.submit')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export function SettingsPage() {
  const { t } = useTranslation(['pages']);
  const [me, setMe] = useState<UserAccount | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const isAdmin = me?.role === 'admin';
  const tabs = useMemo<{ id: TabId; label: string }[]>(() => {
    const list: { id: TabId; label: string }[] = [];
    if (isAdmin) {
      list.push({ id: 'users', label: t('pages:settings.tabs.users') });
    }
    list.push({ id: 'profile', label: t('pages:settings.tabs.profile') });
    return list;
  }, [isAdmin, t]);

  const [activeTab, setActiveTab] = useState<TabId>('profile');

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const data = await getMe();
        if (cancelled) return;
        setMe(data);
        setActiveTab(data.role === 'admin' ? 'users' : 'profile');
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : t('pages:settings.profile.loadFailed'));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [t]);

  return (
    <div>
      <PageHeader
        icon={SettingsIcon}
        title={t('pages:settings.title')}
        subtitle={t('pages:settings.subtitle')}
        color="sky"
      />

      <LoadingState isLoading={loading} error={error}>
        {me && (
          <>
            {tabs.length > 1 && (
              <div className="border-b border-slate-700 mb-6">
                <nav className="-mb-px flex gap-4" aria-label="Tabs">
                  {tabs.map((tab) => {
                    const active = activeTab === tab.id;
                    return (
                      <button
                        key={tab.id}
                        type="button"
                        onClick={() => setActiveTab(tab.id)}
                        aria-current={active ? 'page' : undefined}
                        className={`px-1 py-3 text-sm font-medium border-b-2 transition-colors ${
                          active
                            ? 'border-sky-500 text-sky-400'
                            : 'border-transparent text-slate-400 hover:text-slate-200'
                        }`}
                      >
                        {tab.label}
                      </button>
                    );
                  })}
                </nav>
              </div>
            )}

            {activeTab === 'users' && isAdmin && <UsersTab />}
            {activeTab === 'profile' && <ProfileTab user={me} />}
          </>
        )}
      </LoadingState>
    </div>
  );
}
