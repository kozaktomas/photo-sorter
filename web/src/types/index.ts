// API Response types

// Re-export event types
export * from './events';

export interface Album {
  uid: string;
  title: string;
  description: string;
  photo_count: number;
  thumb: string;
  type: string;
  favorite: boolean;
  created_at: string;
  updated_at: string;
}

export interface Photo {
  uid: string;
  title: string;
  description: string;
  taken_at: string;
  year: number;
  month: number;
  day: number;
  hash: string;
  width: number;
  height: number;
  lat: number;
  lng: number;
  country: string;
  favorite: boolean;
  private: boolean;
  type: string;
  original_name: string;
  file_name: string;
}

export interface Label {
  uid: string;
  name: string;
  slug: string;
  description: string;
  notes: string;
  photo_count: number;
  favorite: boolean;
  priority: number;
  created_at: string;
}

export interface SortJob {
  id: string;
  album_uid: string;
  album_title: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  progress: number;
  total_photos: number;
  processed_photos: number;
  error?: string;
  started_at: string;
  completed_at?: string;
  options: SortJobOptions;
  result?: SortJobResult;
}

export interface SortJobOptions {
  dry_run: boolean;
  limit: number;
  individual_dates: boolean;
  batch_mode: boolean;
  provider: string;
  force_date: boolean;
  concurrency: number;
}

export interface SortJobResult {
  processed_count: number;
  sorted_count: number;
  album_date?: string;
  date_reasoning?: string;
  errors?: string[];
  suggestions?: SortSuggestion[];
  usage?: UsageInfo;
}

export interface SortSuggestion {
  PhotoUID: string;
  Labels: LabelSuggestion[];
  Description: string;
  EstimatedDate: string;
}

export interface LabelSuggestion {
  name: string;
  confidence: number;
}

export interface UsageInfo {
  input_tokens: number;
  output_tokens: number;
  total_cost: number;
}

export interface ProviderInfo {
  name: string;
  available: boolean;
}

export interface Config {
  providers: ProviderInfo[];
  photoprism_domain?: string;
  embeddings_writable?: boolean;
}

export interface AuthStatus {
  authenticated: boolean;
  expires_at?: string;
}

export interface LoginResponse {
  success: boolean;
  session_id?: string;
  expires_at?: string;
  error?: string;
}

export interface JobEvent {
  type: string;
  message?: string;
  data?: unknown;
}

// Face matching types
export interface Subject {
  uid: string;
  name: string;
  slug: string;
  thumb: string;
  photo_count: number;
  favorite: boolean;
  about?: string;
  alias?: string;
  bio?: string;
  notes?: string;
  hidden?: boolean;
  private?: boolean;
  excluded?: boolean;
  created_at?: string;
  updated_at?: string;
}

export type MatchAction = 'create_marker' | 'assign_person' | 'already_done' | 'unassign_person';

export interface FaceMatch {
  photo_uid: string;
  distance: number;
  face_index: number;
  bbox: number[];
  bbox_rel?: number[];
  file_uid?: string;
  action: MatchAction;
  marker_uid?: string;
  marker_name?: string;
  iou?: number;
}

export interface MatchSummary {
  create_marker: number;
  assign_person: number;
  already_done: number;
}

export interface FaceMatchResult {
  person: string;
  source_photos: number;
  source_faces: number;
  matches: FaceMatch[];
  summary: MatchSummary;
}

export interface ApplyFaceMatchResponse {
  success: boolean;
  marker_uid?: string;
  error?: string;
}

// Photo faces (reverse matching) types
export interface PhotoFacesResponse {
  photo_uid: string;
  file_uid: string;
  width: number;
  height: number;
  orientation: number;
  embeddings_count: number;
  markers_count: number;
  faces: PhotoFace[];
}

export interface PhotoFace {
  face_index: number;
  bbox: number[];
  bbox_rel: number[];
  det_score: number;
  marker_uid?: string;
  marker_name?: string;
  action: MatchAction;
  suggestions: FaceSuggestion[];
}

export interface FaceSuggestion {
  person_name: string;
  person_uid: string;
  distance: number;
  confidence: number;
  photo_count: number;
}

// Similar photos types
export interface SimilarPhotoResult {
  photo_uid: string;
  distance: number;
  similarity: number;
}

export interface SimilarPhotosResponse {
  source_photo_uid: string;
  threshold: number;
  results: SimilarPhotoResult[];
  count: number;
}

// Collection similar photos types
export interface CollectionSimilarResult {
  photo_uid: string;
  distance: number;
  similarity: number;
  match_count: number;
}

export interface CollectionSimilarResponse {
  source_type: string;
  source_id: string;
  source_photo_count: number;
  source_embedding_count: number;
  min_match_count: number;
  threshold: number;
  results: CollectionSimilarResult[];
  count: number;
}

// Text search types
export interface TextSearchResponse {
  query: string;
  translated_query?: string;
  translate_cost_usd?: number;
  threshold: number;
  results: SimilarPhotoResult[];
  count: number;
}

// Compute faces response
export interface ComputeFacesResponse {
  photo_uid: string;
  faces_count: number;
  success: boolean;
  error?: string;
}

// Stats response
export interface StatsResponse {
  total_photos: number;
  photos_processed: number;
  photos_with_embeddings: number;
  photos_with_faces: number;
  total_faces: number;
  total_embeddings: number;
}

// Face outlier detection types
export interface OutlierResult {
  photo_uid: string;
  dist_from_centroid: number;
  face_index: number;
  bbox_rel?: number[];
  file_uid?: string;
  marker_uid?: string;
}

export interface OutlierResponse {
  person: string;
  total_faces: number;
  avg_distance: number;
  outliers: OutlierResult[];
  missing_embeddings: OutlierResult[];
}

// Process job types
export interface ProcessJob {
  id: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  total_photos: number;
  processed_photos: number;
  skipped_photos: number;
  error?: string;
  started_at: string;
  completed_at?: string;
  options: ProcessJobOptions;
  result?: ProcessJobResult;
}

export interface ProcessJobOptions {
  concurrency: number;
  limit: number;
  no_faces: boolean;
  no_embeddings: boolean;
}

export interface ProcessJobResult {
  embed_success: number;
  embed_error: number;
  face_success: number;
  face_error: number;
  total_new_faces: number;
  total_embeddings: number;
  total_faces: number;
  total_face_photos: number;
}

// Rebuild index response
export interface RebuildIndexResponse {
  success: boolean;
  face_count: number;
  embedding_count: number;
  face_index_path: string;
  embedding_index_path: string;
  duration_ms: number;
}

// Sync cache response
export interface SyncCacheResponse {
  success: boolean;
  photos_scanned: number;
  faces_updated: number;
  photos_deleted: number;
  duration_ms: number;
  error?: string;
}
