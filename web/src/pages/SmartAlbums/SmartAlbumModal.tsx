import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { X, Filter as FilterIcon, Tag, User as UserIcon, Star, MapPin } from 'lucide-react';
import { Button } from '../../components/Button';
import { Combobox } from '../../components/Combobox';
import { getLabels, getSubjects } from '../../api/client';
import type { Label, Subject, SmartAlbum, SmartAlbumFilters } from '../../types';
import { SORT_OPTIONS } from '../Photos/hooks/usePhotosFilters';

interface SmartAlbumModalProps {
  open: boolean;
  /** When set, the modal opens in edit mode pre-filled from the album. */
  album?: SmartAlbum;
  onClose: () => void;
  onSubmit: (name: string, filters: SmartAlbumFilters) => Promise<void>;
}

// Local state shape — the filter map is held as discrete fields so the form
// controls stay simple. We marshal back to the API shape on submit.
interface FormState {
  name: string;
  search: string;
  labelUids: string[];
  subjectUids: string[];
  favorite: boolean;
  takenFrom: string;
  takenTo: string;
  bboxInput: string;
  sort: string;
}

function bboxToInput(f?: SmartAlbumFilters): string {
  if (!f) return '';
  if (
    f.min_lat === undefined ||
    f.min_lng === undefined ||
    f.max_lat === undefined ||
    f.max_lng === undefined
  ) {
    return '';
  }
  return `${f.min_lat},${f.min_lng},${f.max_lat},${f.max_lng}`;
}

function dateInput(iso?: string): string {
  if (!iso) return '';
  // The API returns RFC3339; the <input type="date"> wants YYYY-MM-DD.
  return iso.slice(0, 10);
}

function parseBBox(raw: string): { ok: boolean; bbox?: [number, number, number, number] } {
  const trimmed = raw.trim();
  if (!trimmed) return { ok: true };
  const parts = trimmed.split(',').map((s) => s.trim());
  if (parts.length !== 4) return { ok: false };
  const nums = parts.map((p) => Number(p));
  if (nums.some((n) => !Number.isFinite(n))) return { ok: false };
  const [a, b, c, d] = nums;
  return {
    ok: true,
    bbox: [Math.min(a, c), Math.min(b, d), Math.max(a, c), Math.max(b, d)],
  };
}

function initialState(album?: SmartAlbum): FormState {
  const f = album?.filters ?? {};
  return {
    name: album?.name ?? '',
    search: f.q ?? '',
    labelUids: f.label_uids ?? [],
    subjectUids: f.subject_uids ?? [],
    favorite: f.favorite ?? false,
    takenFrom: dateInput(f.taken_from),
    takenTo: dateInput(f.taken_to),
    bboxInput: bboxToInput(f),
    sort: f.sort ?? '',
  };
}

