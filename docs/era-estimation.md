# Era Estimation

Photo era estimation uses CLIP cross-modal embeddings to predict when a photo was taken based on its visual characteristics. It compares a photo's CLIP image embedding against pre-computed text embedding centroids for 16 historical eras.

## How It Works

### 1. Era Centroid Computation (`cache compute-eras`)

For each of 16 eras (1900s through 2025-2029), the system:

1. Generates 20 descriptive text prompts per era (e.g., "A photograph from the 1980s with typical grain and color palette")
2. Computes CLIP text embeddings (768-dim) for each prompt via `POST /embed/text`
3. Averages all 20 embeddings into a single centroid vector
4. L2-normalizes the centroid
5. Stores the result in the `era_embeddings` PostgreSQL table

```bash
# Compute and store all era centroids
go run . cache compute-eras

# Preview without saving
go run . cache compute-eras --dry-run
```

### 2. Photo Embedding

Each photo needs a CLIP image embedding (768-dim) computed via the processing pipeline. This happens during `POST /api/v1/process` or `go run . cache sync`.

### 3. Similarity Comparison

When a user views a photo, the `GET /api/v1/photos/:uid/estimate-era` endpoint:

1. Fetches the photo's 768-dim CLIP image embedding from PostgreSQL
2. Loads all era centroids from the `era_embeddings` table
3. Computes cosine similarity between the photo embedding and each era centroid
4. Returns all eras sorted by similarity (highest first)

## Eras

| Era Slug | Era Name | Representative Date |
|----------|----------|-------------------|
| 1900s | 1900s (1900-1909) | 1905-01-01 |
| 1910s | 1910s (1910-1919) | 1915-01-01 |
| 1920s | 1920s (1920-1929) | 1925-01-01 |
| 1930s | 1930s (1930-1939) | 1935-01-01 |
| 1940s | 1940s (1940-1949) | 1945-01-01 |
| 1950s | 1950s (1950-1959) | 1955-01-01 |
| 1960s | 1960s (1960-1969) | 1965-01-01 |
| 1970s | 1970s (1970-1979) | 1975-01-01 |
| 1980s | 1980s (1980-1989) | 1985-01-01 |
| 1990s | 1990s (1990-1999) | 1995-01-01 |
| 2000-2004 | 2000-2004 | 2002-06-15 |
| 2005-2009 | 2005-2009 | 2007-06-15 |
| 2010-2014 | 2010-2014 | 2012-06-15 |
| 2015-2019 | 2015-2019 | 2017-06-15 |
| 2020-2024 | 2020-2024 | 2022-06-15 |
| 2025-2029 | 2025-2029 | 2027-06-15 |

Eras from 2000 onward use 5-year ranges for finer resolution. Earlier decades use 10-year ranges.

## Database Schema

```sql
CREATE TABLE era_embeddings (
    era_slug VARCHAR(64) PRIMARY KEY,
    era_name VARCHAR(255) NOT NULL,
    representative_date DATE NOT NULL,
    prompt_count INTEGER NOT NULL DEFAULT 20,
    embedding VECTOR(768) NOT NULL,
    model VARCHAR(64) NOT NULL,
    pretrained VARCHAR(64) NOT NULL,
    dim INTEGER NOT NULL DEFAULT 768,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
```

## API

### `GET /api/v1/photos/:uid/estimate-era`

Estimates the era of a photo by comparing its CLIP image embedding against all era centroids.

**Response (200):**

```json
{
  "photo_uid": "pq8abc123def",
  "best_match": {
    "era_slug": "2015-2019",
    "era_name": "2015-2019",
    "representative_date": "2017-06-15T00:00:00Z",
    "similarity": 0.251,
    "confidence": 25.1
  },
  "top_matches": [
    { "era_slug": "2015-2019", "era_name": "2015-2019", "similarity": 0.251, "confidence": 25.1 },
    { "era_slug": "2020-2024", "era_name": "2020-2024", "similarity": 0.240, "confidence": 24.0 },
    { "era_slug": "1920s", "era_name": "1920s (1920-1929)", "similarity": 0.08, "confidence": 8.0 }
  ]
}
```

- `similarity` — cosine similarity (0-1) between the photo and era embeddings
- `confidence` — `similarity * 100`, capped at 100

**Error responses:**

| Status | Condition |
|--------|-----------|
| 404 | Photo has no CLIP image embedding |
| 503 | Era embeddings not available (centroids not computed or database not initialized) |

## UI

The era estimate is displayed in the **Photo Detail** page (`/photos/:uid`) right sidebar, below the Faces header.

**Collapsed state (default):**
- Shows the best-matching era name and confidence percentage
- A chevron icon indicates the section is expandable

**Expanded state (click to toggle):**
- Shows all eras ranked by similarity
- Each era has a proportional horizontal bar and percentage
- The best match is highlighted in blue; others are gray

The component renders nothing if the photo has no embedding or era centroids haven't been computed.

## Architecture

```
cmd/cache_compute_eras.go          # CLI command to compute era centroids
internal/database/types.go         # StoredEraEmbedding struct
internal/database/repository.go    # EraEmbeddingReader/Writer interfaces
internal/database/provider.go      # GetEraEmbeddingReader provider
internal/database/postgres/
  era_embeddings.go                # PostgreSQL implementation
  migrations/
    007_create_era_embeddings.sql   # Table migration
internal/web/handlers/photos.go    # EstimateEra handler
internal/web/routes.go             # GET /photos/{uid}/estimate-era route
web/src/api/client.ts              # estimateEra() API function
web/src/types/index.ts             # EraMatch, EraEstimateResponse types
web/src/pages/PhotoDetail/
  EraEstimate.tsx                  # React component
  index.tsx                        # Integration point
```

## Limitations

- **Low absolute confidence values** — CLIP cross-modal similarity (image vs text) produces lower raw cosine similarity scores (typically 10-30%) compared to same-modality comparisons. The relative ranking between eras is more meaningful than the absolute percentages.
- **Visual bias** — The model estimates based on visual characteristics (film grain, color palette, clothing, resolution) rather than actual date metadata. A modern photo styled to look vintage may be classified as an older era.
- **Centroid quality** — Results depend on the quality and diversity of the 20 text prompts per era. Re-running `cache compute-eras` with improved prompts will update the centroids.
