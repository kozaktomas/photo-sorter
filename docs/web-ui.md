# Web UI Guide

The Photo Sorter Web UI provides browser-based access to all features with a modern, responsive interface.

## Starting the Server

```bash
# Production (embedded frontend)
photo-sorter serve

# Custom port
photo-sorter serve --port 3000
```

For development with hot reload:
```bash
# Terminal 1: Frontend dev server
make dev-web

# Terminal 2: Go backend
make dev-go
```

## Authentication

The Web UI uses your PhotoPrism credentials for authentication. Enter your PhotoPrism username and password on the login page.

## Language Support

The Web UI supports English and Czech languages with full internationalization.

**Language Switching:**
- Click the language switcher button in the header (shows 🇨🇿 CZ or 🇬🇧 EN)
- Language preference is saved to localStorage and persists across sessions
- Falls back to Czech if browser language is not detected

**Supported Languages:**
- **Czech (cs)** - Default language, complete translations
- **English (en)** - Full English translations

**Translation Coverage:**
- Navigation labels
- Page titles and subtitles
- Form labels and placeholders
- Buttons and status messages
- Error messages
- Pluralized counts (photos, albums, labels, faces, etc.)

Czech uses proper plural forms (one/few/many) for natural language display.

## Navigation

The header navigation groups items to reduce clutter:

- **Primary** (always visible): Dashboard, Albums, Photos, Labels
- **AI** dropdown: Analyze, Text Search
- **Faces** dropdown: Faces, Recognition, Outliers
- **Tools** dropdown: Similar, Expand, Process

Dropdown buttons highlight when one of their child pages is active. Dropdowns close when clicking outside.

## Pages

### Dashboard

The home page displays:

- **Stats Cards** - Quick overview of albums, labels, processed photo count, face embeddings, and waiting (unprocessed) photos
- **Quick Actions** - Links to common tasks
- **AI Provider Status** - Shows which AI providers are configured and available

### Albums

Browse and manage your PhotoPrism albums.

**Features:**
- View all albums with photo counts
- Search albums by name
- Click an album to view its photos
- Quick access to analyze an album with AI
- **Photo navigation context** - When clicking a photo from an album, navigation arrows and position counter are available in Photo Detail

### Photos

Browse all photos in your library with powerful filtering.

**Quality Filtering:** The Photos page only shows photos with quality score >= 3, matching PhotoPrism's default behavior. Photos with lower quality (e.g., missing EXIF metadata) appear in PhotoPrism's Review section and are hidden here. Album views show all photos regardless of quality.

**Deleted Photo Filtering:** Soft-deleted (archived) photos are automatically filtered out from the listing. PhotoPrism's API may return photos with a non-empty `DeletedAt` field; these are excluded before sending the response to the frontend.

**Features:**
- **Search** - Full-text search across photo metadata
- **Filter by Year** - Dropdown to filter by year
- **Filter by Label** - Select a label to show only photos with that label
- **Filter by Album** - Show photos from a specific album
- **Sort Options** - Date (newest/oldest), recently added, recently edited, name, title
- **Photo Detail Modal** - Click any photo to see full details
  - Photo metadata (date, camera, location)
  - Applied labels
  - Quick link to find similar photos

### Photo Detail (`/photos/:uid`)

Detailed view of a single photo with face management capabilities.

**Features:**
- Full-resolution photo display with interactive face bounding boxes
- Photo metadata (title, date, dimensions)
- Quick actions: Copy UID, Open in PhotoPrism, Find Similar, Load Faces
- **Embeddings status banner** - Automatically checks if embeddings have been calculated for the photo on page load. Shows a yellow warning banner with a "Calculate Embeddings" button if not yet processed
- **Face detection and assignment** - Load faces to see detected faces with bounding boxes, assign people via suggestions or manual input