export function SmartAlbumModal({ open, album, onClose, onSubmit }: SmartAlbumModalProps) {
  const { t } = useTranslation(['pages', 'common']);
  const [form, setForm] = useState<FormState>(() => initialState(album));
  const [labels, setLabels] = useState<Label[]>([]);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [labelPick, setLabelPick] = useState('');
  const [subjectPick, setSubjectPick] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setForm(initialState(album));
    setError(null);
    setLabelPick('');
    setSubjectPick('');
  }, [open, album]);

  useEffect(() => {
    if (!open) return;
    void getLabels({ count: 500 }).then(setLabels).catch(() => undefined);
    void getSubjects({ count: 500 }).then(setSubjects).catch(() => undefined);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  const labelLookup = new Map(labels.map((l) => [l.uid, l]));
  const subjectLookup = new Map(subjects.map((s) => [s.uid, s]));

  const handleAddLabel = () => {
    if (!labelPick) return;
    if (form.labelUids.includes(labelPick)) return;
    setForm({ ...form, labelUids: [...form.labelUids, labelPick] });
    setLabelPick('');
  };

  const handleRemoveLabel = (uid: string) => {
    setForm({ ...form, labelUids: form.labelUids.filter((u) => u !== uid) });
  };

  const handleAddSubject = () => {
    if (!subjectPick) return;
    if (form.subjectUids.includes(subjectPick)) return;
    setForm({ ...form, subjectUids: [...form.subjectUids, subjectPick] });
    setSubjectPick('');
  };

  const handleRemoveSubject = (uid: string) => {
    setForm({ ...form, subjectUids: form.subjectUids.filter((u) => u !== uid) });
  };

  const buildFilters = (): SmartAlbumFilters | null => {
    const bboxParsed = parseBBox(form.bboxInput);
    if (!bboxParsed.ok) return null;
    const out: SmartAlbumFilters = {};
    if (form.search.trim()) out.q = form.search.trim();
    if (form.labelUids.length > 0) out.label_uids = form.labelUids;
    if (form.subjectUids.length > 0) out.subject_uids = form.subjectUids;
    if (form.favorite) out.favorite = true;
    if (form.takenFrom) {
      // RFC3339 at start of day in UTC. The Photos handler accepts any
      // RFC3339 timestamp, so this round-trips fine.
      out.taken_from = `${form.takenFrom}T00:00:00Z`;
    }
    if (form.takenTo) {
      out.taken_to = `${form.takenTo}T23:59:59Z`;
    }
    if (bboxParsed.bbox) {
      [out.min_lat, out.min_lng, out.max_lat, out.max_lng] = bboxParsed.bbox;
    }
    if (form.sort) out.sort = form.sort;
    return out;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!form.name.trim()) {
      setError(t('pages:smartAlbums.nameRequired'));
      return;
    }
    const filters = buildFilters();
    if (!filters) {
      setError(t('pages:smartAlbums.bboxInvalid'));
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(form.name.trim(), filters);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
      onClick={onClose}
    >
      <div
        className="bg-slate-900 border border-slate-700 rounded-xl shadow-2xl w-full max-w-2xl max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-slate-700 px-6 py-4">
          <h2 className="text-xl font-semibold text-white flex items-center gap-2">
            <FilterIcon className="h-5 w-5 text-purple-400" />
            {album
              ? t('pages:smartAlbums.modalEditTitle')
              : t('pages:smartAlbums.modalCreateTitle')}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="text-slate-400 hover:text-white"
            aria-label={t('common:buttons.close')}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4">
          {/* Name */}
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1" htmlFor="sa-name">
              {t('pages:smartAlbums.nameLabel')}
            </label>
            <input
              id="sa-name"
              type="text"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder={t('pages:smartAlbums.namePlaceholder')}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
            />
          </div>

          <h3 className="text-sm font-semibold text-slate-300 pt-2">
            {t('pages:smartAlbums.filtersTitle')}
          </h3>

          {/* Search */}
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="sa-search">
              {t('pages:smartAlbums.searchLabel')}
            </label>
            <input
              id="sa-search"
              type="text"
              value={form.search}
              onChange={(e) => setForm({ ...form, search: e.target.value })}
              placeholder={t('pages:smartAlbums.searchPlaceholder')}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
            />
          </div>

          {/* Labels */}
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              <Tag className="inline h-3 w-3 mr-1" />
              {t('pages:smartAlbums.labelsLabel')}
            </label>
            <div className="flex gap-2">
              <Combobox
                value={labelPick}
                onChange={setLabelPick}
                options={labels.map((l) => ({ value: l.uid, label: `${l.name} (${l.photo_count})` }))}
                placeholder={t('pages:smartAlbums.addLabel')}
                className="flex-1"
              />
              <Button type="button" variant="ghost" onClick={handleAddLabel}>
                {t('pages:smartAlbums.addLabel')}
              </Button>
            </div>
            {form.labelUids.length > 0 && (
              <div className="flex flex-wrap gap-2 mt-2">
                {form.labelUids.map((uid) => (
                  <span
                    key={uid}
                    className="inline-flex items-center gap-1 px-2 py-1 bg-purple-900/40 border border-purple-700 rounded text-sm text-purple-100"
                  >
                    {labelLookup.get(uid)?.name ?? uid}
                    <button
                      type="button"
                      onClick={() => handleRemoveLabel(uid)}
                      className="hover:text-white"
                      aria-label={t('pages:smartAlbums.removeLabel')}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>

          {/* Subjects */}
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1">
              <UserIcon className="inline h-3 w-3 mr-1" />
              {t('pages:smartAlbums.subjectsLabel')}
            </label>
            <div className="flex gap-2">
              <Combobox
                value={subjectPick}
                onChange={setSubjectPick}
                options={subjects.map((s) => ({ value: s.uid, label: s.name }))}
                placeholder={t('pages:smartAlbums.addLabel')}
                className="flex-1"
              />
              <Button type="button" variant="ghost" onClick={handleAddSubject}>
                {t('pages:smartAlbums.addLabel')}
              </Button>
            </div>
            {form.subjectUids.length > 0 && (
              <div className="flex flex-wrap gap-2 mt-2">
                {form.subjectUids.map((uid) => (
                  <span
                    key={uid}
                    className="inline-flex items-center gap-1 px-2 py-1 bg-blue-900/40 border border-blue-700 rounded text-sm text-blue-100"
                  >
                    {subjectLookup.get(uid)?.name ?? uid}
                    <button
                      type="button"
                      onClick={() => handleRemoveSubject(uid)}
                      className="hover:text-white"
                      aria-label={t('pages:smartAlbums.removeLabel')}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}
          </div>

          {/* Favorite toggle */}
          <div>
            <label className="inline-flex items-center gap-2 text-sm text-slate-200 cursor-pointer">
              <input
                type="checkbox"
                checked={form.favorite}
                onChange={(e) => setForm({ ...form, favorite: e.target.checked })}
                className="rounded border-slate-600 bg-slate-800 text-purple-500 focus:ring-purple-500"
              />
              <Star className="h-4 w-4 text-yellow-400" />
              {t('pages:smartAlbums.favoriteLabel')}
            </label>
          </div>

          {/* Date range */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="sa-from">
                {t('pages:smartAlbums.dateFromLabel')}
              </label>
              <input
                id="sa-from"
                type="date"
                value={form.takenFrom}
                onChange={(e) => setForm({ ...form, takenFrom: e.target.value })}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="sa-to">
                {t('pages:smartAlbums.dateToLabel')}
              </label>
              <input
                id="sa-to"
                type="date"
                value={form.takenTo}
                onChange={(e) => setForm({ ...form, takenTo: e.target.value })}
                className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
              />
            </div>
          </div>

          {/* BBox */}
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="sa-bbox">
              <MapPin className="inline h-3 w-3 mr-1" />
              {t('pages:smartAlbums.bboxLabel')}
            </label>
            <input
              id="sa-bbox"
              type="text"
              value={form.bboxInput}
              onChange={(e) => setForm({ ...form, bboxInput: e.target.value })}
              placeholder="49.0,16.5,49.5,17.0"
              className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
            />
          </div>

          {/* Sort */}
          <div>
            <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="sa-sort">
              {t('pages:smartAlbums.sortLabel')}
            </label>
            <select
              id="sa-sort"
              value={form.sort}
              onChange={(e) => setForm({ ...form, sort: e.target.value })}
              className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-500"
            >
              <option value="">{t('common:status.default', { defaultValue: '—' })}</option>
              {SORT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {t(opt.label)}
                </option>
              ))}
            </select>
          </div>

          {error && (
            <div className="px-3 py-2 bg-red-900/40 border border-red-700 rounded text-sm text-red-200">
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2 border-t border-slate-700">
            <Button type="button" variant="ghost" onClick={onClose} disabled={submitting}>
              {t('pages:smartAlbums.cancelButton')}
            </Button>
            <Button type="submit" disabled={submitting}>
              {t('pages:smartAlbums.saveButton')}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
