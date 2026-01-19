# Photo Match Command

Find all photos containing a specific person by comparing face embeddings stored in PostgreSQL.

## Usage

```bash
go run . photo match <person-name> [flags]
```

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--threshold` | float | 0.5 | Maximum cosine distance for face matching |
| `--limit` | int | 0 | Limit number of results (0 = no limit) |
| `--json` | bool | false | Output as JSON |
| `--apply` | bool | false | Apply changes to PhotoPrism (create markers and assign person) |
| `--dry-run` | bool | false | Preview changes without applying them (use with --apply) |

## How It Works

1. **Fetch source photos** - Queries PhotoPrism for photos tagged with the person using `q=person:<name>`
2. **Get source embeddings** - Retrieves face embeddings for source photos from PostgreSQL
3. **Search similar faces** - Uses pgvector cosine distance to find similar faces across all photos
4. **Filter by match count** - Only keeps faces that match at least 10% of the source embeddings
5. **Check markers** - For each match, fetches existing markers from PhotoPrism
6. **Determine action** - Compares bounding boxes (IoU) to determine what action is needed

### Match Count Requirement

A face is only considered a match if it is similar (below threshold) to **at least 10%** of the source face embeddings. This reduces false positives by requiring consistency across multiple reference photos.

For example:
- 100 source embeddings → candidate must match at least 10
- 20 source embeddings → candidate must match at least 2
- 5 source embeddings → candidate must match at least 1

## Understanding the Threshold

The `--threshold` parameter controls the **maximum cosine distance** for face matching. This is a critical parameter that affects both precision and recall.

### Cosine Distance vs Cosine Similarity

- **Cosine similarity** ranges from -1 to 1 (1 = identical, 0 = orthogonal, -1 = opposite)
- **Cosine distance** = 1 - cosine similarity, ranges from 0 to 2
- Lower distance = more similar faces

### Threshold Guidelines

| Threshold | Behavior | Use Case |
|-----------|----------|----------|
| 0.2 - 0.3 | Very strict | High confidence matches only, minimal false positives |
| 0.3 - 0.4 | Strict | Good balance for well-lit photos with clear faces |
| 0.4 - 0.5 | Moderate (default) | General use, may include some false positives |
| 0.5 - 0.6 | Loose | Catches more matches but increases false positives |
| 0.6+ | Very loose | Not recommended, high false positive rate |

### Factors Affecting Distance

Face embeddings can vary due to:
- **Lighting conditions** - Shadows, backlighting, flash
- **Pose angle** - Front-facing vs profile
- **Image quality** - Resolution, blur, compression
- **Age differences** - Same person at different ages
- **Occlusions** - Glasses, hats, masks
- **Expression** - Smiling vs neutral

### Recommended Workflow

1. **Start strict** - Use `--threshold 0.3` initially
2. **Review results** - Check if matches are accurate
3. **Adjust as needed** - Increase threshold if missing valid matches, decrease if seeing false positives

```bash
# Strict matching - high confidence only
go run . photo match tomas-kozak --threshold 0.3

# Default matching
go run . photo match tomas-kozak

# Loose matching - catch more but review carefully
go run . photo match tomas-kozak --threshold 0.6
```

## Output Actions

For each matched photo, the command determines what action is needed:

| Action | Description |
|--------|-------------|
| `create_marker` | No marker exists in PhotoPrism at the face location - need to create one |
| `assign_person` | Marker exists but no person assigned - just assign the person |
| `already_done` | Marker exists with person already assigned - no action needed |

### Marker Matching

The command uses **IoU (Intersection over Union)** to match our detected face bounding box with existing PhotoPrism markers:

- IoU >= 0.3 is considered a match
- The marker with the highest IoU is selected
- If no marker has IoU >= 0.3, action is `create_marker`

## Examples

### Basic Usage

```bash
# Find photos matching a person
go run . photo match tomas-kozak
```

Output:
```
Connecting to PhotoPrism...
Connecting to PostgreSQL...
Searching for photos with query: person:tomas-kozak
Found 15 source photos for tomas-kozak
Found 18 face embeddings from source photos
Searching for similar faces (threshold: 0.50)...
Fetching marker info for 42 matches...

Found 42 photos matching tomas-kozak:

PHOTO UID       DISTANCE  ACTION          MARKER UID       MARKER NAME  IoU
---------       --------  ------          ----------       -----------  ---
pq8abc123def    0.2134    assign_person   mq8xyz789ghi     -            0.85
pq8def456ghi    0.2567    create_marker   -                -            -
pq8ghi789jkl    0.3012    already_done    mq8abc456def     tomas-kozak  0.92

Summary:
  Create marker:  12
  Assign person:  25
  Already done:   5
```

### JSON Output

```bash
go run . photo match tomas-kozak --json --limit 10
```

```json
{
  "person": "tomas-kozak",
  "source_photos": 15,
  "source_faces": 18,
  "matches": [
    {
      "photo_uid": "pq8abc123def",
      "distance": 0.2134,
      "face_index": 0,
      "bbox": [125.5, 80.2, 245.8, 230.1],
      "action": "assign_person",
      "marker_uid": "mq8xyz789ghi",
      "iou": 0.85
    }
  ],
  "summary": {
    "create_marker": 3,
    "assign_person": 5,
    "already_done": 2
  }
}
```

## Applying Changes

Use `--apply` to create markers and assign the person in PhotoPrism. Use `--dry-run` to preview what would be done.

### Dry Run (Preview)

```bash
go run . photo match tomas-kozak --apply --dry-run
```

Output:
```
[DRY-RUN] Would apply changes to 37 photos:
  [DRY-RUN] Would create marker for pq8abc123def and assign tomas-kozak
  [DRY-RUN] Would assign tomas-kozak to marker mq8xyz789ghi on pq8def456ghi
  ...
```

### Apply Changes

```bash
go run . photo match tomas-kozak --apply
```

Output:
```
Applying changes to 37 photos...
  Created marker mq8new123abc for pq8abc123def
  Assigned tomas-kozak to marker mq8xyz789ghi on pq8def456ghi
  ...

Applied: 35, Errors: 2
```

### What Gets Applied

| Action | What Happens |
|--------|--------------|
| `create_marker` | Creates a new face marker at the detected location and assigns the person |
| `assign_person` | Updates existing marker to assign the person name |
| `already_done` | Skipped - marker already has the person assigned |

### Error Handling

- If marker creation fails, the error is logged and the photo is skipped
- If marker update fails, the error is logged and the photo is skipped
- Use `--json` to get detailed error information in the output

## Prerequisites

1. **Face embeddings computed** - Run `photo faces` first to detect faces and store embeddings
2. **Person tagged in PhotoPrism** - At least some photos must be tagged with the person name
3. **PostgreSQL with pgvector** - Required for similarity search