**Photo Navigation:**
When accessing a photo from an album or label, navigation controls are available:
- **Left/Right arrows** - Semi-transparent navigation buttons appear on hover over the photo
- **Position counter** - Shows current position (e.g., "22 / 41") at the bottom center on hover
- **Keyboard navigation** - Use ← and → arrow keys to navigate between photos
- URL preserves context via query parameter (`?album=xyz` or `?label=slug`)
- Photo list is cached in sessionStorage for fast navigation without extra API calls
- Direct URL access (e.g., sharing a link) fetches the album/label photos from API automatically

**Embedding Status:**
- On page load, the faces API is queried to check if embeddings exist
- If `embeddings_count` is 0 or the API returns an error, a banner is shown: "Embeddings not calculated for this photo"
- Clicking "Calculate Embeddings" triggers face detection and embedding computation via `POST /api/v1/photos/:uid/faces/compute`
- The banner disappears once embeddings are successfully computed

**Era Estimation:**
- Automatically displayed in the right sidebar when the photo has a CLIP image embedding
- Shows the best-matching era (e.g., "2015-2019") with a confidence percentage
- Click the chevron to expand and see all 16 eras ranked by similarity with proportional bars
- Computation: the photo's 768-dim CLIP image embedding is compared via cosine similarity against pre-computed era text embedding centroids (see `cache compute-eras` command)
- Returns silently if the photo has no embedding or era centroids haven't been computed

**Face Assignment:**
- Click "Faces" to load face data with bounding boxes overlaid on the photo
- Select a face to see AI-powered person suggestions with confidence scores
- Accept a suggestion or manually type a person name (with autocomplete)
- Color-coded bounding boxes indicate assignment status (red=unassigned, yellow=needs assignment, green=assigned, orange=outlier)

### Labels

Manage labels in your PhotoPrism library.

**Features:**
- View all labels with photo counts
- Sort by name or count
- Click a label name to view its detail page
- Multi-select labels for batch operations
- **Delete Labels** - Remove unwanted labels (with confirmation)

### Label Detail (`/labels/:uid`)

View and edit a single label.

**Features:**
- **Rename** - Click pencil icon to edit the label name inline
- **Details** - Shows slug, description, notes, priority, favorite status, photo count, created date
- **Photo Grid** - Thumbnails of all photos with this label (up to 60)
- **Photo navigation context** - When clicking a photo, navigation arrows and position counter are available in Photo Detail

### Subject Detail (`/subjects/:uid`)

View and edit a single person/subject.

**Features:**
- **Rename** - Click pencil icon to edit the person name inline
- **Thumbnail** - Subject's face thumbnail from PhotoPrism
- **Details** - Shows slug, about, alias, bio, notes, photo count, favorite/hidden/excluded status, created date
- **Photo Grid** - Thumbnails of all photos tagged with this person (up to 60)

### Analyze

The main AI analysis interface for sorting photos.

**Options:**
- **Album Selection** - Choose which album to analyze
- **AI Provider** - Select from configured providers (OpenAI, Gemini, Ollama, llama.cpp)
- **Dry Run** - Preview changes without applying them (recommended first)
- **Individual Dates** - Estimate date per photo instead of album-wide
- **Batch Mode** - Use batch API for 50% cost savings (slower)
- **Force Date** - Overwrite existing dates with AI estimates
- **Limit** - Process only N photos (useful for testing)
- **Concurrency** - Number of parallel API requests

**Progress Tracking:**
- Real-time progress via Server-Sent Events (SSE)
- Shows processed/total photos
- Displays cost estimation
- Cancel button for long-running jobs

**Results:**
- Summary of processed photos
- API usage and cost
- Per-photo details with labels and descriptions
- Clickable photo thumbnails

### Similar Photos

Find visually similar photos using image embeddings.

**Search Options:**
- **Photo UID** - Enter a photo UID to find similar photos
- **Threshold** - Maximum cosine distance (lower = more similar, default 0.3)
- **Limit** - Maximum results to return

