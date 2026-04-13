# API Reference

This document describes all REST API endpoints for the PhotoPrism AI Sorter web server.

**Base URL:** `/api/v1`

## Table of Contents

- [Authentication](#authentication)
- [Albums](#albums)
- [Photos](#photos)
- [Labels](#labels)
- [Subjects (People)](#subjects-people)
- [Face Matching](#face-matching)
- [Sort (AI Analysis)](#sort-ai-analysis)
- [Process (Embeddings & Faces)](#process-embeddings--faces)
- [Upload](#upload)
- [Configuration](#configuration)
- [Statistics](#statistics)
- [Health Check](#health-check)
- [Error Responses](#error-responses)
- [Real-Time Updates (SSE)](#real-time-updates-sse)
- [Text AI](#text-ai)
- [Text Version History](#text-version-history)
- [MCP Server](#mcp-server)

---

## Authentication

Authentication can be enabled or disabled via configuration. When enabled, login creates a session stored in cookies.

### Login

Authenticate with PhotoPrism credentials.

```
POST /auth/login
```

**Request:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response (200):**
```json
{
  "success": true,
  "session_id": "abc123xyz",
  "expires_at": "2024-01-15T10:30:00Z"
}
```

**Response (401):**
```json
{
  "success": false,
  "error": "invalid credentials"
}
```

### Logout

End the current session.

```
POST /auth/logout
```

**Request:** Empty body

**Response (200):**
```json
{
  "success": true
}
```

### Check Authentication Status

```
GET /auth/status
```

**Response (200):**
```json
{
  "authenticated": true,
  "expires_at": "2024-01-15T10:30:00Z"
}
```

---

## Albums

### List Albums

```
GET /albums
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `count` | int | 100 | Number of albums to return |
| `offset` | int | 0 | Pagination offset |
| `order` | string | - | Sort order |
| `q` | string | - | Search query |

**Response (200):**
```json
[
  {
    "uid": "aq8i4k2l3m9n0o1p",
    "title": "Vacation 2024",
    "description": "Summer trip to Italy",
    "photo_count": 150,
    "thumb": "abc123hash",
    "type": "album",
    "favorite": false,
    "created_at": "2024-01-10T08:00:00Z",
    "updated_at": "2024-01-12T15:30:00Z"
  }
]
```

### Get Album

```
GET /albums/{uid}
```

**Response (200):**
```json
{
  "uid": "aq8i4k2l3m9n0o1p",
  "title": "Vacation 2024",
  "description": "Summer trip to Italy",
  "photo_count": 150,
  "thumb": "abc123hash",
  "type": "album",
  "favorite": false,
  "created_at": "2024-01-10T08:00:00Z",
  "updated_at": "2024-01-12T15:30:00Z"
}
```

**Response (404):**
```json
{
  "error": "album not found"
}
```

### Create Album

```
POST /albums
```

**Request:**
```json
{
  "title": "New Album Name"
}
```

**Response (201):**
```json
{
  "uid": "aq8newalbum123",
  "title": "New Album Name",
  "description": "",
  "photo_count": 0,
  "thumb": "",
  "type": "album",
  "favorite": false,
  "created_at": "2024-01-15T10:00:00Z",
  "updated_at": "2024-01-15T10:00:00Z"
}
```

### Get Album Photos

```
GET /albums/{uid}/photos
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `count` | int | 100 | Number of photos to return |
| `offset` | int | 0 | Pagination offset |

**Response (200):**
```json
[
  {
    "uid": "pq8abc123def456",
    "title": "Beach Sunset",
    "description": "Beautiful sunset at the beach",
    "taken_at": "2024-07-15T19:30:00Z",
    "year": 2024,
    "month": 7,
    "day": 15,
    "hash": "abc123filehash",
    "width": 4000,
    "height": 3000,
    "lat": 43.7696,
    "lng": 11.2558,
    "country": "it",
    "favorite": true,
    "private": false,
    "type": "image",
    "original_name": "IMG_1234.jpg",
    "file_name": "20240715_193000_ABC123.jpg"
  }
]
```

### Add Photos to Album

```
POST /albums/{uid}/photos
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456", "pq8ghi789"]
}
```

**Response (200):**
```json
{
  "added": 3
}
```

### Clear Album (Remove All Photos)

```
DELETE /albums/{uid}/photos
```

**Response (200):**
```json
{
  "removed": 150
}
```

### Remove Specific Photos from Album (Batch)

Remove specific photos from an album by UID.

```
DELETE /albums/{uid}/photos/batch
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456"]
}
```

**Response (200):**
```json
{
  "removed": 2
}
```

---

## Photos

### List Photos

```
GET /photos
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `count` | int | 100 | Number of photos to return |
| `offset` | int | 0 | Pagination offset |
| `order` | string | - | Sort order |
| `q` | string | - | General search query |
| `year` | string | - | Filter by year |
| `label` | string | - | Filter by label name |
| `album` | string | - | Filter by album UID |

**Response (200):**
```json
[
  {
    "uid": "pq8abc123def456",
    "title": "Beach Sunset",
    "description": "Beautiful sunset at the beach",
    "taken_at": "2024-07-15T19:30:00Z",
    "year": 2024,
    "month": 7,
    "day": 15,
    "hash": "abc123filehash",
    "width": 4000,
    "height": 3000,
    "lat": 43.7696,
    "lng": 11.2558,
    "country": "it",
    "favorite": true,
    "private": false,
    "type": "image",
    "original_name": "IMG_1234.jpg",
    "file_name": "20240715_193000_ABC123.jpg"
  }
]
```

### Get Photo

```
GET /photos/{uid}
```

**Response (200):** Same structure as list item

**Response (404):**
```json
{
  "error": "photo not found"
}
```

### Update Photo

```
PUT /photos/{uid}
```

**Request:** All fields optional
```json
{
  "title": "Updated Title",
  "description": "Updated description",
  "taken_at": "2024-07-15T19:30:00Z",
  "lat": 43.7696,
  "lng": 11.2558,
  "favorite": true,
  "private": false
}
```

**Response (200):** Updated photo object

### Get Photo Thumbnail

```
GET /photos/{uid}/thumb/{size}
```

**Size Values:**
- `tile_50`, `tile_100`, `tile_224`, `tile_500`, `tile_1080`
- `left_224`, `right_224`
- `fit_720`, `fit_1280`, `fit_1600`, `fit_1920`, `fit_2048`, `fit_2560`, `fit_3840`, `fit_4096`, `fit_7680`

**Response:** Binary image data with `Content-Type: image/*`

### Get Photo Album Memberships

```
GET /photos/{uid}/albums
```

Returns the list of albums that contain the given photo.

**Response (200):**
```json
[
  { "uid": "abc123", "title": "Album Name", "photo_count": 42 }
]
```

### Batch Add Labels to Photos

```
POST /photos/batch/labels
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456"],
  "label": "vacation"
}
```

**Response (200):**
```json
{
  "updated": 2,
  "errors": []
}
```

### Batch Edit Photos

Batch update photo metadata (favorite, private flags).

```
POST /photos/batch/edit
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456"],
  "favorite": true,
  "private": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `photo_uids` | string[] | Yes | Photo UIDs to edit |
| `favorite` | boolean | No | Set favorite flag |
| `private` | boolean | No | Set private flag |

At least one of `favorite` or `private` must be provided.

**Response (200):**
```json
{
  "updated": 2,
  "errors": []
}
```

### Batch Archive Photos

Archive (soft-delete) multiple photos.

```
POST /photos/batch/archive
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456"]
}
```

**Response (200):**
```json
{
  "archived": 2
}
```

### Find Similar Photos

Find photos visually similar to a source photo using CLIP embeddings.

```
POST /photos/similar
```

**Request:**
```json
{
  "photo_uid": "pq8abc123def456",
  "limit": 50,
  "threshold": 0.3
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `photo_uid` | string | Yes | - | Source photo UID |
| `limit` | int | No | 50 | Max results to return |
| `threshold` | float | No | 0.3 | Max cosine distance (0-1) |

**Response (200):**
```json
{
  "source_photo_uid": "pq8abc123def456",
  "threshold": 0.3,
  "results": [
    {
      "photo_uid": "pq8xyz789",
      "distance": 0.15,
      "similarity": 0.85
    }
  ],
  "count": 12
}
```

### Find Similar Photos to Collection

Find photos similar to all photos in a label or album.

```
POST /photos/similar/collection
```

**Request:**
```json
{
  "source_type": "label",
  "source_id": "nature",
  "limit": 50,
  "threshold": 0.3
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source_type` | string | Yes | `"label"` or `"album"` |
| `source_id` | string | Yes | Label name or album UID |
| `limit` | int | No | Max results |
| `threshold` | float | No | Max cosine distance |

**Response (200):**
```json
{
  "source_type": "label",
  "source_id": "nature",
  "source_photo_count": 50,
  "source_embedding_count": 48,
  "min_match_count": 1,
  "threshold": 0.3,
  "results": [
    {
      "photo_uid": "pq8xyz789",
      "distance": 0.18,
      "similarity": 0.82,
      "match_count": 5
    }
  ],
  "count": 25
}
```

### Search Photos by Text

Search photos using natural language descriptions (CLIP text-to-image).

```
POST /photos/search-by-text
```

**Request:**
```json
{
  "text": "sunset at the beach",
  "limit": 50,
  "threshold": 0.5
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `text` | string | Yes | - | Search query (supports Czech, auto-translated) |
| `limit` | int | No | 50 | Max results |
| `threshold` | float | No | 0.5 | Max cosine distance |

**Response (200):**
```json
{
  "query": "západ slunce na pláži",
  "translated_query": "sunset at the beach with orange sky and waves",
  "translate_cost_usd": 0.0001,
  "translate_error": "translating text: 401 Unauthorized",
  "threshold": 0.5,
  "results": [
    {
      "photo_uid": "pq8abc123",
      "distance": 0.32,
      "similarity": 0.68
    }
  ],
  "count": 15
}
```

---

## Labels

### List Labels

```
GET /labels
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `count` | int | 1000 | Number of labels to return |
| `offset` | int | 0 | Pagination offset |
| `all` | boolean | false | Include all labels (including system labels) |

**Response (200):**
```json
[
  {
    "uid": "lq8abc123",
    "name": "Nature",
    "slug": "nature",
    "description": "Nature and landscape photos",
    "notes": "",
    "photo_count": 250,
    "favorite": true,
    "priority": 5,
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### Get Label

```
GET /labels/{uid}
```

**Response (200):** Same structure as list item

**Response (404):**
```json
{
  "error": "label not found"
}
```

### Update Label

```
PUT /labels/{uid}
```

**Request:** All fields optional
```json
{
  "name": "Landscapes",
  "description": "Updated description",
  "notes": "Some notes",
  "priority": 10,
  "favorite": true
}
```

**Response (200):** Updated label object

### Batch Delete Labels

```
DELETE /labels
```

**Request:**
```json
{
  "uids": ["lq8abc123", "lq8def456"]
}
```

**Response (200):**
```json
{
  "deleted": 2
}
```

---

## Subjects (People)

### List Subjects

```
GET /subjects
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `count` | int | 100 | Number of subjects to return |
| `offset` | int | 0 | Pagination offset |

**Response (200):**
```json
[
  {
    "uid": "sq8abc123",
    "name": "Jan Novák",
    "slug": "jan-novak",
    "thumb": "face_hash_abc",
    "photo_count": 150,
    "favorite": false,
    "about": "Family member",
    "alias": "Johnny",
    "bio": "",
    "notes": "",
    "hidden": false,
    "private": false,
    "excluded": false,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-10T12:00:00Z"
  }
]
```

### Get Subject

```
GET /subjects/{uid}
```

**Response (200):** Same structure as list item

### Update Subject

```
PUT /subjects/{uid}
```

**Request:** All fields optional
```json
{
  "name": "Jan Novák Jr.",
  "about": "Updated description",
  "alias": "Junior",
  "bio": "Biography text",
  "notes": "Some notes",
  "favorite": true,
  "hidden": false,
  "private": false,
  "excluded": false
}
```

**Response (200):** Updated subject object

---

## Face Matching

### Match Faces

Find photos containing a person's face across the entire library.

```
POST /faces/match
```

**Request:**
```json
{
  "person_name": "jan-novak",
  "threshold": 0.5,
  "limit": 100
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `person_name` | string | Yes | - | Person slug or name |
| `threshold` | float | No | 0.5 | Max cosine distance (0-1) |
| `limit` | int | No | 0 | Max matches (0 = unlimited) |

**Response (200):**
```json
{
  "person": "jan-novak",
  "source_photos": 50,
  "source_faces": 55,
  "matches": [
    {
      "photo_uid": "pq8abc123",
      "distance": 0.25,
      "face_index": 0,
      "bbox": [100, 50, 300, 280],
      "bbox_rel": [0.025, 0.017, 0.075, 0.093],
      "file_uid": "fq8xyz789",
      "action": "create_marker",
      "marker_uid": null,
      "marker_name": null,
      "iou": 0
    },
    {
      "photo_uid": "pq8def456",
      "distance": 0.18,
      "face_index": 1,
      "bbox": [500, 200, 700, 450],
      "bbox_rel": [0.125, 0.067, 0.175, 0.15],
      "file_uid": "fq8abc123",
      "action": "assign_person",
      "marker_uid": "mq8marker1",
      "marker_name": "",
      "iou": 0.85
    },
    {
      "photo_uid": "pq8ghi789",
      "distance": 0.12,
      "face_index": 0,
      "bbox": [200, 100, 400, 350],
      "bbox_rel": [0.05, 0.033, 0.1, 0.117],
      "file_uid": "fq8def456",
      "action": "already_done",
      "marker_uid": "mq8marker2",
      "marker_name": "jan-novak",
      "iou": 0.92
    }
  ],
  "summary": {
    "create_marker": 15,
    "assign_person": 8,
    "already_done": 32
  }
}
```

**Action Types:**
| Action | Description |
|--------|-------------|
| `create_marker` | No marker exists; create new face marker |
| `assign_person` | Marker exists but unassigned; assign to person |
| `already_done` | Marker already assigned to this person |

### Apply Face Match

Apply a face detection result (create marker or assign person).

```
POST /faces/apply
```

**Request for `create_marker`:**
```json
{
  "photo_uid": "pq8abc123",
  "person_name": "jan-novak",
  "action": "create_marker",
  "file_uid": "fq8xyz789",
  "bbox_rel": [0.025, 0.017, 0.075, 0.093]
}
```

**Request for `assign_person`:**
```json
{
  "photo_uid": "pq8def456",
  "person_name": "jan-novak",
  "action": "assign_person",
  "marker_uid": "mq8marker1"
}
```

**Request for `unassign_person`:**
```json
{
  "photo_uid": "pq8ghi789",
  "person_name": "jan-novak",
  "action": "unassign_person",
  "marker_uid": "mq8marker2"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `photo_uid` | string | Yes | Photo UID |
| `person_name` | string | Yes | Person slug or name |
| `action` | string | Yes | `create_marker`, `assign_person`, or `unassign_person` |
| `marker_uid` | string | For assign/unassign | Existing marker UID |
| `file_uid` | string | For create_marker | File UID |
| `bbox_rel` | array | For create_marker | Relative bbox `[x, y, w, h]` |

**Response (200):**
```json
{
  "success": true,
  "marker_uid": "mq8newmarker"
}
```

**Response (400/500):**
```json
{
  "success": false,
  "error": "marker not found"
}
```

### Find Face Outliers

Detect potentially misassigned faces for a person by computing distance from the centroid of all their face embeddings.

```
POST /faces/outliers
```

**Request:**
```json
{
  "person_name": "jan-novak",
  "threshold": 0.15,
  "limit": 50
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `person_name` | string | Yes | - | Person slug or name |
| `threshold` | float | No | 0 | Min distance from centroid |
| `limit` | int | No | 0 | Max results (0 = unlimited) |

**Response (200):**
```json
{
  "person": "jan-novak",
  "total_faces": 200,
  "avg_distance": 0.08,
  "outliers": [
    {
      "photo_uid": "pq8abc123",
      "dist_from_centroid": 0.45,
      "face_index": 0,
      "bbox_rel": [0.1, 0.05, 0.1, 0.13],
      "file_uid": "fq8xyz789",
      "marker_uid": "mq8def456"
    }
  ],
  "missing_embeddings": [
    {
      "photo_uid": "pq8ghi789",
      "dist_from_centroid": -1,
      "face_index": -1,
      "bbox_rel": [0.3, 0.2, 0.08, 0.1],
      "marker_uid": "mq8jkl012"
    }
  ]
}
```

**Notes:**
- `outliers` are sorted by `dist_from_centroid` descending (most suspicious first)
- `missing_embeddings` are faces in PhotoPrism without matching database embeddings

### Get Faces in Photo

Get all detected faces in a photo with assignment suggestions.

```
GET /photos/{uid}/faces
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `threshold` | float | 0.5 | Similarity threshold for suggestions |
| `limit` | int | 5 | Max suggestions per face |

**Response (200):**
```json
{
  "photo_uid": "pq8abc123",
  "file_uid": "fq8xyz789",
  "width": 4000,
  "height": 3000,
  "orientation": 1,
  "embeddings_count": 2,
  "markers_count": 3,
  "faces": [
    {
      "face_index": 0,
      "bbox": [100, 50, 300, 280],
      "bbox_rel": [0.025, 0.017, 0.075, 0.093],
      "det_score": 0.95,
      "marker_uid": "mq8marker1",
      "marker_name": "jan-novak",
      "action": "already_done",
      "suggestions": []
    },
    {
      "face_index": 1,
      "bbox": [500, 200, 700, 450],
      "bbox_rel": [0.125, 0.067, 0.175, 0.15],
      "det_score": 0.88,
      "marker_uid": "mq8marker2",
      "marker_name": "",
      "action": "assign_person",
      "suggestions": [
        {
          "person_name": "marie-novakova",
          "person_uid": "sq8xyz123",
          "distance": 0.22,
          "confidence": 0.78,
          "photo_count": 85
        }
      ]
    },
    {
      "face_index": -1,
      "bbox": [],
      "bbox_rel": [0.5, 0.3, 0.08, 0.1],
      "det_score": 0,
      "marker_uid": "mq8marker3",
      "marker_name": "",
      "action": "assign_person",
      "suggestions": []
    }
  ]
}
```

**Notes:**
- `face_index >= 0`: Face from embeddings database
- `face_index < 0`: Unmatched PhotoPrism marker (no embedding, always has empty suggestions)
- `embeddings_count` vs `markers_count` surfaces discrepancies
- Suggestions use a fallback mechanism: if the `threshold` yields fewer than `limit` results, a wider search (max cosine distance 2.0) fills remaining slots so faces with embeddings always get suggestions when named people exist in the database

### Compute Faces for Photo

Attempt to compute face embeddings for a photo.

```
POST /photos/{uid}/faces/compute
```

**Response (200):**
```json
{
  "photo_uid": "pq8abc123",
  "faces_count": 2,
  "success": true
}
```

**Note:** Use the Process page in the web UI to compute embeddings and detect faces for multiple photos. This endpoint is for single-photo computation.

---

## Sort (AI Analysis)

### Start Sort Job

Start an AI analysis job to categorize and label photos in an album.

```
POST /sort
```

**Request:**
```json
{
  "album_uid": "aq8i4k2l3m9n0o1p",
  "dry_run": false,
  "limit": 0,
  "individual_dates": false,
  "batch_mode": false,
  "provider": "openai",
  "force_date": false,
  "concurrency": 5
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `album_uid` | string | Yes | - | Album to analyze |
| `dry_run` | boolean | No | false | Preview without applying changes |
| `limit` | int | No | 0 | Max photos (0 = all) |
| `individual_dates` | boolean | No | false | Estimate date per photo vs album-wide |
| `batch_mode` | boolean | No | false | Use batch API (50% cost savings, slower) |
| `provider` | string | No | "openai" | AI provider: `openai`, `gemini`, `ollama`, `llamacpp` |
| `force_date` | boolean | No | false | Overwrite existing dates |
| `concurrency` | int | No | 5 | Parallel requests |

**Response (202):**
```json
{
  "job_id": "sort_abc123",
  "album_uid": "aq8i4k2l3m9n0o1p",
  "album_title": "Vacation 2024",
  "status": "pending"
}
```

### Get Sort Job Status

```
GET /sort/{jobId}
```

**Response (200):**
```json
{
  "id": "sort_abc123",
  "album_uid": "aq8i4k2l3m9n0o1p",
  "album_title": "Vacation 2024",
  "status": "running",
  "progress": 45,
  "total_photos": 100,
  "processed_photos": 45,
  "error": null,
  "started_at": "2024-01-15T10:00:00Z",
  "completed_at": null,
  "options": {
    "dry_run": false,
    "limit": 0,
    "individual_dates": false,
    "batch_mode": false,
    "provider": "openai",
    "force_date": false,
    "concurrency": 5
  },
  "result": null
}
```

**Status Values:** `pending`, `running`, `completed`, `failed`, `cancelled`

### Stream Sort Job Events (SSE)

```
GET /sort/{jobId}/events
```

**Headers:**
```
Accept: text/event-stream
```

**Event Types:**

```
event: status
data: {"id":"sort_abc123","status":"running",...}

event: started
data: {}

event: photos_counted
data: {"total":100}

event: progress
data: {"processed_photos":45,"total_photos":100}

event: completed
data: {"processed_count":100,"sorted_count":95,"album_date":"2024-07","usage":{...}}

event: job_error
data: {"message":"API rate limit exceeded"}

event: cancelled
data: {}
```

### Cancel Sort Job

```
DELETE /sort/{jobId}
```

**Response (200):**
```json
{
  "cancelled": true
}
```

---

## Process (Embeddings & Faces)

### Start Process Job

Start a job to compute image embeddings and detect faces for all photos.

```
POST /process
```

**Request:**
```json
{
  "concurrency": 5,
  "limit": 0,
  "no_faces": false,
  "no_embeddings": false
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `concurrency` | int | No | 5 | Parallel workers |
| `limit` | int | No | 0 | Max photos (0 = all) |
| `no_faces` | boolean | No | false | Skip face detection |
| `no_embeddings` | boolean | No | false | Skip image embeddings |

**Response (202):**
```json
{
  "job_id": "proc_xyz789",
  "status": "pending"
}
```

### Stream Process Job Events (SSE)

```
GET /process/{jobId}/events
```

**Event Types:**

```
event: status
data: {"id":"proc_xyz789","status":"running",...}

event: started
data: {}

event: photos_counted
data: {"total":5000}

event: filtering_done
data: {"to_process":4950,"skipped":50}

event: progress
data: {"processed":1250,"total":4950}

event: completed
data: {"embed_success":4900,"embed_error":50,"face_success":4800,"face_error":150,"total_new_faces":12500}

event: job_error
data: {"message":"embedding service unavailable"}

event: cancelled
data: {}
```

### Cancel Process Job

```
DELETE /process/{jobId}
```

**Response (200):**
```json
{
  "cancelled": true
}
```

### Rebuild HNSW Indexes

Rebuild in-memory HNSW indexes (face and embedding) from PostgreSQL data. Saves to disk if persistence paths are configured.

```
POST /process/rebuild-index
```

**Response (200):**
```json
{
  "face_count": 12500,
  "embedding_count": 5000,
  "duration_ms": 3200
}
```

### Sync Cache

Synchronize face marker data from PhotoPrism to local cache. Updates marker metadata and cleans up orphaned data for deleted/archived photos.

```
POST /process/sync-cache
```

**Response (200):**
```json
{
  "faces_updated": 150,
  "photos_deleted": 3,
  "duration_ms": 8500
}
```

---

## Upload

### Upload Photos

Upload photos to an album.

```
POST /upload
```

**Content-Type:** `multipart/form-data`

**Form Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `album_uid` | string | Yes | Target album UID |
| `files` | file[] | Yes | Photo files to upload |

**Response (200):**
```json
{
  "uploaded": 15,
  "album": "aq8i4k2l3m9n0o1p"
}
```

### Start Upload Job

Start a background upload job with progress tracking via SSE.

```
POST /upload/job
```

**Content-Type:** `multipart/form-data`

**Form Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `files` | file[] | Yes | Photo files to upload |
| `album_uids` | string (JSON array) | Yes | Target album UIDs (first is primary) |
| `labels` | string (JSON array) | No | Labels to apply to new photos |
| `book_section_id` | string | No | Book section ID to add photos to |
| `auto_process` | string | No | "false" to skip embeddings/faces (default: true) |

**Response (202):**
```json
{
  "job_id": "b2f806a0-2e10-4062-8b3c-b5f7b946fb18",
  "status": "pending"
}
```

### Get Upload Job Events

Stream upload job progress via Server-Sent Events.

```
GET /upload/{jobId}/events
```

**SSE Event Types:**
| Event | Data | Description |
|-------|------|-------------|
| `started` | - | Job started |
| `upload_progress` | `{current, total, filename}` | Per-file upload progress |
| `processing_upload` | - | PhotoPrism processing phase |
| `detecting_photos` | - | Detecting new photos via album diff |
| `applying_labels` | `{current, total}` | Applying labels to new photos |
| `applying_albums` | - | Adding to additional albums |
| `adding_to_book` | - | Adding to book section |
| `process_progress` | `{processed, total}` | Embeddings/faces progress |
| `completed` | `UploadJobResult` | Job completed successfully |
| `job_error` | `{message}` | Job failed |
| `cancelled` | - | Job was cancelled |

### Cancel Upload Job

```
DELETE /upload/{jobId}
```

**Response (200):**
```json
{
  "cancelled": true
}
```

---

## Configuration

### Get Configuration

```
GET /config
```

**Response (200):**
```json
{
  "providers": [
    {
      "name": "openai",
      "available": true
    },
    {
      "name": "gemini",
      "available": true
    },
    {
      "name": "ollama",
      "available": false
    },
    {
      "name": "llamacpp",
      "available": false
    }
  ],
  "photoprism_domain": "https://photos.example.com",
  "embeddings_writable": true,
  "version": "v1.0.2",
  "commit_sha": "a1b2c3d"
}
```

---

## Statistics

### Get Statistics

```
GET /stats
```

**Response (200):**
```json
{
  "total_photos": 25000,
  "photos_processed": 24500,
  "photos_with_embeddings": 24000,
  "photos_with_faces": 18000,
  "total_faces": 45000,
  "total_embeddings": 24000
}
```

---

## Health Check

### Health Check (No Authentication Required)

```
GET /health
```

**Response (200):**
```json
{
  "status": "ok"
}
```

---

## Error Responses

All error responses follow a consistent format:

```json
{
  "error": "description of the error"
}
```

**HTTP Status Codes:**

| Code | Description |
|------|-------------|
| 200 | Success |
| 201 | Created |
| 202 | Accepted (async operation started) |
| 400 | Bad Request (invalid parameters) |
| 401 | Unauthorized (authentication required) |
| 404 | Not Found |
| 409 | Conflict (e.g., job already running) |
| 500 | Internal Server Error |
| 503 | Service Unavailable (embeddings not configured) |

---

## Real-Time Updates (SSE)

The API supports Server-Sent Events for real-time job progress monitoring.

**Endpoints:**
- `GET /sort/{jobId}/events` - Sort job progress
- `GET /process/{jobId}/events` - Process job progress

**Connection:**
```javascript
const eventSource = new EventSource('/api/v1/sort/job123/events');

eventSource.addEventListener('progress', (event) => {
  const data = JSON.parse(event.data);
  console.log(`Progress: ${data.processed_photos}/${data.total_photos}`);
});

eventSource.addEventListener('completed', (event) => {
  const result = JSON.parse(event.data);
  console.log('Job completed:', result);
  eventSource.close();
});

eventSource.addEventListener('job_error', (event) => {
  const data = JSON.parse(event.data);
  console.error('Job failed:', data.message);
  eventSource.close();
});
```

**TypeScript Types:**

```typescript
// Sort Job Events
type SortJobEvent =
  | { type: 'status'; data: SortJob }
  | { type: 'started'; data: {} }
  | { type: 'photos_counted'; data: { total: number } }
  | { type: 'progress'; data: { processed_photos: number; total_photos: number } }
  | { type: 'completed'; data: SortJobResult }
  | { type: 'job_error'; message: string }
  | { type: 'cancelled'; data: {} };

// Process Job Events
type ProcessJobEvent =
  | { type: 'status'; data: ProcessJob }
  | { type: 'started'; data: {} }
  | { type: 'photos_counted'; data: { total: number } }
  | { type: 'filtering_done'; data: { to_process: number; skipped: number } }
  | { type: 'progress'; data: { processed: number; total: number } }
  | { type: 'completed'; data: ProcessJobResult }
  | { type: 'job_error'; message: string }
  | { type: 'cancelled'; data: {} };
```

---

## Data Types Reference

### AlbumResponse

```typescript
interface AlbumResponse {
  uid: string;
  title: string;
  description: string;
  photo_count: number;
  thumb: string;          // File hash (not photo UID)
  type: string;
  favorite: boolean;
  created_at: string;     // ISO 8601
  updated_at: string;     // ISO 8601
}
```

### PhotoResponse

```typescript
interface PhotoResponse {
  uid: string;
  title: string;
  description: string;
  taken_at: string;       // ISO 8601
  year: number;
  month: number;
  day: number;
  hash: string;           // File hash
  width: number;
  height: number;
  lat: number;
  lng: number;
  country: string;        // ISO country code
  favorite: boolean;
  private: boolean;
  type: string;           // "image", "video", etc.
  original_name: string;
  file_name: string;
  camera_model: string;
}
```

### LabelResponse

```typescript
interface LabelResponse {
  uid: string;
  name: string;
  slug: string;
  description: string;
  notes: string;
  photo_count: number;
  favorite: boolean;
  priority: number;
  created_at: string;     // ISO 8601
}
```

### SubjectResponse

```typescript
interface SubjectResponse {
  uid: string;
  name: string;
  slug: string;
  thumb: string;          // File hash (not photo UID)
  photo_count: number;
  favorite: boolean;
  about?: string;
  alias?: string;
  bio?: string;
  notes?: string;
  hidden: boolean;
  private: boolean;
  excluded: boolean;
  created_at?: string;    // ISO 8601
  updated_at?: string;    // ISO 8601
}
```

### FaceMatch

```typescript
interface FaceMatch {
  photo_uid: string;
  distance: number;       // Cosine distance (0-1)
  face_index: number;     // Index in embeddings DB (-1 for unmatched markers)
  bbox: number[];         // Pixel coords [x1, y1, x2, y2]
  bbox_rel?: number[];    // Relative coords [x, y, w, h]
  file_uid?: string;
  action: 'create_marker' | 'assign_person' | 'already_done';
  marker_uid?: string;
  marker_name?: string;
  iou?: number;           // Intersection over Union with marker
}
```

### SortJobResult

```typescript
interface SortJobResult {
  processed_count: number;
  sorted_count: number;
  album_date?: string;
  date_reasoning?: string;
  errors?: string[];
  suggestions?: SortSuggestion[];
  usage?: UsageInfo;
}

interface UsageInfo {
  input_tokens: number;
  output_tokens: number;
  total_cost_usd: number;
}
```

---

## Photo Books

**Page `style` field:** Each page has a `style` field (`"modern"` or `"archival"`, default `"modern"`). Set via `POST /books/{id}/pages` (`{ format, section_id, style? }`) or `PUT /pages/{id}` (`{ style }`).

**Page `split_position` field:** For `2l_1p` and `1p_2l` formats, the `split_position` field (0.0-1.0, default 0.5) controls the column split ratio between landscape and portrait slots. Set via `POST /books/{id}/pages` (`{ format, section_id, split_position? }`) or `PUT /pages/{id}` (`{ split_position }`).

**Slot crop control:** Each photo slot has `crop_x` and `crop_y` fields (0.0-1.0, default 0.5) that control the crop center position, and a `crop_scale` field (0.1-1.0, default 1.0) that controls the zoom level (1.0 = fill one axis, lower = zoom in). Use `PUT /pages/{id}/slots/{index}/crop` to adjust.

### Fonts

#### List Available Fonts

```
GET /fonts
```

Returns all fonts available for book typography customization.

**Response (200):**
```json
[
  {
    "id": "pt-serif",
    "display_name": "PT Serif",
    "category": "serif",
    "google_family": "PT+Serif",
    "google_spec": "ital,wght@0,400;0,700;1,400;1,700"
  },
  {
    "id": "source-sans-3",
    "display_name": "Source Sans 3",
    "category": "sans-serif",
    "google_family": "Source+Sans+3",
    "google_spec": "ital,wght@0,400;0,600;0,700;1,400;1,600;1,700"
  }
]
```

### Books CRUD

#### List Books

```
GET /books
```

Returns all photo books.

#### Create Book

```
POST /books
```

**Request:**
```json
{
  "title": "My Photo Book"
}
```

#### Get Book

```
GET /books/{id}
```

Returns book details with chapters, sections, and pages. The response includes a `chapters` array; sections have a `chapter_id` field indicating which chapter they belong to (nullable).

#### Update Book

```
PUT /books/{id}
```

**Request:**
```json
{
  "title": "Updated Title",
  "description": "Updated description",
  "body_font": "pt-serif",
  "heading_font": "source-sans-3",
  "body_font_size": 11.0,
  "body_line_height": 15.0,
  "h1_font_size": 18.0,
  "h2_font_size": 13.0,
  "caption_opacity": 0.85,
  "caption_font_size": 9.0,
  "heading_color_bleed": 4.0,
  "caption_badge_size": 4.0
}
```

All fields are optional (partial updates). Font IDs are validated against the font registry. Size ranges: font sizes 6–36 pt, line height 8–48 pt, caption font size 6–16 pt, opacity 0.0–1.0, heading color bleed 0–20 mm, caption badge size 2–12 mm.

#### Delete Book

```
DELETE /books/{id}
```

Deletes the book and all associated chapters, sections, pages, and slots (cascades).

### Chapters

Chapters are an optional grouping level between books and sections (Book > Chapters > Sections > Pages > Slots).

#### Create Chapter

```
POST /books/{id}/chapters
```

**Request:**
```json
{
  "title": "Part One",
  "color": "#8B0000"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | Yes | Chapter title |
| `color` | string | No | Hex color for chapter theme (e.g. `#8B0000`) |

**Response (201):** Created chapter object with `id`, `title`, `color`, `sort_order`.

#### Update Chapter

```
PUT /chapters/{id}
```

**Request:**
```json
{
  "title": "Updated Chapter Title",
  "color": "#2E5090"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | No | New chapter title |
| `color` | string | No | New hex color for chapter theme |

**Response (200):** Updated chapter object.

#### Reorder Chapters

```
PUT /books/{id}/chapters/reorder
```

**Request:**
```json
{
  "chapter_ids": ["chapter-uuid-1", "chapter-uuid-2", "chapter-uuid-3"]
}
```

**Response (200):** Success.

#### Delete Chapter

```
DELETE /chapters/{id}
```

**Response (200):** Success. Sections belonging to this chapter have their `chapter_id` set to NULL.

### Sections

#### Create Section

```
POST /books/{id}/sections
```

**Request:**
```json
{
  "title": "Childhood",
  "chapter_id": "chapter-uuid-1"
}
```

#### Update Section

```
PUT /sections/{id}
```

**Request:** All fields optional.
```json
{
  "title": "Updated Section Title",
  "chapter_id": "chapter-uuid-1"
}
```

#### Reorder Sections

```
PUT /books/{id}/sections/reorder
```

**Request:**
```json
{
  "ids": ["section-uuid-1", "section-uuid-2", "section-uuid-3"]
}
```

#### Delete Section

```
DELETE /sections/{id}
```

### Section Photos

#### Get Section Photos

```
GET /sections/{id}/photos
```

#### Add Photos to Section

```
POST /sections/{id}/photos
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123", "pq8def456"]
}
```

#### Remove Photos from Section

```
DELETE /sections/{id}/photos
```

**Request:**
```json
{
  "photo_uids": ["pq8abc123"]
}
```

#### Update Photo Description

```
PUT /sections/{id}/photos/{photoUid}/description
```

**Request:**
```json
{
  "description": "Caption for print",
  "note": "Internal note"
}
```

### Pages

#### Create Page

```
POST /books/{id}/pages
```

**Request:**
```json
{
  "format": "4_landscape",
  "section_id": "section-uuid",
  "style": "modern",
  "split_position": 0.5
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `format` | string | Yes | - | `4_landscape`, `2l_1p`, `1p_2l`, `2_portrait`, `1_fullscreen` |
| `section_id` | string | No | null | Section UUID |
| `style` | string | No | `modern` | `modern` or `archival` |
| `split_position` | float | No | 0.5 | Column split ratio (0.0-1.0) for `2l_1p`/`1p_2l` formats |

#### Update Page

```
PUT /pages/{id}
```

**Request:**
```json
{
  "format": "2l_1p",
  "section_id": "section-uuid",
  "description": "Page title",
  "style": "archival",
  "split_position": 0.6
}
```

All fields are optional.

#### Reorder Pages

```
PUT /books/{id}/pages/reorder
```

**Request:**
```json
{
  "ids": ["page-uuid-1", "page-uuid-2", "page-uuid-3"]
}
```

#### Delete Page

```
DELETE /pages/{id}
```

### Slots

#### Assign Photo to Slot

```
PUT /pages/{id}/slots/{index}
```

**Request:**
```json
{
  "photo_uid": "pq8abc123"
}
```

#### Assign Text to Slot

```
PUT /pages/{id}/slots/{index}
```

**Request:**
```json
{
  "text_content": "# Heading\n\nParagraph text"
}
```

Supports GFM markdown: headings, bold, italic, lists, blockquotes, and tables. Tables use pipe syntax with optional column width percentages in the separator row (e.g., `|--- 60% ---|--- 40% ---|`).

#### Update Slot Crop Position

```
PUT /pages/{id}/slots/{index}/crop
```

**Request:**
```json
{
  "crop_x": 0.4,
  "crop_y": 0.6,
  "crop_scale": 0.5
}
```

| Field | Type | Required | Range | Description |
|-------|------|----------|-------|-------------|
| `crop_x` | float | Yes | 0.0-1.0 | Horizontal crop center (0.0 = left, 1.0 = right) |
| `crop_y` | float | Yes | 0.0-1.0 | Vertical crop center (0.0 = top, 1.0 = bottom) |
| `crop_scale` | float | No | 0.1-1.0 | Zoom level (1.0 = fill one axis, lower = zoom in). Defaults to 1.0 if omitted. |

**Response (200):**
```json
{
  "success": true
}
```

#### Swap Slots

```
POST /pages/{id}/slots/swap
```

**Request:**
```json
{
  "slot_a": 0,
  "slot_b": 2
}
```

Atomically swaps both photo assignments, crop positions, and crop scales.

#### Clear Slot

```
DELETE /pages/{id}/slots/{index}
```

Removes photo/text assignment and resets crop to defaults (0.5, 0.5, scale 1.0).

### Auto-Layout

#### Generate Pages from Section Photos

```
POST /books/{id}/sections/{sectionId}/auto-layout
```

Automatically generates pages with optimal format choices based on photo orientations in a section's unassigned pool.

**Request Body (all fields optional):**

| Field | Type | Description |
|-------|------|-------------|
| `prefer_formats` | `string[]` | Allowed page formats (default: all 5 formats) |
| `max_pages` | `number` | Maximum pages to create (default: unlimited) |

**Layout Algorithm Priority:**
1. 4 landscapes → `4_landscape` (2×2 grid)
2. 2 landscapes + 1 portrait → alternating `2l_1p` / `1p_2l`
3. 2 portraits → `2_portrait`
4. Remaining landscapes paired with portraits as `2_portrait`, else `1_fullscreen`
5. Remaining portraits → `1_fullscreen`

**Response:**

```json
{
  "pages_created": 3,
  "photos_placed": 9,
  "pages": [
    {
      "id": "page-uuid",
      "section_id": "section-uuid",
      "format": "4_landscape",
      "style": "modern",
      "sort_order": 1,
      "slots": [
        { "slot_index": 0, "photo_uid": "photo1", "crop_x": 0.5, "crop_y": 0.5, "crop_scale": 1.0 }
      ]
    }
  ]
}
```

### Preflight Check

#### Validate Book Before Export

```
GET /books/{id}/preflight
```

Runs validation checks on a book and returns a report of warnings and informational issues without generating a PDF. Use this before export to catch problems early.

**Checks performed:**

| Check | Severity | Description |
|-------|----------|-------------|
| Empty slots | Warning | Pages with unfilled slot positions |
| Low DPI | Warning | Photos with effective DPI < 200 at their assigned slot size |
| Empty sections | Warning | Sections with no pages |
| Unplaced photos | Info | Section photos not assigned to any page slot |
| Missing captions | Info | Photo slots without a description in section_photos |

**Response (200):**
```json
{
  "ok": false,
  "errors": [],
  "warnings": [
    { "type": "empty_slot", "page_number": 3, "section": "Summer", "slot_index": 2 },
    { "type": "low_dpi", "page_number": 5, "section": "Summer", "slot_index": 0, "photo_uid": "abc", "dpi": 185 },
    { "type": "empty_section", "section": "Winter" }
  ],
  "info": [
    { "type": "unplaced_photos", "section": "Summer", "count": 4 },
    { "type": "missing_captions", "count": 12 }
  ],
  "summary": { "total_pages": 20, "total_photos": 45, "filled_slots": 38, "total_slots": 52 }
}
```

`ok` is `true` when there are no warnings and no errors.

**Error Responses:**
| Status | Description |
|--------|-------------|
| 404 | Book not found |
| 500 | Failed to load book data |

---

### PDF Export

#### Export Book as PDF

```
GET /books/{id}/export-pdf
```

Generates and downloads a print-ready A4 landscape PDF of the book. Features include:
- 12-column grid layout with 3 fixed page zones (header/canvas/footer)
- Asymmetric mirrored margins for binding (inside 20mm, outside 12mm)
- Running headers: section title (verso), page description (recto)
- Numbered footer captions with marker overlays on multi-photo pages
- Modern (full-bleed) and archival (mat border/inset) photo styles per page
- Adjustable column split for mixed landscape/portrait formats
- Per-photo crop control for fine-tuned framing
- Section divider pages (24pt bold centered title)
- Text slots with auto-detected types (T1 explanation, T2 fact box, T3 oral history) and GFM table support
- Czech typography via polyglossia + EBGaramond font
- Effective DPI computation for print quality analysis

**Query Parameters:**

| Parameter | Value | Description |
|-----------|-------|-------------|
| `format` | `report` | Return JSON export report instead of PDF binary |
| `format` | `test` | Generate diagnostic test PDF (layout grid, font samples) |
| `format` | `debug` | Generate real book PDF with debug overlay (grid lines, slot borders) |

**Response (200 — default):** Binary PDF file with headers:
- `Content-Type: application/pdf`
- `Content-Disposition: attachment; filename="<book-title>.pdf"`
- `X-Export-Warnings: <count>` (only present when there are DPI warnings)

**Response (200 — `?format=report`):**
```json
{
  "book_title": "My Book",
  "page_count": 12,
  "photo_count": 25,
  "pages": [
    {
      "page_number": 1,
      "format": "divider",
      "section_title": "Childhood",
      "is_divider": true
    },
    {
      "page_number": 2,
      "format": "4_landscape",
      "section_title": "Childhood",
      "title": "Summer 1995",
      "is_divider": false,
      "photos": [
        {
          "photo_uid": "ps12345",
          "slot_index": 0,
          "effective_dpi": 312.5,
          "low_res": false
        }
      ]
    }
  ],
  "warnings": [
    "Page 5, slot 0 (ps67890): effective DPI 142 is below 200"
  ]
}
```

**Error Responses:**
| Status | Description |
|--------|-------------|
| 404 | Book not found |
| 500 | PDF generation failed (LaTeX error, photo download error) |
| 503 | `lualatex` not installed on server |

This endpoint is synchronous: the request blocks for the entire export (~4 min on large books) and then streams the PDF bytes. It remains for CLI / curl / MCP callers that want a single request. The web UI uses the asynchronous job flow below instead, so users see real-time progress.

#### Export Book as PDF (job-based, with progress)

The UI-facing flow splits export into a background job plus a separate
download so the browser can show two progress bars: one for server-side
generation (photo download + lualatex) and one for the actual file
transfer. The rendered PDF is kept on disk as a temp file — not in memory
— so a 700 MB book export costs the server ~0 resident memory per job.

Only one export per book can be running at a time; a second `POST` for the
same book returns `409 Conflict` with the existing `job_id` so the client
can reattach to the SSE stream.

**Start the job**

```
POST /books/{id}/export-pdf/job
```

Query parameters: `format=debug` to enable the debug overlay (same as the
synchronous endpoint). Returns `202 Accepted`:

```json
{
  "job_id": "9cac027d-7feb-43f4-8533-739e5556ab24",
  "book_id": "9797de58-a0ec-4330-8173-b7ce5b198f33",
  "book_title": "My Book",
  "status": "pending"
}
```

On conflict (`409`):
```json
{
  "error": "export already in progress for this book",
  "job_id": "<existing-job-id>",
  "status": "running"
}
```

**Get current job state**

```
GET /book-export/{jobId}
```

Returns the `BookExportJob` object (status, phase, counters, filename,
file_size, consumed flag, timestamps).

**Stream progress via SSE**

```
GET /book-export/{jobId}/events
```

Server-Sent Events stream. Emitted event types:

| Event        | Data payload |
|--------------|--------------|
| `status`     | Full `BookExportJob` snapshot (sent once on connect) |
| `started`    | `null` — job has begun |
| `progress`   | `{phase, current, total, photo_uid?}` — `phase` is one of `fetching_metadata`, `downloading_photos`, `compiling_pass1`, `compiling_pass2`. `current`/`total` are only meaningful during `downloading_photos`. |
| `completed`  | `{job_id, filename, file_size, download_url}` |
| `job_error`  | `{message}` |
| `cancelled`  | `null` |

The SSE connection closes once the job reaches a terminal state
(`completed` / `failed` / `cancelled`).

**Download the compiled PDF**

```
GET /book-export/{jobId}/download
```

Streams the temp file via `http.ServeContent`, which populates
`Content-Length` and supports range requests so the browser's
`fetch().body.getReader()` loop can report bytes-loaded in real time.
Headers:

- `Content-Type: application/pdf`
- `Content-Disposition: attachment; filename="<book-title>.pdf"`
- `X-Accel-Buffering: no` — disables reverse-proxy buffering so chunks
  flow through nginx/Caddy unbuffered (no-op when not behind a proxy).
- `Cache-Control: no-store`

After a successful download the job is marked `consumed=true` and its TTL
is shortened to **10 minutes** so a mid-download network blip can retry
without re-running the 4-minute export. Completed-but-unconsumed exports
live for **1 hour**; failed/cancelled jobs live for **5 minutes**. A
sweeper goroutine deletes expired jobs and their temp files.

| Status | Description |
|--------|-------------|
| 404 | Job not found |
| 409 | Job is not `completed` yet |
| 410 | Export file is no longer available (TTL elapsed) |

**Cancel the job**

```
DELETE /book-export/{jobId}
```

Cancels the job's context (which `SIGKILL`s any running `lualatex`
process), removes the temp file if present, and emits a `cancelled` SSE
event. Idempotent — returns `{"cancelled": true}` regardless of current
status.

#### Export Single Page as PDF

```
GET /pages/{id}/export-pdf
```

Generates a PDF of a single book page for quick preview. The output is pixel-identical to the corresponding page in the full book export — the page number, recto/verso side (and therefore mirrored margins and folio placement), and printer crop marks all match the page's actual position in the book. The position is computed by sorting all book pages via the same section/sort order used by the full book export and counting non-empty pages up to and including the target.

**Response (200):** Binary PDF file with headers:
- `Content-Type: application/pdf`
- `Content-Disposition: inline` (opens in browser PDF viewer)

**Error Responses:**
| Status | Description |
|--------|-------------|
| 404 | Page not found |
| 500 | PDF generation failed |
| 503 | `lualatex` not installed on server |

---

### ProcessJobResult

```typescript
interface ProcessJobResult {
  embed_success: number;
  embed_error: number;
  face_success: number;
  face_error: number;
  total_new_faces: number;
  total_embeddings: number;
  total_faces: number;
  total_face_photos: number;
}
```

---

## Text AI

AI-powered text operations for Czech text editing. Requires `OPENAI_TOKEN` to be configured.

### Check Text

Check Czech text for spelling, diacritics, and grammar issues using GPT-4.1-mini. Markdown syntax (headings, `**bold**`, `*italic*`, `^^small caps^^`, lists, blockquotes, GFM tables, alignment macros `->text<-` / `->text->`, horizontal rules) and special typography characters (`~` for non-breaking space, `\~` for literal tilde, backslash-escapes) are preserved verbatim and not flagged as errors.

```
POST /text/check
```

**Request:**
```json
{
  "text": "Fotografie z naseho domu v Veselici"
}
```

**Response (200):**
```json
{
  "corrected_text": "Fotografie z našeho domu ve Veselici",
  "readability_score": 85,
  "changes": [
    "naseho → našeho (diacritics)",
    "v Veselici → ve Veselici (preposition)"
  ]
}
```

**Error Responses:**
| Status | Description |
|--------|-------------|
| 400 | Missing or empty `text` field |
| 503 | `OPENAI_TOKEN` not configured |

### Rewrite Text

Rewrite Czech text to a target length using GPT-4.1-mini. Existing markdown structure (headings, lists, tables, blockquotes, alignment macros) and special typography characters (`~`, `\~`) are preserved in place — the model only adjusts the prose inside them.

```
POST /text/rewrite
```

**Request:**
```json
{
  "text": "Tato fotografie zachycuje pohled na náš dům v zimním období roku 1985.",
  "target_length": "shorter"
}
```

**Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `text` | string | Yes | Czech text to rewrite |
| `target_length` | string | Yes | One of: `much_shorter`, `shorter`, `longer`, `much_longer` |

**Response (200):**
```json
{
  "rewritten_text": "Náš dům v zimě 1985."
}
```

**Error Responses:**
| Status | Description |
|--------|-------------|
| 400 | Missing `text` or invalid `target_length` |
| 503 | `OPENAI_TOKEN` not configured |

### Check Text Consistency

Analyze style consistency across multiple Czech texts (e.g., all texts in a book). Returns a consistency score, detected tone, and specific issues. Markdown formatting and special typography characters are ignored for the analysis — only the Czech prose is judged for tone, register, and style.

```
POST /text/consistency
```

**Request:**
```json
{
  "texts": [
    { "id": "section1:photo1", "content": "Pohled na dům v zimě roku 1985." },
    { "id": "section1:photo2", "content": "Krásný letní den u rybníka." }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `texts` | array | Yes | At least 2 text entries with `id` and `content` |

**Response (200):**
```json
{
  "consistency_score": 78,
  "tone": "nostalgický, popisný",
  "issues": [
    "Nekonzistentní použití času: první text v minulém čase, druhý v přítomném."
  ],
  "cost_czk": 0.12,
  "cached": false
}
```

**Error Responses:**
| Status | Description |
|--------|-------------|
| 400 | Fewer than 2 texts provided |
| 503 | `OPENAI_TOKEN` not configured |

### Check Text and Save Result

Run AI text check and persist the result to the database for status tracking.

```
POST /text/check-and-save
```

**Request:**
```json
{
  "text": "Fotografie z naseho domu",
  "source_type": "section_photo",
  "source_id": "abc123:pq8def456",
  "field": "description"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `text` | string | Yes | Czech text to check |
| `source_type` | string | Yes | Source type (`section_photo` or `page_slot`) |
| `source_id` | string | Yes | Source identifier (format depends on type) |
| `field` | string | Yes | Field name (`description`, `note`, or `text_content`) |

**Response (200):**
```json
{
  "corrected_text": "Fotografie z našeho domu",
  "readability_score": 90,
  "changes": ["naseho → našeho (diacritics)"],
  "cost_czk": 0.05,
  "cached": false,
  "status": "has_errors",
  "content_hash": "a1b2c3...",
  "checked_at": "2025-03-31T10:00:00Z"
}
```

### Get Book Text Check Status

Get the text check status for all texts in a book. Returns check results keyed by `source_type:source_id:field`.

```
GET /books/{id}/text-check-status
```

**Response (200):**
```json
{
  "section_photo:abc123:pq8def456:description": {
    "status": "has_errors",
    "readability_score": 85,
    "checked_at": "2025-03-31T10:00:00Z",
    "is_stale": false,
    "corrected_text": "Fotografie z našeho domu",
    "changes": ["naseho → našeho"]
  },
  "page_slot:page1:0:text_content": {
    "status": "clean",
    "readability_score": 95,
    "checked_at": "2025-03-31T09:30:00Z",
    "is_stale": true
  }
}
```

The `is_stale` flag indicates the text content has changed since the last check (content hash mismatch). Results with `status: "clean"` omit `corrected_text` and `changes`.

---

## Text Version History

Automatic version history tracking for text fields in photo books. Versions are created when text content changes.

### List Text Versions

```
GET /text-versions
```

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `source_type` | string | Yes | Source type (`section_photo` or `page_slot`) |
| `source_id` | string | Yes | Source identifier |
| `field` | string | Yes | Field name (`description`, `note`, or `text_content`) |

**Response (200):**
```json
[
  {
    "id": 42,
    "content": "Previous text content",
    "changed_by": "user",
    "created_at": "2025-03-31T09:00:00Z"
  },
  {
    "id": 41,
    "content": "Even older text",
    "changed_by": "ai_rewrite",
    "created_at": "2025-03-30T15:00:00Z"
  }
]
```

Returns up to 20 most recent versions, newest first.

### Restore Text Version

Restore a previous version. Saves the current text as a new version before restoring.

```
POST /text-versions/{id}/restore
```

**Response (200):**
```json
{
  "content": "Restored text content"
}
```

**Error Responses:**
| Status | Description |
|--------|-------------|
| 400 | Invalid version ID |
| 404 | Version not found |

---

## MCP Server

The MCP (Model Context Protocol) server is integrated into the `serve` command. When `MCP_API_TOKEN` is set, MCP endpoints are mounted at `/mcp/sse` and `/mcp/message` on the same HTTP server. If the token is not set, MCP routes are not registered.

**Endpoints:** `/mcp/sse` (SSE connection), `/mcp/message` (message posting)

**Transport:** HTTP SSE (not stdio)

**Authentication:** Bearer token via `MCP_API_TOKEN` environment variable. Clients must send `Authorization: Bearer <token>` header.

**Server name:** `photo-sorter-books`

### MCP Tools — Books

| Tool | Description | Parameters |
|------|-------------|------------|
| `list_books` | List all photo books | (none) |
| `get_book` | Get book detail with chapters, sections, pages | `book_id` (string, required) |
| `create_book` | Create a new book | `title` (string, required), `description` (string, optional) |
| `update_book` | Update book title/description | `book_id` (string, required), `title` (string, optional), `description` (string, optional) |
| `delete_book` | Delete a book and all its content | `book_id` (string, required) |

### MCP Tools — Chapters

| Tool | Description | Parameters |
|------|-------------|------------|
| `create_chapter` | Create a chapter in a book | `book_id` (string, required), `title` (string, required), `color` (string, optional — hex like `#8B0000`) |
| `update_chapter` | Update chapter title/color | `chapter_id` (string, required), `title` (string, optional), `color` (string, optional) |
| `delete_chapter` | Delete a chapter | `chapter_id` (string, required) |
| `reorder_chapters` | Reorder chapters in a book | `book_id` (string, required), `chapter_ids` (array of strings, required) |

### MCP Tools — Sections

| Tool | Description | Parameters |
|------|-------------|------------|
| `create_section` | Create a section in a book | `book_id` (string, required), `title` (string, required), `chapter_id` (string, optional) |
| `update_section` | Update section title/chapter | `section_id` (string, required), `title` (string, optional), `chapter_id` (string, optional) |
| `delete_section` | Delete a section | `section_id` (string, required) |
| `reorder_sections` | Reorder sections in a book | `book_id` (string, required), `section_ids` (array of strings, required) |
| `list_section_photos` | List photos in a section | `section_id` (string, required) |
| `add_photos_to_section` | Add photos to a section | `section_id` (string, required), `photo_uids` (array of strings, required) |
| `remove_photos_from_section` | Remove photos from a section | `section_id` (string, required), `photo_uids` (array of strings, required) |
| `update_section_photo` | Update photo description/note | `section_id` (string, required), `photo_uid` (string, required), `description` (string, optional), `note` (string, optional) |

### MCP Tools — Pages & Slots

| Tool | Description | Parameters |
|------|-------------|------------|
| `create_page` | Create a page in a book | `book_id` (string, required), `format` (string, required — `4_landscape`, `2l_1p`, `1p_2l`, `2_portrait`, `1_fullscreen`), `section_id` (string, optional), `style` (string, optional — `modern`, `archival`) |
| `update_page` | Update page format/section/description | `page_id` (string, required), `format` (string, optional), `section_id` (string, optional), `description` (string, optional), `style` (string, optional), `split_position` (number, optional — 0.2-0.8) |
| `delete_page` | Delete a page and all slots | `page_id` (string, required) |
| `reorder_pages` | Reorder pages in a book | `book_id` (string, required), `page_ids` (array of strings, required) |
| `assign_photo_to_slot` | Assign a photo to a page slot | `page_id` (string, required), `slot_index` (number, required), `photo_uid` (string, required) |
| `assign_text_to_slot` | Assign markdown text to a slot | `page_id` (string, required), `slot_index` (number, required), `text_content` (string, required) |
| `clear_slot` | Clear a page slot | `page_id` (string, required), `slot_index` (number, required) |
| `swap_slots` | Swap two slots on a page | `page_id` (string, required), `slot_a` (number, required), `slot_b` (number, required) |
| `update_slot_crop` | Update crop position and zoom | `page_id` (string, required), `slot_index` (number, required), `crop_x` (number, required — 0.0-1.0), `crop_y` (number, required — 0.0-1.0), `crop_scale` (number, optional — 0.1-1.0) |

### MCP Tools — Photos

| Tool | Description | Parameters |
|------|-------------|------------|
| `list_photos` | List photos with filtering/pagination | `query` (string, optional — supports `label:`, `person:`, `year:`), `count` (number, optional — default 20, max 100), `offset` (number, optional) |
| `get_photo` | Get photo metadata (title, date, GPS, camera, faces, labels) | `photo_uid` (string, required) |
| `get_photo_thumbnail` | Get base64-encoded JPEG thumbnail | `photo_uid` (string, required), `size` (string, optional — `fit_720`, `fit_1280`, `fit_2048`, `tile_500`, `tile_224`) |
| `update_photo` | Update photo metadata | `photo_uid` (string, required), `title` (string, optional), `description` (string, optional), `taken_at` (string, optional), `favorite` (boolean, optional), `private` (boolean, optional), `lat` (number, optional), `lng` (number, optional) |
| `get_photo_faces` | Get face markers with positions and names | `photo_uid` (string, required) |
| `find_similar_photos` | Find visually similar photos using CLIP embeddings | `photo_uid` (string, required), `count` (number, optional — default 10), `max_distance` (number, optional — default 0.3), `book_id` (string, optional — include book placement info), `exclude_album` (string, optional), `exclude_label` (string, optional) |
| `search_photos_by_text` | Search photos by text description (auto-translates Czech) | `query` (string, required), `count` (number, optional — default 10), `max_distance` (number, optional — default 0.5) |

### MCP Tools — Albums

| Tool | Description | Parameters |
|------|-------------|------------|
| `list_albums` | List PhotoPrism albums | `type` (string, optional — `album`, `folder`, `moment`, `month`, `state`), `order` (string, optional), `query` (string, optional), `count` (number, optional — default 50, max 500), `offset` (number, optional) |
| `get_album` | Get album details by UID | `album_uid` (string, required) |
| `create_album` | Create a new album | `title` (string, required) |
| `get_album_photos` | Get photos in an album with pagination | `album_uid` (string, required), `count` (number, optional — default 50, max 500), `offset` (number, optional) |
| `add_photos_to_album` | Add photos to an album | `album_uid` (string, required), `photo_uids` (array of strings, required) |
| `remove_photos_from_album` | Remove photos from an album | `album_uid` (string, required), `photo_uids` (array of strings, required) |

### MCP Tools — Labels

| Tool | Description | Parameters |
|------|-------------|------------|
| `list_labels` | List PhotoPrism labels | `count` (number, optional — default 100, max 1000), `offset` (number, optional), `all` (boolean, optional — include empty labels) |
| `get_label` | Get label details by UID | `label_uid` (string, required) |
| `update_label` | Update label properties | `label_uid` (string, required), `name` (string, optional), `description` (string, optional), `notes` (string, optional), `priority` (number, optional), `favorite` (boolean, optional) |
| `delete_labels` | Delete labels by UIDs | `label_uids` (array of strings, required) |
| `add_photo_label` | Add label to a photo | `photo_uid` (string, required), `label_uid` (string, required), `uncertainty` (number, optional — 0-100), `priority` (number, optional) |
| `remove_photo_label` | Remove label from a photo | `photo_uid` (string, required), `label_id` (number, required) |

### MCP Tools — Text & AI

| Tool | Description | Parameters |
|------|-------------|------------|
| `check_text` | AI text check (spelling, grammar, diacritics for Czech) | `text` (string, required), `source_type` (string, optional — for persistence), `source_id` (string, optional), `field` (string, optional) |
| `rewrite_text` | AI text rewrite (length adjustment) | `text` (string, required), `target_length` (string, required — `much_shorter`, `shorter`, `longer`, `much_longer`) |
| `check_consistency` | AI style consistency check across all book texts | `book_id` (string, required) |
| `list_text_versions` | List version history for a text field | `source_type` (string, required), `source_id` (string, required), `field` (string, required) |
| `restore_text_version` | Restore a previous text version | `version_id` (number, required) |
