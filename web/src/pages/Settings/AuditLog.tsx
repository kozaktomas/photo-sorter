import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FileDown, ChevronDown, ChevronRight } from 'lucide-react';
import { Card, CardContent } from '../../components/Card';
import { Alert } from '../../components/Alert';
import { Button } from '../../components/Button';
import { FormInput } from '../../components/FormInput';
import { FormSelect } from '../../components/FormSelect';
import { LoadingState } from '../../components/LoadingState';
import { listAuditLog, listUsers } from '../../api/client';
import type {
  AuditLogEntry,
  AuditLogFilter,
  UserAccount,
} from '../../api/client';

// Pagination size options. The backend caps `limit` at 200 — anything
// larger is silently clamped, so we don't bother offering bigger
// pages.
const PAGE_SIZE_OPTIONS = [50, 100, 200];

// Entity types the audit log can carry. Kept in sync with the
// audit.EntityX constants in internal/audit/audit.go.
const ENTITY_TYPES = [
  'photo',
  'album',
  'label',
  'subject',
  'user',
  'book',
  'share_link',
  'smart_album',
  'session',
  'marker',
  'process_job',
  'sort_job',
  'book_export',
];

// Actions grouped by category for the action filter dropdown. Keeping
// the groupings here (rather than expanding them into a flat list) lets
// the dropdown render with optgroup headers that match the spec.
const ACTION_GROUPS: { label: string; actions: string[] }[] = [
  {
    label: 'auth',
    actions: ['login', 'logout', 'login_failed', 'password_change'],
  },
  {
    label: 'photo',
    actions: [
      'photo_upload',
      'photo_update',
      'photo_exif_edit',
      'photo_archive',
      'photo_restore',
      'photo_purge',
      'photo_batch_edit',
      'photo_batch_label',
      'photo_edits_update',
      'photo_edits_clear',
    ],
  },
  {
    label: 'album',
    actions: [
      'album_create',
      'album_update',
      'album_delete',
      'album_photos_add',
      'album_photos_remove',
    ],
  },
  { label: 'label', actions: ['label_update', 'label_delete'] },
  { label: 'subject', actions: ['subject_update'] },
  { label: 'face', actions: ['face_apply', 'face_outlier_unassign'] },
  {
    label: 'user',
    actions: [
      'user_create',
      'user_update',
      'user_disable',
      'user_enable',
      'user_delete',
      'user_password_reset',
    ],
  },
  {
    label: 'book',
    actions: ['book_create', 'book_update', 'book_delete', 'book_export_pdf'],
  },
  {
    label: 'share_link',
    actions: [
      'share_link_create',
      'share_link_revoke',
      'share_link_password_verify',
      'share_link_password_failed',
    ],
  },
  {
    label: 'smart_album',
    actions: ['smart_album_create', 'smart_album_update', 'smart_album_delete'],
  },
  {
    label: 'process',
    actions: [
      'process_job_start',
      'process_job_cancel',
      'sort_job_start',
      'sort_job_cancel',
    ],
  },
];

// formatTime renders an ISO timestamp in the user's locale. Falls back
// to the raw string when parsing fails so the column never goes empty.
function formatTime(iso: string, locale: string): string {
  try {
    return new Date(iso).toLocaleString(locale);
  } catch {
    return iso;
  }
}