**Features:**
- Visual grid of similar photos with similarity scores
- Select multiple photos
- **Add to Album** - Add selected photos to an existing album
- **Add Label** - Apply a label to selected photos
- Click any result to find photos similar to that photo

### Expand

Find photos similar to an entire collection (label or album).

**Source Options:**
- **Label** - Find photos similar to all photos with a specific label
- **Album** - Find photos similar to all photos in an album

**Features:**
- Same selection and action capabilities as Similar Photos
- Useful for expanding collections based on visual similarity
- Great for finding uncategorized photos that belong in a label/album

### Process

Compute image embeddings and detect faces for unprocessed photos.

**Requirements:**
- `DATABASE_URL` (PostgreSQL) must be set
- Embedding server must be running (defaults to `EMBEDDING_URL` or `LLAMACPP_URL`)

**Options:**
- **Concurrency** - Number of parallel workers (default: 5)
- **Limit** - Process only N photos (0 = unlimited)
- **Skip face detection** - Only compute CLIP embeddings
- **Skip image embeddings** - Only detect faces

**Progress:**
- Real-time progress bar via SSE
- Shows skipped (already processed) count
- Periodic saves every 50 photos for crash recovery

**Results (on completion):**
- Embeddings: success/error counts, total in database
- Faces: photos processed, errors, new faces detected, totals

**Rebuild HNSW Index:**

After processing new photos or modifying data directly in the database, you can rebuild the HNSW similarity search indexes directly from the web UI. This section appears below the main processing configuration.

- **Description** - Explains when to rebuild
- **Rebuild Index** button - Rebuilds HNSW indexes for the active backend
- **Success message** - Shows faces indexed, embeddings indexed, and duration
- **Error handling** - Displays any errors that occur during rebuild

The rebuild operation works differently based on the storage backend:

**What it does:**
1. Reloads all face embeddings from PostgreSQL into memory
2. Rebuilds the in-memory HNSW index for O(log N) similarity search
3. Index is immediately available (no file I/O required)

Use this when you've modified face data directly in PostgreSQL outside of Photo Sorter.

**Sync Cache:**

Syncs face marker data from PhotoPrism to the local cache without recomputing embeddings. This is useful when faces have been assigned or unassigned directly in PhotoPrism's native UI, and you want the Photo Sorter cache to reflect those changes. Also cleans up orphaned data for photos that have been deleted or archived in PhotoPrism — detects both hard-deleted photos (404) and soft-deleted photos (with `DeletedAt` timestamp set).

- **Description** - Explains when to use sync
- **Sync Cache** button - Syncs marker data for all photos with faces/embeddings
- **Success message** - Shows photos scanned, faces updated, deleted photos cleaned up, and duration
- **Error handling** - Displays any errors that occur during sync

**What gets synced:**
| Field | Description |
|-------|-------------|
| `marker_uid` | PhotoPrism marker UID |
| `subject_uid` | Subject/person UID from marker |
| `subject_name` | Person name from marker |
| `photo_width`, `photo_height` | Photo dimensions |
| `orientation` | EXIF orientation (1-8) |
| `file_uid` | Primary file UID |

**When to use:**
- After assigning/unassigning faces in PhotoPrism's native UI
- After bulk face operations in PhotoPrism
- When face matches show incorrect "already_done" status

The sync operation processes all photos with faces in parallel (20 workers) and only updates faces where the cached data differs from PhotoPrism.

**API Endpoints:**
- `POST /api/v1/process` - Start processing job
- `GET /api/v1/process/{jobId}/events` - SSE event stream
- `DELETE /api/v1/process/{jobId}` - Cancel running job
- `POST /api/v1/process/rebuild-index` - Rebuild HNSW indexes
- `POST /api/v1/process/sync-cache` - Sync face marker data from PhotoPrism

Only one process job can run at a time. Changes are immediately available in the database.

### Faces

Find and match faces across your photo library.

