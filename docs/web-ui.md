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

The Web UI uses local user accounts stored in the `users` table (bcrypt-
hashed passwords) with roles `admin` / `editor` / `viewer`. The first
admin is bootstrapped from `BOOTSTRAP_ADMIN_USERNAME` /
`BOOTSTRAP_ADMIN_PASSWORD` on a fresh install; further users are managed
via **Settings → Users** (admin only). Sessions are 30-day signed
`HttpOnly` cookies persisted in PostgreSQL.

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

- **Primary** (always visible): Dashboard, Albums, Photos, Browse, Labels
- **AI** dropdown: Analyze, Text Search
- **Faces** dropdown: Faces, Recognition, Outliers
- **Tools** dropdown: Similar, Expand, Duplicates, Album Completion, Photo Book, Upload, Process, Trash
- **Right toolbar** (top-right): Capture link (mobile-only, below the `md` breakpoint), GitHub icon with version label, language switcher, Settings link (admin-only tabs for Users and Audit log are hidden for non-admin users), Logout button.

Dropdown buttons highlight when one of their child pages is active. Dropdowns close when clicking outside or pressing `Escape`; `ArrowDown` on the trigger opens the menu.

The header also displays the app version next to the GitHub icon: tag name (e.g., `v1.0.2`) for releases, or the short commit hash for dev builds.

## Routing

All routes live in `web/src/App.tsx`. Authenticated routes are wrapped in `ProtectedRoute` (redirects to `/login` when no session). The public share viewer and the login page are the only routes that skip auth.

| Path | Component |
|------|-----------|
| `/login` | Login |
| `/share/:slug` | Public share viewer (no app session) |
| `/` | Dashboard |
| `/albums`, `/albums/:uid` | Albums (list + detail share the same component) |
| `/smart-albums/:uid` | Smart album detail (CRUD lives on `/albums`) |
| `/photos`, `/photos/:uid` | Photos list / Photo Detail |
| `/browse` | Map + timeline scrubber |
| `/labels`, `/labels/:uid` | Labels list / Label Detail |
| `/subjects/:uid` | Subject Detail |
| `/analyze` | AI Analyze |
| `/faces`, `/recognition`, `/outliers` | Face workflows |
| `/similar`, `/expand`, `/duplicates`, `/compare`, `/suggest-albums` | Photo similarity tools |
| `/text-search` | CLIP text-to-image search |
| `/process` | Compute embeddings + faces |
| `/books`, `/books/:id` | Books list / Book Editor |
| `/upload` | Drag-and-drop upload |
| `/capture` | Mobile PWA quick-shoot |
| `/trash` | Archived-photos browser |
| `/settings`, `/settings/audit-log` | Settings (the `/audit-log` deep link opens the audit tab) |
| `/slideshow` | Fullscreen slideshow (rendered without `Layout` chrome) |
| `*` | Redirect to `/` |

## Pages

### Dashboard

The home page displays:

- **Stats Cards** - Quick overview of albums, labels, processed photo count, face embeddings, and waiting (unprocessed) photos
- **Quick Actions** - Links to common tasks
- **AI Provider Status** - Shows which AI providers are configured and available

### Albums

Browse and manage your albums (from the native `albums` table).

