import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';

export interface SortOption {
  value: string;
  label: string;
}

export const SORT_OPTIONS: SortOption[] = [
  { value: 'newest', label: 'Date (Newest)' },
  { value: 'oldest', label: 'Date (Oldest)' },
  { value: 'added', label: 'Recently Added' },
  { value: 'edited', label: 'Recently Edited' },
  { value: 'name', label: 'File Name' },
  { value: 'title', label: 'Title' },
];

export interface UsePhotosFiltersReturn {
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
  hasActiveFilters: boolean;
  clearFilters: () => void;
  filterKey: string;
}

function getFilterKey(params: URLSearchParams): string {
  return params.toString();
}

export function usePhotosFilters(): UsePhotosFiltersReturn {
  const [searchParams, setSearchParams] = useSearchParams();

  // Filter states - initialized from URL params
  const [search, setSearch] = useState(() => searchParams.get('q') || '');
  const [selectedYear, setSelectedYear] = useState<number | ''>(() => {
    const year = searchParams.get('year');
    return year ? parseInt(year) : '';
  });
  const [selectedLabel, setSelectedLabel] = useState(() => searchParams.get('label') || '');
  const [selectedAlbum, setSelectedAlbum] = useState(() => searchParams.get('album') || '');
  const [sortBy, setSortBy] = useState(() => searchParams.get('sort') || 'newest');

  // Sync filter state to URL params
  useEffect(() => {
    const params = new URLSearchParams();
    if (search) params.set('q', search);
    if (selectedYear) params.set('year', selectedYear.toString());
    if (selectedLabel) params.set('label', selectedLabel);
    if (selectedAlbum) params.set('album', selectedAlbum);
    if (sortBy && sortBy !== 'newest') params.set('sort', sortBy);
    setSearchParams(params, { replace: true });
  }, [search, selectedYear, selectedLabel, selectedAlbum, sortBy, setSearchParams]);

  const hasActiveFilters = !!(search || selectedYear || selectedLabel || selectedAlbum);

  const clearFilters = () => {
    setSearch('');
    setSelectedYear('');
    setSelectedLabel('');
    setSelectedAlbum('');
    setSortBy('newest');
  };

  const filterKey = getFilterKey(searchParams);

  return {
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
    hasActiveFilters,
    clearFilters,
    filterKey,
  };
}

// Generate year options from current year to 1900
export function getYearOptions(): number[] {
  const currentYear = new Date().getFullYear();
  const years: number[] = [];
  for (let year = currentYear; year >= 1900; year--) {
    years.push(year);
  }
  return years;
}
