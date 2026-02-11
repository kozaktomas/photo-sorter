import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Tags, Trash2, Search, ChevronUp, ChevronDown } from 'lucide-react';
import { Card, CardContent } from '../components/Card';
import { Button } from '../components/Button';
import { PageHeader } from '../components/PageHeader';
import { PAGE_CONFIGS } from '../constants/pageConfig';
import { getLabels, deleteLabels } from '../api/client';
import type { Label } from '../types';

type SortField = 'name' | 'photo_count';
type SortDirection = 'asc' | 'desc';

export function LabelsPage() {
  const { t } = useTranslation(['pages', 'common']);
  const [labels, setLabels] = useState<Label[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [sortField, setSortField] = useState<SortField>('name');
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc');
  const [selectedLabels, setSelectedLabels] = useState<Set<string>>(new Set());
  const [isDeleting, setIsDeleting] = useState(false);

  useEffect(() => {
    void loadLabels();
  }, []);

  async function loadLabels() {
    try {
      const data = await getLabels({ count: 5000, all: true });
      setLabels(data);
    } catch (err) {
      console.error('Failed to load labels:', err);
    } finally {
      setIsLoading(false);
    }
  }

  const filteredLabels = labels
    .filter((label) =>
      label.name.toLowerCase().includes(search.toLowerCase())
    )
    .sort((a, b) => {
      const aVal = sortField === 'name' ? a.name.toLowerCase() : a.photo_count;
      const bVal = sortField === 'name' ? b.name.toLowerCase() : b.photo_count;
      if (aVal < bVal) return sortDirection === 'asc' ? -1 : 1;
      if (aVal > bVal) return sortDirection === 'asc' ? 1 : -1;
      return 0;
    });

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortDirection('asc');
    }
  };

  const toggleSelectAll = () => {
    if (selectedLabels.size === filteredLabels.length) {
      setSelectedLabels(new Set());
    } else {
      setSelectedLabels(new Set(filteredLabels.map((l) => l.uid)));
    }
  };

  const toggleLabel = (uid: string) => {
    const newSelected = new Set(selectedLabels);
    if (newSelected.has(uid)) {
      newSelected.delete(uid);
    } else {
      newSelected.add(uid);
    }
    setSelectedLabels(newSelected);
  };

  const handleDelete = async () => {
    if (selectedLabels.size === 0) return;
    if (!confirm(t('pages:labels.deleteConfirm', { count: selectedLabels.size }))) return;

    setIsDeleting(true);
    try {
      await deleteLabels(Array.from(selectedLabels));
      setSelectedLabels(new Set());
      await loadLabels();
    } catch (err) {
      console.error('Failed to delete labels:', err);
      alert(t('common:errors.failedToDelete'));
    } finally {
      setIsDeleting(false);
    }
  };

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return null;
    return sortDirection === 'asc' ? (
      <ChevronUp className="h-4 w-4" />
    ) : (
      <ChevronDown className="h-4 w-4" />
    );
  };

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.labels.icon}
        title={t('pages:labels.title')}
        subtitle={t('common:units.label', { count: labels.length })}
        color="cyan"
        category="browse"
        actions={
          selectedLabels.size > 0 ? (
            <Button variant="danger" onClick={handleDelete} isLoading={isDeleting}>
              <Trash2 className="h-4 w-4 mr-2" />
              {t('common:buttons.delete')} {selectedLabels.size}
            </Button>
          ) : undefined
        }
      />

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-slate-400" />
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('pages:labels.searchPlaceholder')}
          className="w-full pl-10 pr-4 py-2 bg-slate-800 border border-slate-700 rounded-lg text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      {/* Labels table */}
      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="text-center py-12 text-slate-400">{t('common:status.loading')}</div>
          ) : (
            <table className="w-full">
              <thead className="bg-slate-700/50">
                <tr>
                  <th className="px-4 py-3 text-left">
                    <input
                      type="checkbox"
                      checked={selectedLabels.size === filteredLabels.length && filteredLabels.length > 0}
                      onChange={toggleSelectAll}
                      className="rounded bg-slate-700 border-slate-600"
                    />
                  </th>
                  <th
                    className="px-4 py-3 text-left text-sm font-medium text-slate-300 cursor-pointer hover:text-white"
                    onClick={() => toggleSort('name')}
                  >
                    <div className="flex items-center space-x-1">
                      <span>{t('pages:labels.name')}</span>
                      <SortIcon field="name" />
                    </div>
                  </th>
                  <th
                    className="px-4 py-3 text-left text-sm font-medium text-slate-300 cursor-pointer hover:text-white"
                    onClick={() => toggleSort('photo_count')}
                  >
                    <div className="flex items-center space-x-1">
                      <span>{t('pages:labels.photos')}</span>
                      <SortIcon field="photo_count" />
                    </div>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-700">
                {filteredLabels.map((label) => (
                  <tr
                    key={label.uid}
                    className={`hover:bg-slate-700/50 ${
                      selectedLabels.has(label.uid) ? 'bg-blue-500/10' : ''
                    }`}
                  >
                    <td className="px-4 py-3">
                      <input
                        type="checkbox"
                        checked={selectedLabels.has(label.uid)}
                        onChange={() => toggleLabel(label.uid)}
                        className="rounded bg-slate-700 border-slate-600"
                      />
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center space-x-2">
                        <Tags className="h-4 w-4 text-slate-500" />
                        <Link
                          to={`/labels/${label.uid}`}
                          className="text-white hover:text-blue-400 transition-colors"
                        >
                          {label.name}
                        </Link>
                      </div>
                    </td>
                    <td className="px-4 py-3 text-slate-400">
                      {label.photo_count}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {!isLoading && filteredLabels.length === 0 && (
            <div className="text-center py-12 text-slate-400">
              {t('pages:labels.noLabelsFound')}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