**Features:**
- View all albums with photo counts
- Search albums by name
- Click an album to view its photos
- Quick access to analyze an album with AI
- **Photo navigation context** - When clicking a photo from an album, navigation arrows and position counter are available in Photo Detail
- **Bulk photo removal** - Enter selection mode to select photos and remove them from the album in bulk
- **Public share links** - The album detail page has a Share button that opens a modal where the owner can mint a public share link. Each link has an auto-generated `[a-z0-9-]{3,64}` slug (editable), an optional bcrypt-hashed password, and an optional expiration date. Existing links are listed with copy-URL and revoke actions. The recipient URL is `{origin}/share/<slug>` — see [Public Share Viewer](#public-share-viewer-shareslug) below.
- **Smart albums** - A dedicated section above the regular album grid lists *smart albums* (saved photo searches). Each card shows the saved name, computed photo count, and edit/delete icons. The "Create smart album" button opens a modal with name + the same filter controls as the Photos page (label combobox, subject combobox, favorite toggle, date pickers, GPS bbox, search string, sort). Clicking a smart album card navigates to `/smart-albums/:uid`, which fetches a fresh, live photo list against the saved filter — renames preserve the UID so bookmarks survive. Filters referencing deleted entities silently drop out at query time.

### Public Share Viewer (`/share/:slug`)

The `/share/:slug` route renders a simplified gallery for an anonymous recipient. It MUST NOT redirect to login and uses a separate page chrome (no app navigation, no "sign up" CTA).

**Flow:**
- The page calls `GET /api/v1/public/share/{slug}/` to load metadata. A 404 renders a "share not found" page; a 410 renders "this link expired".
- If the link is password-protected and not yet verified, a centred password gate is shown. Submitting the form hits `POST /verify` which (on success) sets a per-share HttpOnly cookie (`share_<slug>`, 24h) so subsequent requests pass without re-prompting. Verifies are rate-limited to 10 per IP per 5 minutes; on `429` the gate displays a "try again later" message backed by `Retry-After`.
- After verification (or for unprotected links), the recipient sees the album title, photo count, expiration notice (when set), and a thumbnail grid. Clicking a photo opens a lightbox with previous/next navigation and a "Download original" button which streams `GET /photos/{uid}/download` for that share. Bulk download is intentionally out of scope.

### Photos

Browse all photos in your library with powerful filtering.

**Deleted Photo Filtering:** Soft-deleted (archived) photos are excluded from the default `GET /api/v1/photos` listing. Use the Trash page (or `?archived=true`) to view them.

**Features:**
- **Search** - Full-text search across photo metadata
- **Filter by Year** - Dropdown to filter by year
- **Filter by Label** - Autocomplete combobox to filter by label (type to search, keyboard navigation)
- **Filter by Album** - Autocomplete combobox to filter by album (type to search, keyboard navigation)
- **More filters** (collapsible) - Additional optional filters:
  - **Date range** - Two `<input type="date">` controls ("Od"/"Do" in Czech, "From"/"To" in English). Empty bounds are open-ended; values are sent to the backend as RFC3339 `taken_from`/`taken_to` (start-of-day / end-of-day in local time)
  - **GPS bbox** - Single text input accepting `lat1,lng1,lat2,lng2`; parsed into `min_lat`/`min_lng`/`max_lat`/`max_lng` (min/max derived from the two corners). Invalid (non-empty, not four numbers) input shows an inline error and is not sent to the backend
- **Sort Options** - Date (newest/oldest), recently added, recently edited, name, title
- **Selection Mode** - Click "Select" to enter multi-select mode:
  - Click photos to select/deselect
  - "Select All" / "Deselect" buttons for batch selection
  - Bulk actions: Add to Album, Add Label, Favorite
  - When viewing album filter: Remove from Album action
  - Click "Cancel" to exit selection mode
- **Hover quick actions** - When not in selection mode, hovering a photo card surfaces a bottom-right toolbar with three buttons: favorite (★ toggle), add-to-album (➕ opens an in-card album picker), and archive (🗑 with confirm dialog). Mutations call the same batch endpoints as the bulk action bar; archived photos drop from the grid immediately. The toolbar is hidden for viewers (HasWriteAccess required) and on coarse-pointer devices (no hover). Also enabled on the Album Detail and Smart Album Detail grids.
- **Filter persistence** - Active filters are stored in the URL (`?q=…&year=…&label=…&album=…&sort=…&taken_from=…&taken_to=…&bbox=lat1,lng1,lat2,lng2`) and forwarded to Photo Detail, so the back button returns you to the same filtered view with cached photos and scroll position restored. Sharing the URL preserves the full filter (including date range and bbox).
- **Photo navigation context** - When clicking a photo, navigation arrows and position counter are available in Photo Detail
- **Photo Detail Modal** - Click any photo to see full details
  - Photo metadata (date, camera, location)
  - Applied labels
  - Quick link to find similar photos

### Photo Detail (`/photos/:uid`)

Detailed view of a single photo with face management capabilities.

**Features:**
- Full-resolution photo display with interactive face bounding boxes
- Photo metadata (title, date, dimensions)
- **Edit photo (non-destructive)** — header button (sliders icon, `editor`/`admin` only) opens a full-screen modal with a live `react-easy-crop` preview, brightness + contrast sliders (-100..+100 → -1.0..+1.0), and Rotate Left/Right buttons (90° steps). **Save** PUTs `/photos/{uid}/edits` (the server synchronously regenerates every cached thumbnail from the post-edit pixels and rolls back if `heif-convert`/`dcraw` is missing on a HEIC/RAW source — surfaced as a 503). **Restore original** confirms then DELETEs the edits row (idempotent revert). The original file on disk is never modified — edits live in `photo_edits` and are re-applied at thumbnail/download/PDF-export time. Photos with stored edits get an amber "Edited" badge next to the title. EXIF metadata edits are available via the backend (`PUT /api/v1/photos/{uid}/exif` writes the photo row AND an XMP sidecar via `exiftool`); the Web UI does not yet expose an inline form — use the CLI or REST API directly.
- Quick actions: Copy UID, Find Similar, Add to Book, Load Faces
- **Album membership** - If the photo belongs to any albums, an "In albums" panel is shown in the right sidebar listing each album as a clickable link to the album detail page
- **Book membership** - If the photo belongs to any photo book sections, a "In books" panel is shown in the right sidebar (above Era Estimation) listing each book/section as a clickable link to the book editor
- **Add to Book dropdown** - Click "Book" in the header to open a two-step picker (book → section) to quickly add the photo to a book section without leaving the page. Shows success/error feedback and auto-closes
- **Embeddings status banner** - Automatically checks if embeddings have been calculated for the photo on page load. Shows a yellow warning banner with a "Calculate Embeddings" button if not yet processed
- **Face detection and assignment** - Load faces to see detected faces with bounding boxes, assign people via suggestions or manual input
- **Fullscreen mode** - Press `Shift+F` to hide all chrome (header, sidebar, status banner, app navigation) and display the photo at maximum size using the full viewport. Press `Shift+F` again or `Escape` to return to normal view. Navigation arrows and keyboard shortcuts (`←`/`→`, `M`) remain functional in fullscreen
- **Toggle face markings** - Press `M` to show/hide face bounding box overlays on the photo. Automatically loads face data if not already loaded
- **Quick actions via keyboard** - `J`/`K` (or `←`/`→`) navigate prev/next, `F` toggles favorite, `E` opens the edit modal, `A` archives (with confirm), `Esc` closes the detail view. These ship through the global shortcut registry — press `?` anywhere to see the full list

**Photo Navigation:**
When accessing a photo from an album, label, or the Photos page, navigation controls are available:
- **Left/Right arrows** - Semi-transparent navigation buttons appear on hover over the photo
- **Position counter** - Shows current position (e.g., "22 / 41") at the bottom center on hover
- **Keyboard navigation** - Use ← and → arrow keys to navigate between photos
- URL preserves context via query parameter (`?album=xyz`, `?label=slug`, or `?source=photos` — the Photos source additionally carries the active `q`/`year`/`label`/`album`/`sort` filters so the back button restores them)
- Photo list is cached in sessionStorage for fast navigation without extra API calls
- Direct URL access (e.g., sharing a link) fetches the album/label photos from API automatically (Photos page uses cache only)

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
- Select a face to see AI-powered person suggestions with confidence scores (up to 3 shown in the UI)
- Suggestions use a fallback mechanism: if the default distance threshold yields fewer results than requested, a wider search fills the remaining slots so that faces with embeddings always get suggestions
- Accept a suggestion or manually type a person name (with autocomplete)
- Color-coded bounding boxes indicate assignment status (red=unassigned, yellow=needs assignment, green=assigned, orange=outlier)
- **Reassign** - For already-assigned faces, click "Reassign" to change the person. Shows suggestions (excluding the current person) and manual input. Cancel to return to the assigned view
- **Unassign** - For already-assigned faces, click "Unassign" to remove the person assignment. The face reverts to unassigned status with suggestions available for re-assignment

### Browse (`/browse`)

Explore the library on a map and a timeline simultaneously. The map shows
photos with GPS coordinates as clustered markers; the timeline shows a
photo-count histogram with a draggable range selector ("brush"). The two
views are bidirectionally synced:

- Panning or zooming the map updates `min_lat` / `min_lng` / `max_lat` /
  `max_lng`, which constrains both the markers fetched AND the
  histogram bars (so the timeline reflects only photos visible on the
  map).
- Dragging the timeline brush updates `taken_from` / `taken_to`, which
  narrows the markers shown on the map. The histogram itself is NOT
  filtered by the date range — keeping the chart shape stable while the
  brush moves makes the date-picking gesture feel direct.

**Features:**
- **Clustered map** — Leaflet + OpenStreetMap tiles with the
  `react-leaflet-cluster` marker cluster layer (`leaflet.markercluster`).
  Click a cluster to see its photos in the side panel; click a single
  marker to see one photo there. Cluster pills are sized by member
  count.
- **Auto bucketing** — the timeline picks `month` buckets by default and
  switches to `year` when the matching photo set spans more than 5
  years.
- **No-location chip** — photos without GPS are reported via a "No
  location (N photos)" chip; clicking it opens the regular Photos page
  filtered by the active date range so the user can still find those
  photos.
- **No-date chip** — photos with no `taken_at` are surfaced as a
  separate chip so the user knows they're not represented in the
  histogram.
- **Truncation warning** — when the geo-points endpoint hits its 50,000
  point cap (the server-side cap), the UI shows a banner asking the
  user to zoom in or narrow the date range.
- **URL state** — `min_lat` / `min_lng` / `max_lat` / `max_lng` /
  `taken_from` / `taken_to` are mirrored to the URL so back/forward
  navigation and link sharing work.
- **Empty states** — when the library has no photos at all, an "upload
  some photos" nudge is shown. When the library has photos but none
  carry GPS coordinates, the map is replaced with a "No photos have
  GPS coordinates yet" placeholder; the timeline keeps working so the
  user can still browse by date.
- **Mobile** — the map and timeline stack vertically on mobile (below
  the `md` breakpoint).

**Endpoints:** `GET /api/v1/photos/histogram` and
`GET /api/v1/photos/geo-points`. See `docs/API.md` for the full
contract.

### Labels

Manage labels in your library (from the native `labels` table).

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
- **Thumbnail** - Subject's face thumbnail from the `subjects` table
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

**Similarity-search indexes:**

pgvector maintains the HNSW indexes on `embeddings.embedding` and
`faces.embedding` automatically on every INSERT / UPDATE / DELETE — there
is no "Rebuild Index" button and nothing for the operator to flush at
shutdown. See [`similarity-search.md`](similarity-search.md) for the
operator-side `REINDEX` escape hatch.

**Sync Cache:**

Re-derives the cached face-marker columns on the `faces` table (subject linkage, photo dimensions, orientation) from the canonical native `markers` table. Useful after bulk data fixes outside the UI — face assignments performed inside the UI are already kept in sync automatically. Also cleans up orphaned data for photos that have been archived or hard-deleted.

- **Description** - Explains when to use sync
- **Sync Cache** button - Refreshes marker metadata for all photos with faces/embeddings
- **Success message** - Shows photos scanned, faces updated, deleted photos cleaned up, and duration
- **Error handling** - Displays any errors that occur during sync

**What gets synced:**
| Field | Description |
|-------|-------------|
| `marker_uid` | Marker UID |
| `subject_uid` | Subject/person UID from the marker |
| `subject_name` | Person name from the subject row |
| `photo_width`, `photo_height` | Photo dimensions |
| `orientation` | EXIF orientation (1-8) |
| `file_uid` | Primary file UID |

**When to use:**
- After bulk modifications to the `markers` / `subjects` tables outside the UI
- When face matches show incorrect "already_done" status

The sync operation processes all photos with faces in parallel (20 workers) and only writes back rows whose cached data drifted.

**API Endpoints:**
- `POST /api/v1/process` - Start processing job
- `GET /api/v1/process/{jobId}/events` - SSE event stream
- `DELETE /api/v1/process/{jobId}` - Cancel running job
- `POST /api/v1/process/sync-cache` - Re-derive cached marker metadata on the `faces` table

Only one process job can run at a time. Changes are immediately available in the database.

### Faces

Find and match faces across your photo library.

**Search:**
- Select a person from the dropdown (subjects with at least one assigned face)
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
- **Reject** - Remove from view without modifying any marker

**Empty State:**
When no actionable matches are found after scanning, displays "All matches already assigned".

### Duplicate Detection

Find near-duplicate photos in your library using CLIP embedding similarity.

**Configuration:**
- **Scope** - All photos or filter by album
- **Similarity Threshold** - Slider from 80% to 99% (default 90%). Maps to cosine distance: `distance = 1 - (percentage / 100)`
- **Max Groups** - Maximum number of duplicate groups to return (default 100)

**Algorithm:**
Uses union-find (disjoint set) to build connected components of similar photos. For each photo, finds neighbors within the cosine distance threshold using the pgvector HNSW index, then groups connected photos together.

**Results:**
- **Stats** - Photos scanned, groups found, total duplicates
- **Groups** - Each group shows a card with photos and their similarity scores
- **Actions** - Select photos within groups for bulk actions (add to album, add label, favorite)
- **Compare** - Side-by-side comparison view for each duplicate group

### Compare View

The Compare page (`/compare`) provides a side-by-side photo comparison interface for resolving duplicate groups.

**How to access:** Click the "Compare" button on any duplicate group in the Duplicates page.

**Features:**
- **Side-by-side display** - Two photos shown at `fit_1280` resolution
- **Metadata diff table** - Compares dimensions, megapixels, date taken, camera model, filename, original name, type, country, and favorite status. Differences highlighted in amber; better values (e.g., higher resolution) in green
- **Actions per pair:**
  - **Keep Left** (key: `1`) - Archives the right photo
  - **Keep Right** (key: `2`) - Archives the left photo
  - **Keep Both** (key: `Space`) - Skips to next pair without archiving
- **Navigation** - Arrow keys (`←`/`→`) to move between pairs
- **Smart pair management** - When a photo is archived, all remaining pairs involving it are automatically removed
- **Summary screen** - Shows archived/skipped counts when all pairs are resolved

**Pair generation:** For a group of N photos, generates all unique pairs: N*(N-1)/2 combinations.

### Slideshow (`/slideshow`)

Fullscreen slideshow viewer for photos in an album or label.

**How to access:** Click the "Slideshow" button on an album detail page or label detail page.

**URL Parameters:**
- `?album=UID` - Show photos from an album
- `?label=UID` - Show photos from a label

**Features:**
- Fullscreen dark background with no navigation chrome
- Auto-play advances photos every 5 seconds by default; choices: 5s/10s/20s/30s, persisted in `localStorage`
- Photo info overlay (source name, photo title, date) fades in on hover
- Controls bar fades in on hover with play/pause, speed selection, counter, and exit
- Preloads next 2 images for instant transitions
- Stops at last photo (no loop); pressing play at the end restarts from the beginning
- Transition effects with smooth crossfade animations
- Ken Burns motion (slow pan + zoom, alternates direction per photo) — independent toggle, persisted
- TV mode for presentation on a TV: browser fullscreen, all chrome hidden, large captions, wake lock, floating pill control bar that fades in on cursor movement

**TV Mode (`F` or the TV icon button):**
- Requests browser fullscreen via the Fullscreen API; falls back to in-page maximized black overlay with a toast if denied
- Hides every UI chrome (nav, top info, bottom controls); replaces them with a floating pill bar (prev / play-pause / next / exit) at bottom-center
- Floating bar and mouse cursor fade out after 3s of inactivity and reappear on movement
- Requests `navigator.wakeLock.request('screen')` to keep the display awake; silent no-op when the API is unsupported (e.g. older Safari) and released on exit
- Optional caption overlay at bottom-left (semi-transparent, large `clamp()`-scaled font) — shows photo title, description, and a human-friendly date ("June 2024")
- Pause instantly freezes Ken Burns motion on the current frame, not just auto-advance
- Press `Esc` to exit TV mode and restore chrome

**Transition Effects** (cycled via the wand button — Ken Burns motion is now a separate toggle, see below):
- **No effect** - Simple opacity fade-in (default)
- **Reflections** - Subtle breathing pulse during display with slide-up transition
- **Dissolve** - Smooth cross-dissolve between photos
- **Push** - Outgoing photo slides left, incoming slides from right
- **Origami** - 3D fold/unfold page-turn effect

**Ken Burns Motion (`K`):** Independent toggle from the transition effect — slow CSS-keyframe pan + zoom (max 1.15× scale, max 8% pan from center, cubic-bezier ease) layered on top of any transition. Direction alternates per photo (zoom-in/out, pan left/right) so the loop doesn't feel repetitive.

**Captions (`C`):** Toggles the TV-mode caption strip (bottom-left). Persisted in `localStorage`. Has no visible effect outside TV mode.

**Speed Options:** 5s / 10s / 20s / 30s. Persisted in `localStorage`.

**Keyboard Shortcuts:**
- `←` / `→` - Previous / next photo
- `↑` / `↓` - Slower / faster (cycles through speed presets, wraps around)
- `Space` - Toggle play/pause
- `K` - Toggle Ken Burns motion
- `C` - Toggle TV-mode captions overlay
- `I` - Toggle top info overlay
- `F` - Toggle TV mode (full chrome-less presentation)
- `Escape` - Exit TV mode if active; otherwise exit slideshow (returns to previous page)

### Upload (`/upload`)

Upload photos with optional labels, multi-album assignment, book section placement, and auto-processing. Files are ingested via the native `internal/photopipe` pipeline (hash + format detect → exact-duplicate skip → HEIC/RAW decode → EXIF → near-duplicate scan → write to `STORAGE_ORIGINALS_PATH/YYYY/MM/` → `photos` + `photo_files` rows → `internal/thumb.GenerateSizes`).

**Configuration (left card):**
- **Drag & Drop Zone** - Drag files or click to browse. Supports JPG, PNG, GIF, HEIC, WebP, TIFF, RAW formats. Files are validated by MIME type and extension, deduplicated by name+size. On mobile (< 768px viewport) the zone is compact and the "drag and drop" hint is replaced by a tap-to-choose hint
- **Take photo / Choose files (mobile only)** - Below 768px viewport, two explicit full-width buttons render under the drop zone. "Take photo" opens the device camera directly via `<input type="file" accept="image/*" capture="environment">` and uploads a single shot through the existing job flow as a 1-file batch. "Choose files" opens the gallery picker (multi-select). On desktop both buttons are hidden — the drop zone itself is the file picker affordance. iOS Safari may treat `capture="environment"` as a hint rather than a hard switch; that's accepted as a browser quirk
- **Album Selection** - Checkbox list with search filter. At least one album required. First album is the primary upload target; additional albums receive the photos after upload
- **Labels** - Tag input with autocomplete from existing labels. Press Enter to add custom labels
- **Book Section** - Optional cascading dropdowns: select a book, then a section. The two combos stack vertically below 640px to keep each control wide enough on phones. Photos are added to the section after upload
- **Auto-process** - Checkbox (default: on). When enabled, computes CLIP embeddings and detects faces for uploaded photos
- **Mobile layout (< 768px)** - The whole left card stacks vertically (already true via the global single-column grid). Album rows, the album filter, the label input, and the action button all hit a minimum 44px touch target. The file list and album checklist scroll internally to keep the page short. Bulk upload still uses the existing `POST /api/v1/upload/job` flow — no backend changes

**Progress (right card):**
- Real-time progress via SSE with phase indicators:
  - Uploading (per-file progress with filename)
  - Processing (hash, EXIF, dedup, write originals, generate thumbs)
  - Applying labels, albums, book section
  - Computing embeddings & faces (if auto-process enabled)
- Cancel button during upload
- Results summary: uploaded count, new photos, existing (duplicates), labels applied, albums added, book section added
- Thumbnail grid of new photos linking to Photo Detail

**Backend:**
- Files ingested one-by-one through `internal/photopipe` for per-file progress
- Exact duplicates are short-circuited by SHA256 and surfaced in the summary
- Only one upload job runs at a time

### Capture (`/capture`)

Mobile-first quick-shoot page that uses the device's native camera (via
`<input type="file" capture="environment">`) to push one photo at a time into
a chosen album.