// downloadCSV builds a CSV blob from the supplied rows and triggers a
// browser download. The metadata column is JSON-serialised so the
// recipient can post-process it; values containing commas / quotes /
// newlines are escaped per RFC 4180.
function downloadCSV(entries: AuditLogEntry[]) {
  const header = [
    'id',
    'created_at',
    'user_uid',
    'user_username',
    'action',
    'entity_type',
    'entity_uid',
    'ip',
    'user_agent',
    'metadata',
  ];
  const escape = (raw: string) => {
    const needsQuote = /[",\n\r]/.test(raw);
    const cleaned = raw.replace(/"/g, '""');
    return needsQuote ? `"${cleaned}"` : cleaned;
  };
  const lines = [header.join(',')];
  for (const e of entries) {
    lines.push(
      [
        String(e.id),
        e.created_at,
        e.user_uid,
        e.user_username,
        e.action,
        e.entity_type,
        e.entity_uid,
        e.ip,
        e.user_agent,
        JSON.stringify(e.metadata),
      ]
        .map(escape)
        .join(','),
    );
  }
  const csv = lines.join('\n');
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  a.download = `audit-log-${stamp}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

// metadataPreview returns a short summary string used as the collapsed
// metadata cell. The default render of `JSON.stringify({})` is "{}" —
// we replace that with a thin em-dash so the column doesn't shout
// "empty" for the (very common) no-metadata case.
function metadataPreview(meta: Record<string, unknown> | undefined): string {
  if (!meta || Object.keys(meta).length === 0) return '—';
  const compact = JSON.stringify(meta);
  return compact.length > 80 ? `${compact.slice(0, 77)}...` : compact;
}

interface AuditRowProps {
  entry: AuditLogEntry;
  locale: string;
}

function AuditRow({ entry, locale }: AuditRowProps) {
  const { t, i18n } = useTranslation(['pages']);
  const [open, setOpen] = useState(false);
  void i18n;

  const actionLabel = t(`pages:settings.auditLog.actions.${entry.action}`, {
    defaultValue: entry.action,
  });
  const entityLabel = entry.entity_type
    ? t(`pages:settings.auditLog.entityTypes.${entry.entity_type}`, {
        defaultValue: entry.entity_type,
      })
    : '—';

  const userDisplay = entry.user_username
    ? entry.user_username
    : entry.user_uid
      ? t('pages:settings.auditLog.deletedUser')
      : t('pages:settings.auditLog.anonymous');

  return (
    <>
      <tr className="border-b border-slate-800 hover:bg-slate-800/40">
        <td className="px-2 py-2 whitespace-nowrap text-xs text-slate-400 font-mono">
          {formatTime(entry.created_at, locale)}
        </td>
        <td className="px-2 py-2 text-xs text-slate-200">{userDisplay}</td>
        <td className="px-2 py-2 text-xs">
          <span className="rounded bg-sky-900/40 text-sky-300 px-1.5 py-0.5 font-mono">
            {actionLabel}
          </span>
        </td>
        <td className="px-2 py-2 text-xs text-slate-300">
          {entry.entity_type ? (
            <>
              <span className="text-slate-400">{entityLabel}</span>
              {entry.entity_uid && (
                <span className="ml-1 font-mono text-slate-400">
                  {entry.entity_uid}
                </span>
              )}
            </>
          ) : (
            '—'
          )}
        </td>
        <td className="px-2 py-2 text-xs text-slate-400 font-mono">
          {entry.ip || '—'}
        </td>
        <td className="px-2 py-2 text-xs">
          <button
            type="button"
            onClick={() => setOpen((p) => !p)}
            className="inline-flex items-center gap-1 text-slate-400 hover:text-slate-200 font-mono"
            title={open
              ? t('pages:settings.auditLog.collapse')
              : t('pages:settings.auditLog.expand')}
          >
            {open ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
            <span className="truncate max-w-xs">{metadataPreview(entry.metadata)}</span>
          </button>
        </td>
      </tr>
      {open && (
        <tr className="border-b border-slate-800 bg-slate-900/60">
          <td colSpan={6} className="px-3 py-2">
            <pre className="text-xs text-slate-300 whitespace-pre-wrap break-all">
              {JSON.stringify(entry.metadata, null, 2)}
            </pre>
            {entry.user_agent && (
              <div className="mt-2 text-xs text-slate-500 break-all">
                <span className="font-semibold">UA:</span> {entry.user_agent}
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

export function AuditLogTab() {
  const { t, i18n } = useTranslation(['pages']);
  const locale = i18n.language;

  const [users, setUsers] = useState<UserAccount[]>([]);
  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [userFilter, setUserFilter] = useState('');
  const [actionFilter, setActionFilter] = useState('');
  const [entityFilter, setEntityFilter] = useState('');
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');
  const [limit, setLimit] = useState(50);
  const [offset, setOffset] = useState(0);

  // toISO converts a `<input type="datetime-local">` value (which has
  // no timezone) into an RFC3339 string by appending the browser's
  // local offset. The backend wants RFC3339; just slapping a "Z" on
  // would silently shift the filter by hours.
  const toISO = useCallback((local: string): string | undefined => {
    if (!local) return undefined;
    try {
      return new Date(local).toISOString();
    } catch {
      return undefined;
    }
  }, []);

  const buildFilter = useCallback((): AuditLogFilter => {
    return {
      user_uid: userFilter || undefined,
      action: actionFilter || undefined,
      entity_type: entityFilter || undefined,
      since: toISO(since),
      until: toISO(until),
      limit,
      offset,
    };
  }, [userFilter, actionFilter, entityFilter, since, until, limit, offset, toISO]);

  // Load users once for the user filter dropdown.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const data = await listUsers();
        if (!cancelled) setUsers(data);
      } catch {
        // Non-fatal: the filter dropdown just falls back to free-form text.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Re-fetch entries whenever any filter changes.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    void (async () => {
      try {
        const page = await listAuditLog(buildFilter());
        if (cancelled) return;
        setEntries(page.entries);
        setTotal(page.total);
      } catch (err) {
        if (cancelled) return;
        setError(
          err instanceof Error ? err.message : t('pages:settings.auditLog.loadFailed'),
        );
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [buildFilter, t]);

  const totalPages = useMemo(() => Math.max(1, Math.ceil(total / limit)), [total, limit]);
  const currentPage = Math.floor(offset / limit) + 1;

  const handleClearFilters = () => {
    setUserFilter('');
    setActionFilter('');
    setEntityFilter('');
    setSince('');
    setUntil('');
    setOffset(0);
  };

  const handlePrev = () => setOffset(Math.max(0, offset - limit));
  const handleNext = () => {
    const nextOffset = offset + limit;
    if (nextOffset < total) setOffset(nextOffset);
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-6 gap-3 mb-3">
            <FormSelect
              label={t('pages:settings.auditLog.filterUser')}
              id="audit-user"
              value={userFilter}
              onChange={(e) => {
                setUserFilter(e.target.value);
                setOffset(0);
              }}
            >
              <option value="">{t('pages:settings.auditLog.filterAll')}</option>
              {users.map((u) => (
                <option key={u.uid} value={u.uid}>
                  {u.username}
                </option>
              ))}
            </FormSelect>
            <FormSelect
              label={t('pages:settings.auditLog.filterAction')}
              id="audit-action"
              value={actionFilter}
              onChange={(e) => {
                setActionFilter(e.target.value);
                setOffset(0);
              }}
            >
              <option value="">{t('pages:settings.auditLog.filterAll')}</option>
              {ACTION_GROUPS.map((g) => (
                <optgroup key={g.label} label={g.label}>
                  {g.actions.map((a) => (
                    <option key={a} value={a}>
                      {t(`pages:settings.auditLog.actions.${a}`, { defaultValue: a })}
                    </option>
                  ))}
                </optgroup>
              ))}
            </FormSelect>
            <FormSelect
              label={t('pages:settings.auditLog.filterEntityType')}
              id="audit-entity"
              value={entityFilter}
              onChange={(e) => {
                setEntityFilter(e.target.value);
                setOffset(0);
              }}
            >
              <option value="">{t('pages:settings.auditLog.filterAll')}</option>
              {ENTITY_TYPES.map((et) => (
                <option key={et} value={et}>
                  {t(`pages:settings.auditLog.entityTypes.${et}`, { defaultValue: et })}
                </option>
              ))}
            </FormSelect>
            <FormInput
              label={t('pages:settings.auditLog.filterSince')}
              id="audit-since"
              type="datetime-local"
              value={since}
              onChange={(e) => {
                setSince(e.target.value);
                setOffset(0);
              }}
            />
            <FormInput
              label={t('pages:settings.auditLog.filterUntil')}
              id="audit-until"
              type="datetime-local"
              value={until}
              onChange={(e) => {
                setUntil(e.target.value);
                setOffset(0);
              }}
            />
            <FormSelect
              label={t('pages:settings.auditLog.filterLimit')}
              id="audit-limit"
              value={String(limit)}
              onChange={(e) => {
                setLimit(Number(e.target.value));
                setOffset(0);
              }}
            >
              {PAGE_SIZE_OPTIONS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </FormSelect>
          </div>
          <div className="flex flex-wrap gap-2 items-center">
            <Button variant="secondary" size="sm" onClick={handleClearFilters}>
              {t('pages:settings.auditLog.filterClear')}
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => downloadCSV(entries)}
              disabled={entries.length === 0}
            >
              <FileDown className="w-3.5 h-3.5 mr-1" />
              {t('pages:settings.auditLog.exportCSV')}
            </Button>
            <span className="text-xs text-slate-400 ml-auto">
              {t('pages:settings.auditLog.showing')} {entries.length} / {total}{' '}
              {t('pages:settings.auditLog.entries')}
            </span>
          </div>
        </CardContent>
      </Card>

      <LoadingState isLoading={loading} error={error}>
        <Card>
          <CardContent>
            {entries.length === 0 ? (
              <Alert variant="warning">{t('pages:settings.auditLog.noEntries')}</Alert>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left">
                  <thead className="border-b border-slate-700">
                    <tr className="text-xs text-slate-400 uppercase">
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnTimestamp')}
                      </th>
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnUser')}
                      </th>
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnAction')}
                      </th>
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnEntity')}
                      </th>
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnIP')}
                      </th>
                      <th className="px-2 py-2">
                        {t('pages:settings.auditLog.columnMetadata')}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {entries.map((e) => (
                      <AuditRow key={e.id} entry={e} locale={locale} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="flex items-center justify-between mt-4">
              <span className="text-xs text-slate-400">
                {t('pages:settings.auditLog.page')} {currentPage} {t('pages:settings.auditLog.of')}{' '}
                {totalPages}
              </span>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handlePrev}
                  disabled={offset === 0}
                >
                  {t('pages:settings.auditLog.previous')}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleNext}
                  disabled={offset + limit >= total}
                >
                  {t('pages:settings.auditLog.next')}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </LoadingState>
    </div>
  );
}
