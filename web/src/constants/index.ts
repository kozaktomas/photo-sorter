// Currency conversion
export const USD_TO_CZK = 23.5;

// API/processing defaults
export const DEFAULT_CONCURRENCY = 5;
export const MAX_SUBJECTS_FETCH = 500;
export const MAX_ALBUMS_FETCH = 500;
export const MAX_LABELS_FETCH = 1000;

// Recognition page
export const RECOGNITION_CONCURRENCY = 3;

// Photos page
export const PHOTOS_PER_PAGE = 100;
export const PHOTOS_CACHE_KEY = 'photos_page_cache';

// Album photo navigation
export const ALBUM_PHOTOS_CACHE_KEY = 'album_photos_cache';

// Label photo navigation
export const LABEL_PHOTOS_CACHE_KEY = 'label_photos_cache';

// Face matching defaults
export const DEFAULT_FACE_THRESHOLD = 50; // percentage
export const DEFAULT_RECOGNITION_CONFIDENCE = 75; // percentage

// Duplicate detection defaults
export const DEFAULT_DUPLICATE_THRESHOLD = 90; // percentage (maps to 0.10 cosine distance)
export const DEFAULT_DUPLICATE_LIMIT = 100;

// Album completion defaults
export const DEFAULT_SUGGEST_ALBUM_THRESHOLD = 70; // percentage (maps to 0.70 cosine similarity)
export const DEFAULT_SUGGEST_ALBUM_TOP_K = 20; // max photos suggested per album

// Threshold conversion: percentage (0-100) to cosine distance (1-0)
export function percentToDistance(percent: number): number {
  return 1 - percent / 100;
}

// Re-export action constants for convenience
export * from './actions';
