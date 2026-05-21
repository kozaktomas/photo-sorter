import { useState, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

export interface SortOption {
  value: string;
  label: string;
}

export const SORT_OPTIONS: SortOption[] = [
  { value: 'newest', label: 'photos.sortNewest' },
  { value: 'oldest', label: 'photos.sortOldest' },
  { value: 'added', label: 'photos.sortAdded' },
  { value: 'edited', label: 'photos.sortEdited' },
  { value: 'name', label: 'photos.sortFileName' },
  { value: 'title', label: 'photos.sortTitle' },
];

export interface BBox {
  minLat: number;
  minLng: number;
  maxLat: number;
  maxLng: number;
}

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
  // Date range (YYYY-MM-DD strings; empty = unbounded).
  takenFrom: string;
  setTakenFrom: (value: string) => void;
  takenTo: string;
  setTakenTo: (value: string) => void;
  // Raw bbox text input ("lat1,lng1,lat2,lng2"); empty = no filter.
  bboxInput: string;
  setBboxInput: (value: string) => void;
  // Parsed bbox (null if input is empty); undefined if input is non-empty but invalid.
  bbox: BBox | null | undefined;
  bboxError: boolean;
  hasActiveFilters: boolean;
  clearFilters: () => void;
  filterKey: string;
}

function getFilterKey(params: URLSearchParams): string {
  return params.toString();
}

// parseBBoxInput parses a "lat1,lng1,lat2,lng2" string. Returns null for empty
// input, undefined for non-empty but invalid input, or the parsed bbox.
function parseBBoxInput(raw: string): BBox | null | undefined {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const parts = trimmed.split(',').map(s => s.trim());
  if (parts.length !== 4) return undefined;
  const nums = parts.map(p => Number(p));
  if (nums.some(n => !Number.isFinite(n))) return undefined;
  const [a, b, c, d] = nums;
  return { minLat: Math.min(a, c), minLng: Math.min(b, d), maxLat: Math.max(a, c), maxLng: Math.max(b, d) };
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
  const [takenFrom, setTakenFrom] = useState(() => searchParams.get('taken_from') || '');
  const [takenTo, setTakenTo] = useState(() => searchParams.get('taken_to') || '');
  const [bboxInput, setBboxInput] = useState(() => searchParams.get('bbox') || '');

  const bbox = useMemo(() => parseBBoxInput(bboxInput), [bboxInput]);
  const bboxError = bboxInput.trim() !== '' && bbox === undefined;

  // Sync filter state to URL params
  useEffect(() => {
    const params = new URLSearchParams();
    if (search) params.set('q', search);
    if (selectedYear) params.set('year', selectedYear.toString());
    if (selectedLabel) params.set('label', selectedLabel);
    if (selectedAlbum) params.set('album', selectedAlbum);
    if (sortBy && sortBy !== 'newest') params.set('sort', sortBy);
    if (takenFrom) params.set('taken_from', takenFrom);
    if (takenTo) params.set('taken_to', takenTo);
    if (bboxInput.trim()) params.set('bbox', bboxInput.trim());
    setSearchParams(params, { replace: true });
  }, [search, selectedYear, selectedLabel, selectedAlbum, sortBy, takenFrom, takenTo, bboxInput, setSearchParams]);

  const hasActiveFilters = !!(
    search || selectedYear || selectedLabel || selectedAlbum || takenFrom || takenTo || bboxInput.trim()
  );

  const clearFilters = () => {
    setSearch('');
    setSelectedYear('');
    setSelectedLabel('');
    setSelectedAlbum('');
    setSortBy('newest');
    setTakenFrom('');
    setTakenTo('');
    setBboxInput('');
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
    takenFrom,
    setTakenFrom,
    takenTo,
    setTakenTo,
    bboxInput,
    setBboxInput,
    bbox,
    bboxError,
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
