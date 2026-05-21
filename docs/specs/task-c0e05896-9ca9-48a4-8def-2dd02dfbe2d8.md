# Docker compose: drop PhotoPrism + MariaDB

Once the photoprism package is gone (previous task), strip the unused services from the docker-compose files and the dev script. The final compose stack should be: `photo-sorter` + `pgvector` + `embeddings` (CLIP + faces).

## Requirements

### Files

- `docker-compose.yml` (production)
- `docker-compose.test.yml` (testing, if it exists)
- `dev.sh`

### docker-compose.yml

Remove the `photoprism` service block (and its `PHOTOPRISM_*` env section) and the `mariadb` block (was only used by PhotoPrism).

Verify the named volumes for PhotoPrism (`/photoprism/originals`, `/photoprism/storage`) are NOT removed before the user has confirmed the backup is good — keep them declared but unused for now, with a `# TODO: remove after backup verified` comment. The cleanup task can also keep the volume declarations as orphans; the docker-compose down won't delete them by default.

Mount the new sorter storage paths into the `photo-sorter` container:
- `${STORAGE_ORIGINALS_PATH:-./data/originals}:/data/originals`
- `${STORAGE_CACHE_PATH:-./data/cache}:/data/cache`

Add `exiftool`, `libheif-tools`, `dcraw` to the photo-sorter `Dockerfile` (system packages on the runtime stage):

```Dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    exiftool libheif-examples dcraw \
 && rm -rf /var/lib/apt/lists/*
```

(`libheif-examples` provides `heif-convert` on Debian.)

### dev.sh

`dev.sh` currently starts PhotoPrism and MariaDB via the test compose. Strip those starts; only start pgvector and the embeddings service. Update the readiness checks to remove PhotoPrism / MariaDB polling.

### CLAUDE.md

Update the "Development Environment" section to remove the PhotoPrism URL + MariaDB references. Add the new originals/cache paths + external tools mention.

### Tests

`make test` must keep passing. The testcontainers setup did not depend on PhotoPrism in most tests; ones that did already moved to native repositories in earlier tasks.

### Manual sanity

- `docker compose down && docker compose up -d` → photoprism + mariadb containers are gone, sorter starts and serves the UI.

## Verification

- `make build` succeeds.
- `make test` passes.
- `docker compose config` validates with no `photoprism` or `mariadb` services.