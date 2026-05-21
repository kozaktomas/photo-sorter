# Plug Subject / Label / Album Data-Loss Gaps in migrate-from-photoprism

An audit found that several non-photo entities lose rich metadata during migration. Subjects keep only their name. Labels keep only name/slug/priority. Albums keep only title/description. This task widens the native schema and updates the relevant migrator stages to preserve the rest. See the sibling task on photo-level gaps for the same idempotency pattern.

## Requirements

### 1. Schema migration
Add a new migration file under `internal/database/postgres/migrations/` (next free three-digit prefix at apply time).

- Table `subjects`:
  - `bio TEXT NOT NULL DEFAULT ''`
  - `about TEXT NOT NULL DEFAULT ''`
  - `alias TEXT NOT NULL DEFAULT ''` (PhotoPrism subjects can have an alternate-name "alias")
- Table `labels`:
  - `description TEXT NOT NULL DEFAULT ''`
  - `categories TEXT[] NOT NULL DEFAULT '{}'` (PhotoPrism stores this as comma-separated in `label_categories`; unpack to array)
- Table `albums`:
  - `location TEXT NOT NULL DEFAULT ''` (PhotoPrism `album_location` — a free-form place string)
  - `category TEXT NOT NULL DEFAULT ''` (PhotoPrism `album_category`)
  - `notes TEXT NOT NULL DEFAULT ''` (PhotoPrism `album_notes`)
  - `filter TEXT NOT NULL DEFAULT ''` (PhotoPrism `album_filter` — the smart-album query DSL string)
  - `album_order TEXT NOT NULL DEFAULT ''` (PhotoPrism `album_order` — sort order setting, e.g. `oldest` / `newest`)
- Use `ADD COLUMN IF NOT EXISTS` for every column.

### 2. Go model + repository updates
- Subject, Label, Album structs (look in `internal/database/types.go` and any native repository files): add corresponding fields (`Bio`, `About`, `Alias` on Subject; `Description`, `Categories []string` on Label; `Location`, `Category`, `Notes`, `Filter`, `Order` on Album).
- Update `internal/database/postgres/{subjects,labels,albums}.go` SELECT / INSERT / UPDATE statements to include the new columns.
- Update repository interface methods so callers can read and write the new fields.
- Update the native REST handlers (`internal/web/handlers/subjects.go`, `labels.go`, `albums.go`) to:
  - Include the new fields in GET responses.
  - Accept them on PUT/POST where appropriate (subject rename already supports name; extend to bio/about/alias; label PUT already updates name/priority/favorite — extend with description/categories; album POST/PUT — extend with location/category/notes/filter/order).
  - Validate: bio/about/notes capped at 8 KiB each; alias capped at 256 chars; categories array length capped at 50.

### 3. Migrator stage updates
For each of `internal/migrate/stage_subjects.go`, `stage_labels.go`, `stage_albums.go`:

- Extend the source SELECT to pull the relevant PhotoPrism columns (`subj_bio`, `subj_about`, `subj_alias`; `label_description`, `label_categories`; `album_location`, `album_category`, `album_notes`, `album_filter`, `album_order`).
- For comma-separated source columns like `label_categories`: trim whitespace, drop empties, deduplicate.
- For `album_filter`: store the raw DSL string verbatim. **Do not** attempt to evaluate or translate it — photo-sorter has no smart-album evaluator yet. The string is preserved so a future "smart albums" feature can consume it, and so the operator can audit which albums were smart-filtered in PhotoPrism. Add a comment in the migrator noting this is informational-only for now.

Idempotency (same pattern as the photo-level gap-fix task):
- If a row already exists in the destination, do NOT skip it. UPDATE the new columns ONLY when the destination value is the default zero-value AND the source has a non-default value. This way:
  - First-run migration: writes everything.
  - Re-run after this task lands: backfills the new columns on rows migrated by the old version.
  - User edits to bio/description/location made in photo-sorter after migration are preserved across re-runs.

### 4. Frontend type updates
Update `web/src/types/index.ts` so the TypeScript Subject / Label / Album types know about the new fields. Do NOT build new UI in this task — leave forms unchanged. The goal is only to surface the data on the API contract so subsequent UI work can use it.

### 5. Tests
- Add or update tests in `internal/migrate/migrate_test.go` covering:
  - Subjects: bio/about/alias migrate on first run; re-run backfills empty fields without overwriting user edits.
  - Labels: description + categories array migration; comma-split correctness for `"family, kids , travel"` → `["family", "kids", "travel"]`.
  - Albums: location/category/notes/filter/order all preserved verbatim.
- Add a handler-level test for each affected REST endpoint confirming the new fields round-trip via JSON.

## Edge Cases
- PhotoPrism `subj_alias` may contain comma-separated aliases. Native schema stores it as a single TEXT field — preserve verbatim including commas. (Splitting would lose intent.)
- PhotoPrism `album_type` values include `album`, `folder`, `month`, `moment`, `state`. The migrator currently maps only `album`. Verify that `folder`/`month`/`moment`/`state` smart-album types are migrated as albums with their `album_filter` preserved — these are the ones that most depend on `album_filter` being non-empty.
- Album `album_order` validation: enum-like in PhotoPrism but stored as text. Native column accepts any string; if non-empty and the future smart-album feature wants to enforce an enum, that's a later concern.
- `subj_alias` empty on most PhotoPrism instances — that's fine, the column defaults to ''.

## Verification
- `make build` and `make check` pass.
- For 3 sample subjects with non-empty `subj_bio` in the operator's PhotoPrism: post-migration, the destination row matches.
- For 5 sample labels with `label_description` and `label_categories`: destination round-trips.
- For 5 sample smart albums (`album_type != 'album'`, e.g. month/moment/state): destination row preserves `album_filter` exactly.
- Re-running the migration after a manual subject `bio` edit in photo-sorter leaves the user's edit intact.
- `GET /api/v1/subjects/{uid}` returns bio/about/alias.
- `GET /api/v1/albums/{uid}` returns location/category/notes/filter/order.
- `GET /api/v1/labels/{uid}` returns description/categories.
