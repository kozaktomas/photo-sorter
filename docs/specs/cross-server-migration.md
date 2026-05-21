# Cross-server migration with existing photo-sorter data

This runbook covers the case where photo-sorter has been deployed for
some time against a PhotoPrism instance (so the native Postgres database
already holds books, embeddings, faces, and other rows keyed on
PhotoPrism photo UIDs) and the operator wants to move everything to a
new host while also retiring PhotoPrism.

The end state is a single photo-sorter binary that owns its photos,
people, labels, and books — no PhotoPrism, no MariaDB. PhotoPrism's
photo UIDs are preserved as the native UIDs so historic references in
`embeddings`, `faces`, `section_photos`, `page_slots`, etc. keep working
without a rewrite.

## Recommended workflow

1. **Old host — export the existing photo-sorter Postgres database.**
   ```bash
   photo-sorter db-export -o sorter.sql.gz
   ```
   This snapshot includes every book, embedding, face, marker, and label
   the operator has built up over time. Photos table rows in this dump
   currently reference PhotoPrism photo UIDs.

2. **New host — install a fresh photo-sorter and import the dump.**
   ```bash
   photo-sorter db-import -i sorter.sql.gz
   ```
   At this point the new host has all the auxiliary data but no
   originals on disk.

3. **rsync the PhotoPrism originals tree to the new host.** Drop them
   under `STORAGE_ORIGINALS_PATH` (or any temporary path; the migrator
   reads from `--pp-originals`).

4. **Make PhotoPrism's MariaDB reachable from the new host.** Either
   open the port directly or set up an SSH tunnel; the migrator only
   reads from it.

5. **Run the migrator with `--emit-photo-map`.**
   ```bash
   photo-sorter migrate-from-photoprism \
     --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
     --pp-originals /path/to/rsynced/originals \
     --uploader-username admin \
     --emit-photo-map /tmp/photo-map.json
   ```
   The migrator preserves PhotoPrism UIDs (photos, albums, subjects,
   markers) verbatim, so the emitted `photo_uid_map` is an identity
   mapping in the happy path. Existing rows in `embeddings`,
   `section_photos`, etc. continue to find their photos without a
   remap.

6. **(Only if the operator previously ran a buggy version of the
   migrator that wrote generated UIDs)** run the remap pass:
   ```bash
   photo-sorter migrate-remap-references --map /tmp/photo-map.json --dry-run
   photo-sorter migrate-remap-references --map /tmp/photo-map.json --yes
   ```
   In the happy path the command detects the identity map and exits
   immediately. For an operator coming from the buggy version, they
   should hand-edit the JSON to add `(old_generated_uid → preserved_pp_uid)`
   pairs and re-run.

7. **Verify.** Open the photo-sorter web UI and confirm that an
   existing book renders every page, the section photo pool still
   shows its cover image, and a previously-clustered face still shows
   the same subject. Also run:
   ```bash
   photo-sorter migrate-verify \
     --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
     --pp-originals /path/to/rsynced/originals
   ```

8. **Cut PhotoPrism out of the compose file.** Once the verify pass is
   clean and the web UI looks healthy, remove the PhotoPrism +
   MariaDB containers and restart photo-sorter.

## Background

The migrator inserts rows into `photos` with `uid = PhotoPrism.photo_uid`
verbatim (see `internal/migrate/stage_photos.go:buildPhotoRecord`). The
same preservation applies to `subjects.uid`, `albums.uid`, and
`markers.uid`. PhotoPrism's label rows are keyed on an integer ID rather
than a UID, so labels get freshly generated native UIDs; no other table
references PhotoPrism's label IDs, so this is benign.

The `--emit-photo-map` file is still emitted in the happy path because:

- The dry-run remap pass can verify there is nothing to remap (the file
  is an identity map), giving the operator a positive "nothing to do"
  signal rather than a silent skip.
- An operator who lands a buggy version of the migrator first can take
  the file as a starting point, fill in `(old_generated_uid →
  preserved_pp_uid)` pairs, and feed it back into the remap command.

The remap command (`internal/migrate/remap.go`) updates the following
soft-FK columns inside one Postgres transaction:

| Table | Column |
|-------|--------|
| `embeddings` | `photo_uid` |
| `faces` | `photo_uid` |
| `faces_processed` | `photo_uid` |
| `markers` | `photo_uid` |
| `album_photos` | `photo_uid` |
| `photo_labels` | `photo_uid` |
| `photo_phashes` | `photo_uid` |
| `section_photos` | `photo_uid` |
| `page_slots` | `photo_uid` |

`photo_phashes` carries a FK to `photos(uid)` ON DELETE CASCADE; the
remap pass never updates `photos.uid` directly (only the soft references
above), so the CASCADE constraint never fires.

After the UPDATEs, an integrity audit counts rows per target whose
`photo_uid` no longer matches any `photos.uid`. Non-zero is a warning,
not an error — some orphans may exist for unrelated reasons (e.g.
deleted PhotoPrism originals).