**Search:**
- Select a person from the dropdown (people already tagged in PhotoPrism)
- Adjust threshold (lower = stricter matching)
- Set result limit

**Results:**
- Grid of matched faces with bounding boxes highlighted
- Distance score for each match
- Action status:
  - `create_marker` - No marker exists, needs creation
  - `assign_person` - Marker exists but person not assigned
  - `already_done` - Already correctly tagged

**Filter Tabs:**
- All matches
- Needs marker creation
- Needs person assignment
- Already done

**Actions:**
- **Accept All** - Apply all pending changes at once
- **Individual Accept** - Accept single matches one at a time

### Text Search

Find photos matching a text description using CLIP text-to-image embeddings.

**Search Options:**
- **Query** - Enter a text description of the image you're looking for
- **Threshold** - Minimum similarity percentage (lower = more results, higher = better matches)
- **Limit** - Maximum results to return

**Features:**
- Uses CLIP text embeddings searched against stored image embeddings
- Czech queries are automatically translated to CLIP-optimized English via GPT-4.1-mini (requires `OPENAI_TOKEN`)
- Translated query is displayed in results as "CLIP Query" with translation cost in Kč
- Falls back to raw text if translation is unavailable
- Visual grid of matching photos with similarity scores
- Select multiple photos
- **Add to Album** - Add selected photos to an existing album
- **Add Label** - Apply a label to selected photos (with autocomplete from existing labels)

### Outliers

Detect wrongly assigned faces by computing the centroid (average) embedding for a person's assigned faces, then ranking each face by distance from that centroid. Faces far from the centroid are likely misassignments.

**Configuration:**
- **Person** - Select a person from the dropdown
- **Min Distance** - Minimum cosine distance from centroid to display (0% = show all, higher = only extreme outliers)
- **Limit** - Maximum results to return (0 = no limit)

**Results:**
- Total faces analyzed for the person
- Average distance from centroid across all faces
- Number of outliers shown (after threshold filtering)
- Grid of photo cards sorted by distance (most suspicious first)
- Each card shows similarity percentage (lower = more suspicious)
- Bounding boxes highlight the detected face

### Recognition

Scans all known people for high-confidence face matches across the entire library. Results are grouped by person for quick bulk review and approval.

**Configuration:**
- **Min Confidence** - Slider from 70% to 95% (maps to cosine distance 0.3 to 0.05). Higher = fewer but more reliable matches
- **Scan All People** - Iterates through all people with photos, running face matching for each (3 concurrent requests for performance)
- **Stop** - Cancel an in-progress scan

**Progress:**
- Shows current/total people scanned with progress bar
- Displays the person currently being scanned
- Results stream in as each person completes (no need to wait for full scan)

**Results Summary:**
- **Actionable** - Total matches across all people that need approval
- **Already Done** - Matches already correctly assigned (hidden from grids)
- **People with Matches** - Number of people that have actionable matches

**Per-Person Sections:**
Each person with actionable matches gets their own card showing:
- Person name with match count
- **Accept All** - Bulk-approve all matches for that person
- Grid of face matches with bounding boxes and confidence scores

**Individual Actions:**
- **Accept** - Apply a single match (create marker or assign person)
- **Reject** - Remove from view without modifying PhotoPrism

**Empty State:**
When no actionable matches are found after scanning, displays "All matches already assigned".

## Keyboard Shortcuts

### Photo Detail Page
- `←` / `→` - Navigate to previous/next photo (when accessed from album or label)

### Photo Detail Modal
- `←` / `→` - Navigate between photos
- `Escape` - Close modal

## API Endpoints

