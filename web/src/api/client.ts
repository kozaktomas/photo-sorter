import type {
  Album,
  Photo,
  Label,
  SortJob,
  Config,
  AuthStatus,
  LoginResponse,
  Subject,
  FaceMatchResult,
  ApplyFaceMatchResponse,
  MatchAction,
  PhotoFacesResponse,
  SimilarPhotosResponse,
  CollectionSimilarResponse,
  ComputeFacesResponse,
  StatsResponse,
  OutlierResponse,
  TextSearchResponse,
  RebuildIndexResponse,
  SyncCacheResponse,
  EraEstimateResponse,
  DuplicatesResponse,
  SuggestAlbumsResponse,
} from '../types';

const API_BASE = '/api/v1';

// Event for 401 responses to trigger re-authentication
export const AUTH_ERROR_EVENT = 'auth-error';

class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const url = `${API_BASE}${endpoint}`;
  const response = await fetch(url, {
    ...options,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));

    // Emit event on 401 to trigger re-authentication (except for auth endpoints)
    if (response.status === 401 && !endpoint.startsWith('/auth/')) {
      window.dispatchEvent(new CustomEvent(AUTH_ERROR_EVENT));
    }

    throw new ApiError(
      response.status,
      errorData.error || `Request failed with status ${response.status}`
    );
  }

  // Handle empty responses
  const text = await response.text();
  if (!text) return {} as T;
  return JSON.parse(text);
}

// Auth
export async function login(
  username: string,
  password: string
): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

export async function logout(): Promise<void> {
  await request('/auth/logout', { method: 'POST' });
}

export async function getAuthStatus(): Promise<AuthStatus> {
  return request<AuthStatus>('/auth/status');
}

// Albums
export async function getAlbums(params?: {
  count?: number;
  offset?: number;
  order?: string;
  q?: string;
}): Promise<Album[]> {
  const searchParams = new URLSearchParams();
  if (params?.count) searchParams.set('count', params.count.toString());
  if (params?.offset) searchParams.set('offset', params.offset.toString());
  if (params?.order) searchParams.set('order', params.order);
  if (params?.q) searchParams.set('q', params.q);
  const query = searchParams.toString();
  return request<Album[]>(`/albums${query ? `?${query}` : ''}`);
}

export async function getAlbum(uid: string): Promise<Album> {
  return request<Album>(`/albums/${uid}`);
}

export async function createAlbum(title: string): Promise<Album> {
  return request<Album>('/albums', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
}

export async function getAlbumPhotos(
  uid: string,
  params?: { count?: number; offset?: number }
): Promise<Photo[]> {
  const searchParams = new URLSearchParams();
  if (params?.count) searchParams.set('count', params.count.toString());
  if (params?.offset) searchParams.set('offset', params.offset.toString());
  const query = searchParams.toString();
  return request<Photo[]>(`/albums/${uid}/photos${query ? `?${query}` : ''}`);
}

export async function clearAlbumPhotos(
  uid: string
): Promise<{ removed: number }> {
  return request<{ removed: number }>(`/albums/${uid}/photos`, {
    method: 'DELETE',
  });
}

export async function addPhotosToAlbum(
  albumUid: string,
  photoUids: string[]
): Promise<{ added: number }> {
  return request<{ added: number }>(`/albums/${albumUid}/photos`, {
    method: 'POST',
    body: JSON.stringify({ photo_uids: photoUids }),
  });
}

// Labels
export async function getLabels(params?: {
  count?: number;
  offset?: number;
  all?: boolean;
}): Promise<Label[]> {
  const searchParams = new URLSearchParams();
  if (params?.count) searchParams.set('count', params.count.toString());
  if (params?.offset) searchParams.set('offset', params.offset.toString());
  if (params?.all) searchParams.set('all', 'true');
  const query = searchParams.toString();
  return request<Label[]>(`/labels${query ? `?${query}` : ''}`);
}

export async function getLabel(uid: string): Promise<Label> {
  return request<Label>(`/labels/${uid}`);
}

export async function updateLabel(
  uid: string,
  updates: { name?: string; description?: string; notes?: string; priority?: number; favorite?: boolean }
): Promise<Label> {
  return request<Label>(`/labels/${uid}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  });
}

export async function deleteLabels(
  uids: string[]
): Promise<{ deleted: number }> {
  return request<{ deleted: number }>('/labels', {
    method: 'DELETE',
    body: JSON.stringify({ uids }),
  });
}

// Photos
export async function getPhotos(params?: {
  count?: number;
  offset?: number;
  order?: string;
  q?: string;
  year?: number;
  label?: string;
  album?: string;
}): Promise<Photo[]> {
  const searchParams = new URLSearchParams();
  if (params?.count) searchParams.set('count', params.count.toString());
  if (params?.offset) searchParams.set('offset', params.offset.toString());
  if (params?.order) searchParams.set('order', params.order);
  if (params?.q) searchParams.set('q', params.q);
  if (params?.year) searchParams.set('year', params.year.toString());
  if (params?.label) searchParams.set('label', params.label);
  if (params?.album) searchParams.set('album', params.album);
  const query = searchParams.toString();
  return request<Photo[]>(`/photos${query ? `?${query}` : ''}`);
}

export async function getPhoto(uid: string): Promise<Photo> {
  return request<Photo>(`/photos/${uid}`);
}

export async function updatePhoto(
  uid: string,
  updates: Partial<Photo>
): Promise<Photo> {
  return request<Photo>(`/photos/${uid}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  });
}