- Album dropdown at the top, persisted to `localStorage.capture_default_album`
- Big circular emerald "shoot" button — single tap opens the camera, the
  captured image is uploaded via `POST /api/v1/upload` (one file at a time,
  no SSE)
- Success / failure toast auto-dismisses after ~2.5 s; the button re-arms
  for another shot
- "Recent" strip below shows the album's latest three thumbnails (best-effort
  refresh after each upload — links to the photo detail page)
- Layout is narrow-column on desktop / full-width on phones; the page is
  also surfaced as a small emerald "Capture" link in the header that is only
  shown below the `md` breakpoint so desktops stay clean

The site is also installable as a PWA via `web/public/manifest.webmanifest`
(name `Photo Sorter`, short name `Sorter`, `display=standalone`, emerald
icons in `web/public/icons/`). Opening the installed app lands on the
dashboard; users navigate to `/capture` via the mobile-only header link or by
bookmarking it as the home-screen target.

### Trash (`/trash`)

Soft-delete inbox for archived photos. Any authenticated role can browse
and restore; the irreversible "Empty trash" hard-delete is admin-only.

- **List** — `GET /api/v1/photos/trash` lists archived photos with the same filters / sort / pagination as `/api/v1/photos`; the `archived` flag is force-overridden so callers can never reach live photos via this route.
- **Restore** — `POST /api/v1/photos/batch/restore` clears `archived_at` on the selected UIDs and moves them back to the main library.
- **Empty trash** (admin only) — `POST /api/v1/photos/batch/purge` hard-deletes the selected photos: the photo row (cascades to phashes, markers, files, album_photos, photo_labels), the embedding, every cached face row, every original file on disk, and every cached thumbnail size. Non-archived UIDs are skipped with an error.
- **Auto-purge** — a background daemon launched from `cmd/serve.go` runs the same purge hourly against photos older than `TRASH_RETENTION_DAYS` (default 30) so the trash never grows unbounded.