The Web UI communicates with these backend endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/login` | Login with PhotoPrism credentials |
| GET | `/api/v1/auth/status` | Check authentication status |
| POST | `/api/v1/auth/logout` | Logout |
| GET | `/api/v1/albums` | List albums |
| GET | `/api/v1/albums/:uid/photos` | Get photos in album |
| GET | `/api/v1/photos` | List/search photos |
| GET | `/api/v1/photos/:uid` | Get single photo details |
| GET | `/api/v1/labels` | List labels |
| GET | `/api/v1/labels/:uid` | Get single label |
| PUT | `/api/v1/labels/:uid` | Update label (rename, etc.) |
| DELETE | `/api/v1/labels` | Batch delete labels |
| POST | `/api/v1/photos/batch/labels` | Add labels to photos |
| GET | `/api/v1/subjects` | List people/subjects |
| GET | `/api/v1/subjects/:uid` | Get single subject |
| PUT | `/api/v1/subjects/:uid` | Update subject (rename, etc.) |
| GET | `/api/v1/config` | Get available providers |
| GET | `/api/v1/stats` | Get processing statistics |
| POST | `/api/v1/sort` | Start AI sort job |
| GET | `/api/v1/sort/:jobId` | Get job status |
| GET | `/api/v1/sort/:jobId/events` | SSE stream for job progress |
| POST | `/api/v1/sort/:jobId/cancel` | Cancel running job |
| POST | `/api/v1/photos/similar` | Find similar photos |
| POST | `/api/v1/photos/similar/collection` | Find similar to label/album |
| POST | `/api/v1/faces/match` | Match faces for a person |
| POST | `/api/v1/faces/apply` | Apply face match result |
| POST | `/api/v1/faces/outliers` | Detect face outliers for a person |
| POST | `/api/v1/photos/search-by-text` | Text-to-image similarity search |
| GET | `/api/v1/photos/:uid/faces` | Get faces in a photo |
| POST | `/api/v1/photos/:uid/faces/compute` | Compute face embeddings for a photo |
| GET | `/api/v1/photos/:uid/estimate-era` | Estimate photo era from CLIP embeddings |
| POST | `/api/v1/albums/:uid/photos` | Add photos to album |
| POST | `/api/v1/process` | Start photo processing job |
| GET | `/api/v1/process/:jobId/events` | SSE stream for process job |
| DELETE | `/api/v1/process/:jobId` | Cancel process job |
| POST | `/api/v1/process/rebuild-index` | Rebuild HNSW indexes |
| POST | `/api/v1/process/sync-cache` | Sync face marker data from PhotoPrism |

## Frontend Architecture

The frontend is built with React + TypeScript + TailwindCSS and follows a modular architecture for maintainability.

### Directory Structure

```
web/src/
├── api/
│   └── client.ts           # Typed API client
├── components/             # Shared UI components
│   ├── Button.tsx
│   ├── Card.tsx
│   ├── ErrorBoundary.tsx   # Error catching wrapper
│   ├── LanguageSwitcher.tsx # Language toggle button
│   ├── LazyImage.tsx
│   ├── Layout.tsx
│   ├── LoadingState.tsx    # Unified loading/error/empty states
│   ├── PhotoCard.tsx
│   ├── PhotoGrid.tsx
│   └── PhotoWithBBox.tsx
├── constants/              # Shared constants
│   ├── actions.ts          # Face action styling
│   └── index.ts            # Magic numbers, defaults, cache keys
├── hooks/                  # Global hooks
│   ├── useAuth.tsx
│   ├── useFaceApproval.ts  # Face approval logic
│   ├── useSSE.ts           # Server-Sent Events
│   └── useSubjectsAndConfig.ts
├── i18n/                   # Internationalization
│   ├── index.ts            # i18next configuration
│   └── locales/
│       ├── en/             # English translations
│       │   ├── common.json # Nav, buttons, status, errors
│       │   ├── pages.json  # Page-specific strings
│       │   └── forms.json  # Form labels, placeholders
│       └── cs/             # Czech translations
│           ├── common.json
│           ├── pages.json
│           └── forms.json
├── pages/                  # Page components
│   ├── Analyze/            # Split into components
│   │   ├── hooks/useSortJob.ts
│   │   ├── AnalyzeForm.tsx
│   │   ├── AnalyzeResults.tsx
│   │   ├── AnalyzeStatus.tsx
│   │   └── index.tsx
│   ├── Faces/              # Split into components
│   │   ├── hooks/useFaceSearch.ts
│   │   ├── FacesConfigPanel.tsx
│   │   ├── FacesMatchGrid.tsx
│   │   ├── FacesResultsSummary.tsx
│   │   └── index.tsx
│   ├── Photos/             # Split into components
│   │   ├── hooks/usePhotosFilters.ts
│   │   ├── hooks/usePhotosPagination.ts
│   │   ├── PhotosFilters.tsx
│   │   └── index.tsx
│   ├── PhotoDetail/        # Split into components
│   │   ├── hooks/
│   │   │   └── usePhotoNavigation.ts  # Album/label navigation
│   │   ├── EmbeddingsStatus.tsx
│   │   ├── FaceAssignmentPanel.tsx
│   │   ├── FacesList.tsx
│   │   ├── PhotoDisplay.tsx
│   │   └── index.tsx
│   └── Recognition/        # Split into components
│       ├── hooks/useScanAll.ts
│       ├── PersonResultCard.tsx
│       ├── ScanConfigPanel.tsx
│       ├── ScanResultsSummary.tsx
│       └── index.tsx
└── types/
    ├── events.ts           # Typed SSE events
    └── index.ts            # API response types