export async function batchAddLabels(
  photoUids: string[],
  label: string
): Promise<{ updated: number; errors?: string[] }> {
  return request<{ updated: number; errors?: string[] }>('/photos/batch/labels', {
    method: 'POST',
    body: JSON.stringify({ photo_uids: photoUids, label }),
  });
}

export function getThumbnailUrl(uid: string, size: string): string {
  return `${API_BASE}/photos/${uid}/thumb/${size}`;
}

// Sort
export async function startSort(params: {
  album_uid: string;
  dry_run?: boolean;
  limit?: number;
  individual_dates?: boolean;
  batch_mode?: boolean;
  provider?: string;
  force_date?: boolean;
  concurrency?: number;
}): Promise<{ job_id: string; album_uid: string; album_title: string }> {
  return request('/sort', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

export async function getSortJobStatus(jobId: string): Promise<SortJob> {
  return request<SortJob>(`/sort/${jobId}`);
}

export async function cancelSortJob(
  jobId: string
): Promise<{ cancelled: boolean }> {
  return request<{ cancelled: boolean }>(`/sort/${jobId}`, {
    method: 'DELETE',
  });
}

// Upload
export async function uploadPhotos(
  albumUid: string,
  files: FileList
): Promise<{ uploaded: number; album: string }> {
  const formData = new FormData();
  formData.append('album_uid', albumUid);
  for (let i = 0; i < files.length; i++) {
    formData.append('files', files[i]);
  }

  const response = await fetch(`${API_BASE}/upload`, {
    method: 'POST',
    body: formData,
    credentials: 'include',
  });

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    throw new ApiError(
      response.status,
      errorData.error || 'Upload failed'
    );
  }

  return response.json();
}

// Config
export async function getConfig(): Promise<Config> {
  return request<Config>('/config');
}

// Health
export async function getHealth(): Promise<{ status: string }> {
  return request<{ status: string }>('/health');
}

// Faces
export async function getSubjects(params?: {
  count?: number;
  offset?: number;
}): Promise<Subject[]> {
  const searchParams = new URLSearchParams();
  if (params?.count) searchParams.set('count', params.count.toString());
  if (params?.offset) searchParams.set('offset', params.offset.toString());
  const query = searchParams.toString();
  return request<Subject[]>(`/subjects${query ? `?${query}` : ''}`);
}

export async function getSubject(uid: string): Promise<Subject> {
  return request<Subject>(`/subjects/${uid}`);
}

export async function updateSubject(
  uid: string,
  updates: { name?: string; about?: string; alias?: string; bio?: string; notes?: string; favorite?: boolean; hidden?: boolean; private?: boolean; excluded?: boolean }
): Promise<Subject> {
  return request<Subject>(`/subjects/${uid}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  });
}

export async function matchFaces(params: {
  person_name: string;
  threshold?: number;
  limit?: number;
}): Promise<FaceMatchResult> {
  return request<FaceMatchResult>('/faces/match', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

export async function applyFaceMatch(params: {
  photo_uid: string;
  person_name: string;
  action: MatchAction;
  marker_uid?: string;
  file_uid?: string;
  bbox_rel?: number[];
  face_index?: number;
}): Promise<ApplyFaceMatchResponse> {
  return request<ApplyFaceMatchResponse>('/faces/apply', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Photo faces (reverse matching)
export async function getPhotoFaces(
  uid: string,
  params?: { threshold?: number; limit?: number }
): Promise<PhotoFacesResponse> {
  const searchParams = new URLSearchParams();
  if (params?.threshold) searchParams.set('threshold', params.threshold.toString());
  if (params?.limit) searchParams.set('limit', params.limit.toString());
  const query = searchParams.toString();
  return request<PhotoFacesResponse>(`/photos/${uid}/faces${query ? `?${query}` : ''}`);
}

// Similar photos
export async function findSimilarPhotos(params: {
  photo_uid: string;
  threshold?: number;
  limit?: number;
}): Promise<SimilarPhotosResponse> {
  return request<SimilarPhotosResponse>('/photos/similar', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Find similar photos to a collection (label or album)
export async function findSimilarToCollection(params: {
  source_type: 'label' | 'album';
  source_id: string;
  threshold?: number;
  limit?: number;
}): Promise<CollectionSimilarResponse> {
  return request<CollectionSimilarResponse>('/photos/similar/collection', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Text-to-image search
export async function searchByText(params: {
  text: string;
  threshold?: number;
  limit?: number;
}): Promise<TextSearchResponse> {
  return request<TextSearchResponse>('/photos/search-by-text', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Compute faces for a photo
export async function computeFaces(uid: string): Promise<ComputeFacesResponse> {
  return request<ComputeFacesResponse>(`/photos/${uid}/faces/compute`, {
    method: 'POST',
  });
}

// Stats
export async function getStats(): Promise<StatsResponse> {
  return request<StatsResponse>('/stats');
}

// Face outliers
export async function findFaceOutliers(params: {
  person_name: string;
  threshold?: number;
  limit?: number;
}): Promise<OutlierResponse> {
  return request<OutlierResponse>('/faces/outliers', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Process (embeddings & face detection)
export async function startProcess(params: {
  concurrency?: number;
  limit?: number;
  no_faces?: boolean;
  no_embeddings?: boolean;
}): Promise<{ job_id: string }> {
  return request('/process', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

export async function cancelProcessJob(
  jobId: string
): Promise<{ cancelled: boolean }> {
  return request<{ cancelled: boolean }>(`/process/${jobId}`, {
    method: 'DELETE',
  });
}

// Rebuild HNSW index
export async function rebuildIndex(): Promise<RebuildIndexResponse> {
  return request<RebuildIndexResponse>('/process/rebuild-index', {
    method: 'POST',
  });
}

// Sync face cache from PhotoPrism
export async function syncCache(): Promise<SyncCacheResponse> {
  return request<SyncCacheResponse>('/process/sync-cache', {
    method: 'POST',
  });
}

// Era estimation
export async function estimateEra(photoUID: string): Promise<EraEstimateResponse> {
  return request<EraEstimateResponse>(`/photos/${photoUID}/estimate-era`);
}

// Batch edit photos (favorite, private)
export async function batchEditPhotos(
  photoUids: string[],
  updates: { favorite?: boolean; private?: boolean }
): Promise<{ updated: number; errors?: string[] }> {
  return request<{ updated: number; errors?: string[] }>('/photos/batch/edit', {
    method: 'POST',
    body: JSON.stringify({ photo_uids: photoUids, ...updates }),
  });
}

// Remove specific photos from album
export async function removePhotosFromAlbum(
  albumUid: string,
  photoUids: string[]
): Promise<{ removed: number }> {
  return request<{ removed: number }>(`/albums/${albumUid}/photos/batch`, {
    method: 'DELETE',
    body: JSON.stringify({ photo_uids: photoUids }),
  });
}

// Find duplicate photos
export async function findDuplicates(params: {
  album_uid?: string;
  threshold?: number;
  limit?: number;
}): Promise<DuplicatesResponse> {
  return request<DuplicatesResponse>('/photos/duplicates', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

// Find photos missing from existing albums (album completion)
export async function suggestAlbums(params: {
  threshold?: number;
  top_k?: number;
}): Promise<SuggestAlbumsResponse> {
  return request<SuggestAlbumsResponse>('/photos/suggest-albums', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}
