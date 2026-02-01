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

// Re-export action constants for convenience
export * from './actions';