```

### Shared Hooks

#### `useSubjectsAndConfig`
Loads subjects (people) and config in one call. Used by Faces, Recognition, and Outliers pages.

```typescript
const { subjects, config, isLoading, error } = useSubjectsAndConfig();
```

#### `useFaceApproval`
Handles single and batch face approval with progress tracking.

```typescript
const { approveMatch, approveAll, isApproving, batchProgress } = useFaceApproval({
  onApprovalSuccess: (match) => updateUI(match),
});
```

#### `useSSE`
Server-Sent Events hook for real-time job progress.

```typescript
const sseUrl = jobId ? `/api/v1/sort/${jobId}/events` : null;
useSSE(sseUrl, { onMessage: handleEvent });
```

### Typed SSE Events

SSE events are typed using discriminated unions in `types/events.ts`:

```typescript
export type SortJobEvent =
  | { type: 'status'; data: SortJob }
  | { type: 'progress'; data: { processed_photos: number; total_photos: number } }
  | { type: 'completed'; data: SortJobResult }
  | { type: 'job_error'; message: string };
```

Use `parseSortJobEvent()` and `parseProcessJobEvent()` helpers to safely parse raw SSE messages.

### Action Constants

Face action styling is centralized in `constants/actions.ts`:

```typescript
import { ACTION_LABELS, ACTION_BORDER_COLORS, ACTION_BG_COLORS } from '../constants/actions';

// Usage
<div className={ACTION_BORDER_COLORS[match.action]}>
  {ACTION_LABELS[match.action]}
</div>
```

### Internationalization

The app uses i18next with react-i18next for translations.

**Using translations in components:**

```typescript
import { useTranslation } from 'react-i18next';

function MyComponent() {
  const { t } = useTranslation(['pages', 'common']);

  return (
    <div>
      <h1>{t('pages:dashboard.title')}</h1>
      <button>{t('common:buttons.save')}</button>
      <p>{t('common:units.photo', { count: 5 })}</p>
    </div>
  );
}
```

**Namespaces:**
- `common` - Shared strings (nav, buttons, status, errors, units)
- `pages` - Page-specific content
- `forms` - Form labels and placeholders

**Pluralization (Czech):**
Czech uses three plural forms: `_one`, `_few`, `_many`:
```json
{
  "photo_one": "{{count}} fotka",
  "photo_few": "{{count}} fotky",
  "photo_many": "{{count}} fotek"
}
```

### Error Handling

The app is wrapped in an `ErrorBoundary` component that catches React rendering errors and displays a user-friendly error page with retry options.

### Loading States

Use the `LoadingState` component for consistent loading/error/empty states:

```typescript
<LoadingState
  isLoading={loading}
  error={error}
  isEmpty={data.length === 0}
  emptyTitle="No results"