### Settings (`/settings`)

Three-tab page for account, user, and audit-trail management.

- **Profile** (any role) — Shows the signed-in user's username / display name / role and lets them change their own password (`POST /api/v1/me/password`, current + new). Wrong current password → `401`; new password under 8 characters → `400`.
- **Users** (admin only) — Lists all users from `GET /api/v1/users` with role and disabled status. The dialog supports:
  - Create user — `POST /api/v1/users` with `{ username, display_name, email, role, password }`. Username collisions return `409`.
  - Rename / change role / change email — `PUT /api/v1/users/{uid}` (username itself is immutable).
  - Reset another user's password — `POST /api/v1/users/{uid}/password`.
  - Disable / re-enable an account — `POST /api/v1/users/{uid}/disable` with `{ disabled: bool }`.
  - Delete user — `DELETE /api/v1/users/{uid}`. The last remaining admin cannot be deleted (`409`).
- **Audit log** (admin only; deep-linkable at `/settings/audit-log`) — Read-only viewer over every mutating action and security-sensitive event recorded in the `audit_log` table (`GET /api/v1/audit-log`). The toolbar exposes filters for **user** (dropdown loaded from `/api/v1/users`), **action** (grouped dropdown by category: auth / photo / album / label / subject / face / user / book / share_link / smart_album / process), **entity type**, **since** / **until** datetime pickers, and a **per-page** size selector (50 / 100 / 200; backend caps at 200). The table renders timestamp (local time), user (with `<deleted user>` / `anonymous` fallbacks), action (human-readable label from the i18n `auditLog.actions` map), entity (type label + monospace UID), client IP, and a collapsed metadata cell that expands to a pretty-printed JSON view of the row's metadata + User-Agent. Pagination shows `Page X of Y` and `Showing M / Total entries`; an **Export CSV** button dumps the currently visible page as RFC4180-escaped CSV (id, timestamp, user_uid, user_username, action, entity_type, entity_uid, ip, user_agent, metadata) so it can be archived or post-processed. Non-admin sessions never see the tab, and the `/audit-log` API path is gated by `RequireRole(RoleAdmin)`.

### Photo Book (`/books`)

Plan and organize photos into a printed landscape photo book with PDF export.

**Books List:**
- Card grid of all books with title, stats (sections, pages, photos)
- Create new books with inline title input
- Delete books with confirmation
- Click a book to open the editor

### Book Editor (`/books/:id`)

Five-tab editor for organizing a photo book.

**Statistics Panel:**
- Toggle via BarChart3 icon in the editor header (next to Export/Delete buttons)
- Shows key metrics: total pages, photos placed, photos unassigned, slots filled (with fill percentage), format distribution, and section count (with empty section count)
- Fill rate uses color coding: green >= 80%, amber >= 50%, red < 50%
- All data computed client-side from existing book and sectionPhotos state
- Toggle state persisted to localStorage per book

