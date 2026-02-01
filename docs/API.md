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
- `face_index < 0`: Unmatched PhotoPrism marker (no embedding)
- `embeddings_count` vs `markers_count` surfaces discrepancies

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
  "embeddings_writable": true
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