>
  {/* Content when loaded */}
</LoadingState>
```

Or use `PageLoading` for simple full-page loading:

```typescript
if (isLoading) return <PageLoading text="Loading..." />;
```

## Performance Optimization

The face recognition system uses two key optimizations to achieve sub-second response times:

### Cached PhotoPrism Data

During `photo process`, the system caches PhotoPrism marker data directly in PostgreSQL:

| Cached Field | Purpose |
|--------------|---------|
| `MarkerUID` | PhotoPrism marker identifier for applying changes |
| `SubjectUID` | Person/subject identifier |
| `SubjectName` | Person name (e.g., "john-doe") |
| `PhotoWidth` | Photo dimensions for coordinate conversion |
| `PhotoHeight` | Photo dimensions for coordinate conversion |
| `Orientation` | EXIF orientation (1-8) for proper bounding box positioning |
| `FileUID` | Primary file identifier |

**Benefits:**
- Face suggestions load instantly (0 API calls vs ~200 calls per face)
- Face matching and outlier detection use cached data
- Cache stays synchronized when faces are assigned via the UI

### HNSW Indexes

For large photo libraries (10k+ photos), HNSW (Hierarchical Navigable Small World) indexes provide O(log N) similarity search instead of O(N) linear scan.

The PostgreSQL backend automatically builds in-memory HNSW indexes for both faces and image embeddings at server startup. By default, this takes ~4 minutes for 45k faces and must be repeated on every restart.

```
Connecting to PostgreSQL database...
Building in-memory HNSW index for face matching...
Face HNSW index built with 45000 faces (in-memory only)
Building in-memory HNSW index for image embeddings...
Embedding HNSW index built with 20000 embeddings (in-memory only)
Using PostgreSQL backend
```

**Enabling fast startup with HNSW persistence:**

Set the index paths to persist to disk:

```bash
export HNSW_INDEX_PATH=/data/faces.pg.hnsw
export HNSW_EMBEDDING_INDEX_PATH=/data/embeddings.pg.hnsw
```

With persistence enabled:
- Indexes are saved on graceful shutdown (Ctrl+C)
- Indexes are saved after "Rebuild Index" operations
- Indexes are loaded from disk on startup if fresh (~seconds instead of ~4 min)
- Indexes are rebuilt if stale (count mismatch)

```
Connecting to PostgreSQL database...
Loading face HNSW index from /data/faces.pg.hnsw...
Face HNSW index ready with 45000 faces (persisted to /data/faces.pg.hnsw)
Loading embedding HNSW index from /data/embeddings.pg.hnsw...
Embedding HNSW index ready with 20000 embeddings (persisted to /data/embeddings.pg.hnsw)
Using PostgreSQL backend
```

If you modify data directly in PostgreSQL, rebuild the indexes via the Web UI or API.

**Performance comparison:**

| Photo Library Size | Linear Scan | With HNSW |
|--------------------|-------------|-----------|
| 1,000 items | ~10ms | ~5ms |
| 10,000 items | ~100ms | ~10ms |
| 100,000 items | ~500ms | ~20ms |

## Configuration

Environment variables for the web server:

| Variable | Default | Description |
|----------|---------|-------------|
| `WEB_PORT` | 8080 | Server port |
| `WEB_HOST` | 0.0.0.0 | Server host |
| `WEB_SESSION_SECRET` | (random) | Secret for signing session cookies |
| `HNSW_INDEX_PATH` | (none) | Path to persist face HNSW index for PostgreSQL backend (enables fast startup) |
| `HNSW_EMBEDDING_INDEX_PATH` | (none) | Path to persist embedding HNSW index for PostgreSQL backend (enables fast startup for Expand/Similar) |