**Sections Tab:**
- **Section Sidebar** - Sortable list of sections with optional chapter grouping (drag to reorder). Create and delete sections and chapters (with confirmation dialogs). Shows placement stats (placed/total) per section — green when all photos are placed
  - **Chapters** (optional) - Add chapters to group sections. Chapters are collapsible with a chevron toggle. Drag-and-drop reordering for both chapters and sections. Inline chapter title editing. Delete chapter confirmation dialog. Uncategorized sections appear at the top when chapters exist. Chapter name shown in section headers with `|` delimiter
  - **Move to Chapter** - Use the dropdown selector on a section to assign it to a chapter
- **Photo Pool** - Grid of photos in the selected section with thumbnails
- **Drag-and-Drop Between Sections** - Select photos and drag them to a different section in the sidebar. Multi-photo dragging supported. Visual feedback shows rose border on drop target and count badge on drag overlay. Target sections without empty capacity are visually dimmed
- **Add by Photo ID** - Inline text input to quickly add a photo by pasting its UID (validates existence, checks for duplicates)
- **Description Editing** - Click a photo to open the PhotoDescriptionDialog modal for editing description and note (same modal as Pages tab). Includes AI-powered text check (spelling/grammar + readability suggestions) and text rewrite (length adjustment) buttons powered by GPT-5.4-mini
- **Bulk Selection** - Select multiple photos for batch removal
- **Photo Browser Modal** - Full-screen modal to browse the entire library, search, and add photos to a section. Album and label filters use autocomplete comboboxes. Already-added photos are grayed out

**Pages Tab:**
- **Page Sidebar** - Pages grouped by section with collapsible headers (collapse state persisted to localStorage). Each section header shows the section title and page count, with a chevron toggle to collapse/expand. Quick-add button (+) next to each section opens a format picker popover for fast page creation. Pages show thumbnail previews of their slots (mini grid matching the page format) instead of plain "Page N" labels. Completed pages have green highlight; partially filled pages have rose highlight. Pages are sortable within a section (drag to reorder) and can be dragged onto another section's container (or a page inside another section) to move the page between sections — the target section highlights while hovered, the moved page is appended at the end of the target section, its slots and typography are preserved, and its photos are reconciled between the source/target section photo pools server-side. Cross-section moves are undoable via Ctrl/Cmd+Z. Global page numbering (1, 2, 3...) is preserved across sections. Creating a new page auto-expands the target section if collapsed. Create pages with format selector and section assignment. Delete pages with confirmation dialog
- **Page Minimap** - Compact visual overview of all pages grouped by section. Shows mini layout renderers matching each format, with rose ring on selected page, green ring on fully filled pages, and amber dot on partially filled ones. Slot thumbnails preview assigned photos, text icons for text slots, and dashed borders for empty slots. Limited to 200px height with scrolling
- **Page Template** - Visual CSS grid representation of the page layout with droppable slots
- **Drag-and-Drop** - Drag photos from the unassigned pool into page slots
- **Undo/Redo** - Ctrl+Z to undo and Ctrl+Shift+Z (or Ctrl+Y) to redo slot assignments. Tracks assign, clear, swap, and cross-section page move operations with up to 50 entries per stack
- **Unassigned Pool** - Photos in the page's section not yet assigned to any page slot
- **Auto-Layout** - Click the wand icon (Auto-layout) next to a section header to automatically generate pages from unassigned photos. Algorithm selects optimal page formats (prioritizing `4_landscape`, then mixed formats, then `2_portrait`, then `1_fullscreen`). Shows success message with page and photo counts
- **Text Slots** - Click "Add text" on empty slots to place markdown content instead of photos. Supports headings, bold, italic, lists, blockquotes, and GFM tables (pipe syntax with optional column width percentages). Preview renders via marked.js + DOMPurify
- **Captions Slots** - Click "Use for captions" on empty slots (button next to "Add text") to dedicate a slot to displaying the page's photo captions instead of holding a photo or text. The captions render stacked vertically inside the slot with numbered badges and hanging indent (wrapped lines align under the first text character), and the bottom captions strip is suppressed for that page. Use this when a single caption is too long to fit in the bottom strip. At most one captions slot per page; the button is hidden once one is set. Clearing the slot or replacing it with a photo/text restores the bottom strip automatically
- **Contents Slots** - Click "Použít pro obsah" / "Use for contents" on empty slots to render the book's auto-generated table of contents (chapter names uppercase, sections italic with dotted leaders and page ranges) in two columns inside the slot. The heading `Obsah` is always shown on top. Page numbers and chapter ordering come from the canonical book structure, so the TOC stays in sync whenever pages are added / reordered. Chapters can be individually hidden from the TOC via the "V obsahu" checkbox in the Typography tab next to each chapter's colour picker (useful for intentional back-matter pages like advertisements). At most one contents slot per page; the button is hidden once one is set

**Page Formats:**

| Format | Slots | Layout |
|--------|-------|--------|
| `4_landscape` | 4 | 2x2 grid of landscape photos |
| `2l_1p` | 3 | 2 landscape (left) + 1 portrait (right, full height) |
| `1p_2l` | 3 | 1 portrait (left, full height) + 2 landscape (right) |
| `2_portrait` | 2 | 2 portrait photos side by side |
| `1_fullscreen` | 1 | Single fullscreen photo |

**Preview Tab:**
- Read-only scrollable view of the entire book
- Section titles as dividers between page groups
- Page numbers computed from sort order
- Photos rendered at reasonable size with descriptions
- Empty slots shown as gray placeholders

**Duplicates Tab:**
- Cross-section duplicate finder that identifies photos appearing in 2+ sections
- Loads all section photos on mount and scans for duplicates
- Shows photo thumbnail, list of sections containing the photo, and one-click remove button per section
- Counter displays total number of duplicate entries found
- Empty state when no duplicates exist

**Texts Tab:**
- Overview of all text content in the book (section photo descriptions, text slots). Each row shows a breadcrumb with **type** (photo caption / text), **chapter** (with color dot), **section**, and **clickable page number(s)** — clicking a page number switches to the Pages tab and selects that page
- Entries are sorted globally by page number (unplaced items last)
- **Stats Panel** - Total texts, checked count, texts with errors, stale checks, count of major readability issues, total reading time
- **Text Search** - Filter texts by content, chapter, section, or page number
- **Batch AI Text Check** - Run AI text check on all texts with progress tracking. Results persisted to database via three-tier cache (in-memory → DB → OpenAI). After server restart, unchanged texts are served from DB cache without burning a fresh OpenAI call
- **Readability Suggestions** - Every text check returns advisory readability/flow items in `suggestions[]`. Severity `major` (red) flags hard-to-read text; `minor` (amber) is polish. Displayed below the mechanical changes in the expanded panel; rendered via the shared `CheckSuggestionsList` component used by all three AI check surfaces (Texts tab, TextSlotDialog, PhotoDescriptionDialog). A warning triangle with a count appears next to the row status indicator whenever any suggestions exist
- **Style Consistency Check** - AI analysis of style consistency across all book texts (tone, issues, score)
- **Text Version History** - View and restore previous versions of any text field (up to 20 versions)
- **Download Texts** - Button in the toolbar exports all texts as a structured JSON file (`<book-slug>-texts.json`) containing chapter, section, page, slot, and content. Intended for external LLM analysis

**Export PDF:**
- Click "Export PDF" in the editor header to generate a print-ready A4 landscape PDF
- **Preflight check** runs automatically before export, validating for empty slots, low-DPI photos, empty sections, unplaced photos, and missing captions
- If preflight finds warnings, a modal displays them with "Go to page" links for quick navigation to issues
- "Export anyway" button is always available to proceed despite warnings
- If no warnings, export starts immediately without the modal

