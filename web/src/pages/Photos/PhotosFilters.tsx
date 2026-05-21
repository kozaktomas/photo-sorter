import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Search, Calendar, Tag, SortAsc, ChevronDown, MapPin } from 'lucide-react';
import { Card, CardContent } from '../../components/Card';
import { Button } from '../../components/Button';
import { Combobox } from '../../components/Combobox';
import { SORT_OPTIONS, getYearOptions } from './hooks/usePhotosFilters';
import type { Label, Album } from '../../types';

interface PhotosFiltersProps {
  search: string;
  setSearch: (value: string) => void;
  selectedYear: number | '';
  setSelectedYear: (value: number | '') => void;
  selectedLabel: string;
  setSelectedLabel: (value: string) => void;
  selectedAlbum: string;
  setSelectedAlbum: (value: string) => void;
  sortBy: string;
  setSortBy: (value: string) => void;
  takenFrom: string;
  setTakenFrom: (value: string) => void;
  takenTo: string;
  setTakenTo: (value: string) => void;
  bboxInput: string;
  setBboxInput: (value: string) => void;
  bboxError: boolean;
  hasActiveFilters: boolean;
  clearFilters: () => void;
  labels: Label[];
  albums: Album[];
}

export function PhotosFilters({
  search,
  setSearch,
  selectedYear,
  setSelectedYear,
  selectedLabel,
  setSelectedLabel,
  selectedAlbum,
  setSelectedAlbum,
  sortBy,
  setSortBy,
  takenFrom,
  setTakenFrom,
  takenTo,
  setTakenTo,
  bboxInput,
  setBboxInput,
  bboxError,
  hasActiveFilters,
  clearFilters,
  labels,
  albums,
}: PhotosFiltersProps) {
  const { t } = useTranslation(['pages', 'common']);
  const yearOptions = getYearOptions();
  // Auto-expand if any of the "More filters" fields already has a value so
  // users see the active state on first render (e.g. after navigating to a
  // shared URL).
  const [moreOpen, setMoreOpen] = useState(() => !!(takenFrom || takenTo || bboxInput.trim()));

  return (
    <Card>
      <CardContent>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
          {/* Search */}
          <div className="relative lg:col-span-2">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-slate-400" />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('pages:photos.searchPlaceholder')}
              className="w-full pl-10 pr-4 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
            />
          </div>

          {/* Year filter */}
          <div className="relative">
            <Calendar className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-slate-400 pointer-events-none" />
            <select
              value={selectedYear}
              onChange={(e) => setSelectedYear(e.target.value ? parseInt(e.target.value) : '')}
              className="w-full pl-10 pr-8 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 appearance-none cursor-pointer"
            >
              <option value="">{t('pages:photos.allYears')}</option>
              {yearOptions.map(year => (
                <option key={year} value={year}>{year}</option>
              ))}
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 pointer-events-none" />
          </div>

          {/* Label filter */}
          <Combobox
            value={selectedLabel}
            onChange={setSelectedLabel}
            options={labels.map(l => ({ value: l.slug, label: `${l.name} (${l.photo_count})` }))}
            placeholder={t('pages:photos.allLabels')}
            icon={Tag}
          />

          {/* Sort */}
          <div className="relative">
            <SortAsc className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-slate-400 pointer-events-none" />
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value)}
              className="w-full pl-10 pr-8 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 appearance-none cursor-pointer"
            >
              {SORT_OPTIONS.map(option => (
                <option key={option.value} value={option.value}>
                  {t(option.label)}
                </option>
              ))}
            </select>
            <ChevronDown className="absolute right-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 pointer-events-none" />
          </div>
        </div>

        {/* Album filter - separate row for better UX */}
        <div className="mt-4 flex items-center gap-4">
          <Combobox
            value={selectedAlbum}
            onChange={setSelectedAlbum}
            options={albums.map(a => ({ value: a.uid, label: `${a.title} (${a.photo_count})` }))}
            placeholder={t('pages:photos.allAlbums')}
            className="flex-1 max-w-xs"
          />

          {hasActiveFilters && (
            <Button variant="ghost" onClick={clearFilters} className="text-sm">
              {t('common:buttons.clearFilters')}
            </Button>
          )}
        </div>

        {/* More filters - collapsible section with date range + GPS bbox */}
        <div className="mt-4 border-t border-slate-700 pt-4">
          <button
            type="button"
            onClick={() => setMoreOpen(o => !o)}
            className="flex items-center gap-2 text-sm text-slate-300 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded"
            aria-expanded={moreOpen}
            aria-controls="photos-more-filters"
          >
            <ChevronDown
              className={`h-4 w-4 transition-transform ${moreOpen ? 'rotate-0' : '-rotate-90'}`}
            />
            {t('pages:photos.filters.more')}
          </button>

          {moreOpen && (
            <div id="photos-more-filters" className="mt-4 grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* Date range */}
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="photos-filter-taken-from">
                    {t('pages:photos.filters.date_from')}
                  </label>
                  <input
                    id="photos-filter-taken-from"
                    type="date"
                    value={takenFrom}
                    onChange={(e) => setTakenFrom(e.target.value)}
                    className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="photos-filter-taken-to">
                    {t('pages:photos.filters.date_to')}
                  </label>
                  <input
                    id="photos-filter-taken-to"
                    type="date"
                    value={takenTo}
                    onChange={(e) => setTakenTo(e.target.value)}
                    className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                  />
                </div>
              </div>

              {/* GPS bbox */}
              <div>
                <label className="block text-xs font-medium text-slate-400 mb-1" htmlFor="photos-filter-bbox">
                  {t('pages:photos.filters.bbox')}
                </label>
                <div className="relative">
                  <MapPin className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 pointer-events-none" />
                  <input
                    id="photos-filter-bbox"
                    type="text"
                    value={bboxInput}
                    onChange={(e) => setBboxInput(e.target.value)}
                    placeholder="lat1,lng1,lat2,lng2"
                    aria-invalid={bboxError}
                    className={`w-full pl-9 pr-3 py-2 bg-slate-800 border rounded-lg text-white placeholder-slate-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ${
                      bboxError ? 'border-red-500' : 'border-slate-600'
                    }`}
                  />
                </div>
                <p className={`mt-1 text-xs ${bboxError ? 'text-red-400' : 'text-slate-500'}`}>
                  {bboxError ? t('pages:photos.filters.bbox_invalid') : t('pages:photos.filters.bbox_help')}
                </p>
              </div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