**Dependencies:** Uses `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities` for drag-and-drop.

### Album Completion

Find photos that belong in existing albums but aren't there yet by searching the pgvector embedding HNSW index.

**Configuration:**
- **Min Similarity** - Slider from 50% to 90% (default 70%). Converted to cosine similarity threshold
- **Max Photos Per Album** - Maximum number of suggested photos per album (1-50, default 20)

**Algorithm:**
1. For each album with enough photos, computes centroid (mean + L2-normalize) of its photo embeddings
2. Searches the pgvector HNSW index with the centroid to find similar photos
3. Filters out photos already in the album
4. Returns albums with suggested photos, sorted by suggestion count

**Results:**
- **Stats** - Albums analyzed, photos suggested, albums skipped (no embeddings)
- **Suggestions** - One card per album with suggested photos and similarity scores
- **Actions** - "Add All to Album" button per suggestion to add all matched photos at once

## Keyboard Shortcuts

### Global
- `?` - Toggle the keyboard cheatsheet overlay (lists every active shortcut grouped by page). The same modal is reachable via the keyboard icon in the top bar. `Esc` or click-outside closes it. All shortcuts noop when focus is inside an `<input>`, `<textarea>`, `<select>`, or `contenteditable` element so typing is never stolen.

The cheatsheet renders from the single registry at `web/src/shortcuts/registry.ts`. Pages opt into a shortcut by calling `useRegisterShortcut(id, handler)`; the registry decides what `id` means in terms of key and description so the modal and the dispatcher can't drift apart. BookEditor predates this system and keeps its own `useBookKeyboardNav` hook — its entries appear in the cheatsheet for discoverability but are flagged in `NON_DISPATCHED_SHORTCUTS` so the global dispatcher does not double-fire them.

### Photos Grid
- `J` / `K` - Move focus to next / previous card (with an amber focus ring that scrolls into view)
- `Enter` - Open the focused photo's detail page
- `Space` - Toggle selection of the focused card (auto-enters selection mode if not active)

### Photo Detail Page
- `J` / `K` or `←` / `→` - Navigate to previous/next photo (when accessed from album, label, or Photos page)
- `F` - Toggle favorite on the current photo
- `E` - Open the non-destructive edit modal (editor/admin only)
- `A` - Archive the current photo (opens a confirm dialog; on confirm, soft-deletes and returns to the photo grid)
- `M` - Toggle face marking bounding boxes (auto-loads face data if needed)
- `Shift+F` - Toggle fullscreen mode (hides all chrome, photo fills viewport)
- `Escape` - Close confirm dialog > close edit modal > exit fullscreen > go back to the grid (in that precedence)

### Photo Detail Modal
- `←` / `→` - Navigate between photos
- `Escape` - Close modal

### Book Editor — Pages Tab
- `W` / `S` - Navigate to previous / next page
- `E` / `D` - Jump to first page of previous / next chapter
- `Ctrl+Z` - Undo last slot assignment (assign, clear, or swap)
- `Ctrl+Shift+Z` / `Ctrl+Y` - Redo last undone slot assignment
- Disabled when a dialog is open (photo description, text slot, crop)

### Book Editor — Sections Tab
- `W` / `S` - Navigate to previous / next section
- `E` / `D` - Jump to first section of previous / next chapter

### Slideshow
- `←` / `→` - Previous / next photo
- `↑` / `↓` - Slower / faster (cycles speed presets 5/10/20/30s, wraps)
- `Space` - Toggle play/pause
- `K` - Toggle Ken Burns motion
- `C` - Toggle TV-mode captions overlay
- `I` - Toggle top info overlay
- `F` - Toggle TV (presentation) mode
- `Escape` - Exit TV mode if active; otherwise exit slideshow

## API Endpoints

`web/src/api/client.ts` is the single typed wrapper for every REST call the UI makes. See [`docs/API.md`](API.md) for the authoritative endpoint reference (request/response shapes, auth requirements, status codes). The UI surfaces the following endpoint groups; cross-reference API.md for details:

| Area | Endpoint group | UI surfaces |
|------|----------------|-------------|
| Auth + session | `/api/v1/auth/{login,status,logout}` | Login page, `useAuth` |
| Current user / users (admin) | `/api/v1/me/*`, `/api/v1/users/*` | Settings → Profile / Users |
| Audit log (admin) | `/api/v1/audit-log` | Settings → Audit log |
| Albums + shares + smart albums | `/api/v1/albums/*`, `/api/v1/shares/*`, `/api/v1/public/share/*`, `/api/v1/smart-albums/*` | Albums page, ShareModal, PublicShare, SmartAlbums |
| Photos + browse | `/api/v1/photos`, `/api/v1/photos/histogram`, `/api/v1/photos/geo-points`, `/api/v1/photos/:uid/*` | Photos, Browse, Photo Detail |
| Photo edits | `/api/v1/photos/:uid/edits` (GET/PUT/DELETE) | PhotoEditModal |
| Photo similarity + text search | `/api/v1/photos/similar*`, `/api/v1/photos/search-by-text`, `/api/v1/photos/duplicates`, `/api/v1/photos/suggest-albums` | SimilarPhotos, Expand, TextSearch, Duplicates, SuggestAlbums |
| Trash | `/api/v1/photos/trash`, `/api/v1/photos/batch/{archive,restore,purge}` | Trash page (purge admin-only) |
| Labels + subjects | `/api/v1/labels`, `/api/v1/labels/:uid`, `/api/v1/subjects/*` | Labels, LabelDetail, SubjectDetail |
| Faces | `/api/v1/photos/:uid/faces*`, `/api/v1/faces/{match,apply,outliers}` | Faces, Outliers, Recognition, PhotoDetail |
| Sort jobs | `/api/v1/sort/*` (+ SSE) | Analyze |
| Process jobs | `/api/v1/process*` (+ SSE), `/api/v1/process/sync-cache` | Process |
| Upload jobs | `/api/v1/upload/job` (+ SSE) | Upload, Capture (the single-shot `/api/v1/upload`) |
| Books | `/api/v1/books*`, `/api/v1/chapters/*`, `/api/v1/sections/*`, `/api/v1/pages/*`, `/api/v1/book-export/*` (+ SSE) | Books, BookEditor |
| AI text | `/api/v1/text/{check,check-and-save,rewrite,consistency}`, `/api/v1/books/:id/text-check-status`, `/api/v1/text-versions/*` | BookEditor (Texts tab, dialogs) |
| Config / stats | `/api/v1/config`, `/api/v1/stats` | Dashboard, Analyze, version label in header |

## Frontend Architecture

The frontend is built with React + TypeScript + TailwindCSS and follows a modular architecture for maintainability.

### Directory Structure

```
web/src/
├── api/
│   └── client.ts           # Typed API client wrapping every REST endpoint
├── components/             # Shared UI components
│   ├── AccentCard.tsx      # Accent-colored card
│   ├── Alert.tsx           # Alert/notification component
│   ├── BulkActionBar.tsx   # Bulk action panel for photo selection
│   ├── Button.tsx
│   ├── Card.tsx
│   ├── Combobox.tsx        # Autocomplete combobox for filtering
│   ├── ConfirmDialog.tsx   # Reusable confirmation dialog
│   ├── ErrorBoundary.tsx   # Error catching wrapper
│   ├── FormCheckbox.tsx    # Styled checkbox with label
│   ├── FormInput.tsx       # Styled text/number input with label
│   ├── FormSelect.tsx      # Styled select dropdown with label
│   ├── LanguageSwitcher.tsx # Language toggle button
│   ├── LazyImage.tsx
│   ├── Layout.tsx          # Header + nav shell wrapping every authenticated page
│   ├── LoadingState.tsx    # Unified loading/error/empty states
│   ├── PageHeader.tsx      # Page header with title/actions
│   ├── PageLayoutPreview.tsx # Mini SVG preview of a book page layout (slot grid)
│   ├── PhotoCard.tsx
│   ├── PhotoGrid.tsx       # Supports optional selection mode; opt-in hover quick-actions toolbar (favorite / archive / add-to-album) on PhotoCard via `enableQuickActions`
│   ├── PhotoQuickActions.tsx # Bottom-right hover toolbar on PhotoCard: favorite toggle, archive (confirm), add-to-album popover. Hidden on coarse-pointer devices.
│   ├── PhotoWithBBox.tsx
│   ├── ShareModal.tsx      # Mint / list / revoke public share links for an album
│   └── StatsGrid.tsx       # Stats display grid (configurable 2-6 columns)
├── constants/              # Shared constants
│   ├── actions.ts          # Face action styling (i18n label keys, colors)
│   ├── bookTypography.ts   # Typography CSS defaults, font registry, CSS variable helpers
│   ├── index.ts            # Magic numbers, defaults, cache keys
│   └── pageConfig.ts       # Book page format configuration
├── hooks/                  # Global hooks
│   ├── useAuth.tsx                 # Session + login/logout + currently-authenticated user
│   ├── useBookKeyboardNav.ts       # Book editor keyboard nav (W/S/E/D)
│   ├── useFaceApproval.ts          # Face approval logic (single + batch)
│   ├── usePhotoSelection.ts        # Shared photo selection + bulk actions
│   ├── useSSE.ts                   # Server-Sent Events
│   └── useSubjectsAndConfig.ts     # Parallel load of subjects + /config
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
│   ├── Albums.tsx          # Album list + album detail (single component, route-based)
│   ├── Analyze/            # AI sort
│   │   ├── hooks/useSortJob.ts
│   │   ├── AnalyzeForm.tsx
│   │   ├── AnalyzeResults.tsx
│   │   ├── AnalyzeStatus.tsx
│   │   └── index.tsx
│   ├── BookEditor/         # Book editor (sections, pages, typography, texts, preview, duplicates)
│   │   ├── hooks/useBookData.ts
│   │   ├── hooks/useBookExportJob.ts # SSE-backed PDF export job runner
│   │   ├── hooks/useUndoRedo.ts      # Undo/redo for slot assignments + cross-section page moves
│   │   ├── BookStatsPanel.tsx        # Statistics panel (pages, photos, fill rate)
│   │   ├── CheckSuggestionsList.tsx  # Shared readability-suggestions list (major/minor severity)
│   │   ├── DuplicatesTab.tsx         # Cross-section duplicate photo finder
│   │   ├── ExportProgressModal.tsx   # Streaming PDF export progress + cancel
│   │   ├── KeyboardShortcutsHelp.tsx
│   │   ├── PageMinimap.tsx           # Compact page overview panel
│   │   ├── PageSidebar.tsx           # Thumbnail previews, quick-add button
│   │   ├── PageSlot.tsx
│   │   ├── PageTemplate.tsx
│   │   ├── PagesTab.tsx
│   │   ├── PhotoActionOverlay.tsx
│   │   ├── PhotoBrowserModal.tsx     # Full-library browser for adding photos to a section
│   │   ├── PhotoDescriptionDialog.tsx # Inline AI check + rewrite for photo captions
│   │   ├── PhotoInfoOverlay.tsx
│   │   ├── PreflightModal.tsx        # Pre-export validation + photo-quality picker
│   │   ├── PreviewTab.tsx
│   │   ├── SectionPhotoPool.tsx
│   │   ├── SectionSidebar.tsx
│   │   ├── SectionsTab.tsx
│   │   ├── TextsTab.tsx              # AI check (with suggestions), consistency, JSON download
│   │   ├── TypographyTab.tsx         # Fonts, sizes, colors, body padding, captions opacity, chapter colors + "show in TOC"
│   │   ├── UnassignedPool.tsx
│   │   └── index.tsx
│   ├── Books/              # Photo books list (create / open / delete)
│   │   └── index.tsx
│   ├── Browse/             # Map + timeline scrubber page
│   │   ├── BrowseMap.tsx       # react-leaflet wrapper with clustering
│   │   ├── BrowseTimeline.tsx  # recharts BarChart + Brush
│   │   ├── BrowseSidePanel.tsx # Side panel for cluster/marker click
│   │   ├── leafletSetup.ts     # Default marker icon fix for Vite
│   │   └── index.tsx
│   ├── Capture/            # Mobile PWA quick-shoot page (`/capture`)
│   │   └── Capture.tsx
│   ├── Compare/            # Side-by-side photo comparison
│   │   ├── hooks/useCompareState.ts
│   │   ├── CompareView.tsx
│   │   ├── MetadataDiff.tsx
│   │   ├── CompareSummary.tsx
│   │   └── index.tsx
│   ├── Dashboard.tsx       # Home page (stats cards, quick actions, AI provider status)
│   ├── Duplicates/         # Near-duplicate detection (single index.tsx)
│   │   └── index.tsx
│   ├── Expand.tsx          # Find photos similar to an entire label/album
│   ├── Faces/              # Face matching (per-subject)
│   │   ├── hooks/useFaceSearch.ts
│   │   ├── FacesConfigPanel.tsx
│   │   ├── FacesMatchGrid.tsx
│   │   ├── FacesResultsSummary.tsx
│   │   └── index.tsx
│   ├── LabelDetail.tsx
│   ├── Labels.tsx
│   ├── Login.tsx
│   ├── Outliers.tsx
│   ├── PhotoDetail/        # Single-photo viewer + face assignment + non-destructive edit
│   │   ├── hooks/usePhotoData.ts
│   │   ├── hooks/useFacesData.ts
│   │   ├── hooks/useFaceAssignment.ts
│   │   ├── hooks/usePhotoNavigation.ts # Album/label/photos navigation
│   │   ├── AddToBookDropdown.tsx
│   │   ├── AlbumMembership.tsx
│   │   ├── BookMembership.tsx
│   │   ├── EmbeddingsStatus.tsx
│   │   ├── EraEstimate.tsx           # Era estimation panel (right sidebar)
│   │   ├── FaceAssignmentPanel.tsx
│   │   ├── FacesList.tsx
│   │   ├── PhotoDisplay.tsx
│   │   ├── PhotoEditModal.tsx        # Non-destructive crop/rotate/brightness/contrast + revert
│   │   └── index.tsx
│   ├── Photos/             # Photo browser with filters + bulk selection
│   │   ├── hooks/usePhotosFilters.ts
│   │   ├── hooks/usePhotosPagination.ts
│   │   ├── PhotosFilters.tsx
│   │   └── index.tsx
│   ├── Process.tsx         # Compute embeddings + faces; sync-cache button
│   ├── Recognition/        # Bulk face recognition (Scan All People)
│   │   ├── hooks/useScanAll.ts
│   │   ├── PersonResultCard.tsx
│   │   ├── ScanConfigPanel.tsx
│   │   ├── ScanResultsSummary.tsx
│   │   └── index.tsx
│   ├── Settings/           # Profile / Users / Audit log (admin-gated tabs)
│   │   ├── AuditLog.tsx        # Filters + pagination + CSV export
│   │   ├── EditUserDialog.tsx  # Create / rename / role / password / disable
│   │   ├── Settings.tsx        # Tab container (initialTab prop honoured)
│   │   └── Users.tsx           # Admin user list
│   ├── Share/              # Public album viewer (no app session)
│   │   └── PublicShare.tsx
│   ├── SimilarPhotos.tsx
│   ├── Slideshow/          # Fullscreen slideshow + TV presentation mode
│   │   ├── hooks/useSlideshow.ts
│   │   ├── hooks/useSlideshowPhotos.ts
│   │   ├── hooks/useTVMode.ts     # Browser fullscreen, wake lock, cursor auto-hide
│   │   ├── effectConfigs.ts       # Transition effects + Ken Burns motion config
│   │   ├── SlideshowControls.tsx
│   │   ├── TVControlBar.tsx       # Floating pill control bar shown in TV mode
│   │   └── index.tsx
│   ├── SmartAlbums/        # Saved photo searches (rendered into the Albums page + own routes)
│   │   ├── SmartAlbumDetail.tsx   # Detail page at /smart-albums/:uid
│   │   ├── SmartAlbumModal.tsx    # Create / edit modal with full filter form
│   │   └── SmartAlbumsSection.tsx # Card section embedded above the album grid
│   ├── SubjectDetail.tsx
│   ├── SuggestAlbums/      # Album completion (pgvector centroid search)
│   │   └── index.tsx
│   ├── TextSearch.tsx      # CLIP text-to-image search
│   ├── Trash/              # Archived-photos browser + restore/purge
│   │   └── Trash.tsx
│   └── Upload/             # Multipart upload with SSE progress
│       ├── hooks/useUploadJob.ts
│       ├── DropZone.tsx
│       ├── NearDuplicatesModal.tsx # Post-upload near-duplicate warnings
│       └── index.tsx
├── types/
│   ├── events.ts           # Typed SSE events (discriminated unions)
│   ├── index.ts            # API response types
│   └── turndown-plugin-gfm.d.ts
└── utils/
    ├── clipboard.ts        # Clipboard copy
    ├── fontLoader.ts       # Google Fonts CSS loader (deduplicates, display=swap)
    ├── markdown.ts         # Markdown-to-HTML renderer (marked.js + DOMPurify)
    ├── pageFormats.ts      # Book page format helpers
    └── paste.ts            # HTML → Markdown paste handler for caption/text textareas
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

#### `usePhotoSelection`
Shared photo selection with bulk actions. Used by Photos, SimilarPhotos, Expand, and Duplicates pages.

```typescript
const selection = usePhotoSelection();
// selection.selectedPhotos, selection.toggleSelection, selection.selectAll, selection.deselectAll
// selection.handleAddToAlbum, selection.handleAddLabel, selection.handleBatchEdit, selection.handleRemoveFromAlbum
```

#### `useSSE`
Server-Sent Events hook for real-time job progress.

```typescript
const sseUrl = jobId ? `/api/v1/sort/${jobId}/events` : null;
useSSE(sseUrl, { onMessage: handleEvent });
```

#### `useBookKeyboardNav`
Keyboard navigation for the Book Editor (W/S to move between pages or sections; E/D to jump by chapter). Disabled when a dialog is open. Used by `BookEditor/PagesTab` and `BookEditor/SectionsTab`.

#### `useAuth`
Authentication context provider. Exposes the currently-authenticated user (with `role`), `login`, `logout`, and `isAuthenticated` / `isLoading` flags. `ProtectedRoute` in `App.tsx` consumes it; pages call `useAuth()` to gate write actions by `role` (e.g., the non-destructive edit button on Photo Detail).

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

// ACTION_LABELS and ACTION_DESCRIPTIVE_LABELS contain i18n keys, not display text.
// Wrap with t() at render time:
<div className={ACTION_BORDER_COLORS[match.action]}>
  {t(ACTION_LABELS[match.action])}
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
- `common` - Shared strings (nav, buttons, status, errors, units, tooltips, actions, effects)
- `pages` - Page-specific content
- `forms` - Form labels and placeholders

**Important:** All user-visible text must use `t()` — including `title`, `aria-label`, and `placeholder` attributes. Do not use hardcoded English strings in any component.

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

### Cached marker metadata

The processing pipeline denormalises a few columns from the `markers` /
`subjects` / `photos` tables directly onto the `faces` row so face
matching, outlier detection, and the "Faces" panel never have to join
across them at request time:

| Cached Field | Purpose |
|--------------|---------|
| `MarkerUID` | Marker UID (for applying changes) |
| `SubjectUID` | Person/subject identifier |
| `SubjectName` | Person name (e.g., "john-doe") |
| `PhotoWidth` | Photo dimensions for coordinate conversion |
| `PhotoHeight` | Photo dimensions for coordinate conversion |
| `Orientation` | EXIF orientation (1-8) for proper bounding box positioning |
| `FileUID` | Primary file identifier |

**Benefits:**
- Face suggestions load instantly (no per-face joins)
- Face matching and outlier detection read everything from `faces`
- Cache stays synchronized when faces are assigned via the UI; bulk
  out-of-band fixes can be re-derived via `POST /api/v1/process/sync-cache`

### Similarity-search indexes

pgvector maintains HNSW indexes on `embeddings.embedding` (768-dim CLIP)
and `faces.embedding` (512-dim ResNet100) with operator class
`vector_cosine_ops`. They are created by migration
`038_pgvector_hnsw_indexes.sql` (auto-applied at startup) and stay in
sync with INSERT / UPDATE / DELETE for free — the server holds no
in-memory similarity-search state and `pg_dump` is a complete backup.

The first server start after an upgrade from a pre-`038` snapshot will
block while pgvector builds the indexes (expected: minutes on a 50k-row
table on a Raspberry Pi). Subsequent restarts are instant. See
[`similarity-search.md`](similarity-search.md) for query shape,
`hnsw.ef_search` tuning, and the `REINDEX` escape hatch.

## Configuration

Environment variables for the web server:

| Variable | Default | Description |
|----------|---------|-------------|
| `WEB_PORT` | 8080 | Server port |
| `WEB_HOST` | 0.0.0.0 | Server host |
| `WEB_SESSION_SECRET` | (insecure default) | Secret for signing session cookies. **Must be set in production** — a warning is logged at startup if unset |
| `WEB_ALLOWED_ORIGINS` | (none) | Comma-separated list of allowed CORS origins (e.g., `https://photos.example.com`). Localhost origins are always allowed for development |

### Security Headers

The server automatically sets the following security headers on all responses:

- **Content-Security-Policy** — Restricts resource loading to same-origin (`default-src 'self'`), with exceptions for inline styles and data/blob URIs for images
- **X-Content-Type-Options: nosniff** — Prevents MIME type sniffing
- **X-Frame-Options: DENY** — Prevents clickjacking via iframes
- **CORS** — Only reflects `Access-Control-Allow-Origin` for whitelisted origins (from `WEB_ALLOWED_ORIGINS`) and localhost. Credentials are allowed only for whitelisted origins
- **Session cookies** — `HttpOnly`, `SameSite=Strict`, and `Secure` is auto-detected when behind HTTPS (via `X-Forwarded-Proto` header or direct TLS)
