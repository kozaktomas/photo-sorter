# Photo Book Editor Improvements

- [x] Convert Photo Detail and Similar Photos buttons to proper `<a>` links for native new-tab behavior
- [x] Show chapter name in section headers with `|` delimiter (e.g. "Chapter | Section Title")
- [x] Persist section collapse state in PageSidebar to localStorage
- [x] Fix E/D keyboard shortcuts on Pages tab to navigate between sections
- [x] Show section photo placement stats in SectionSidebar (placed/total) ([spec](docs/specs/section-placement-stats.md))
- [x] Add quick page creation (+) button with format picker next to section names in PageSidebar ([spec](docs/specs/quick-add-page.md))
- [x] Replace "Page N" labels with thumbnail previews in PageSidebar ([spec](docs/specs/page-thumbnail-previews.md))
- [x] Add cross-section duplicate finder tab to book editor ([spec](docs/specs/cross-section-duplicates.md))
- [x] Add page minimap/overview panel to Pages tab ([spec](docs/specs/page-minimap.md))
- [x] Enable drag-and-drop of photos between sections in Sections tab ([spec](docs/specs/drag-photos-between-sections.md))
- [x] Add undo/redo for page slot assignments with Ctrl+Z / Ctrl+Shift+Z ([spec](docs/specs/undo-redo-slots.md))
- [x] Add DPI quality indicator on photo slots in page editor ([spec](docs/specs/dpi-indicator.md))
- [x] Add book statistics dashboard panel to editor ([spec](docs/specs/book-stats-dashboard.md))
- [x] Detect duplicate photos across page slots in Duplicates tab ([spec](docs/specs/cross-page-duplicates.md))
- [x] Add inline caption editing from page slots ([spec](docs/specs/inline-caption-editing.md))
- [x] Add spread view (facing pages) to Preview tab ([spec](docs/specs/spread-view.md))
- [x] Add auto-layout backend endpoint to generate pages from section photos ([spec](docs/specs/auto-layout-backend.md))
- [x] Add auto-layout UI button in Pages tab ([spec](docs/specs/auto-layout-frontend.md))
- [x] Add preflight check backend endpoint for book export validation ([spec](docs/specs/preflight-backend.md))
- [x] Add preflight check modal before PDF export ([spec](docs/specs/preflight-modal.md))

## Auth / session hardening sweep — 2026-05-23

Deferred from [task-228eb564](docs/specs/task-228eb564-fa4a-4fef-8344-c2736ed99580.md).
These were flagged during the audit but intentionally left as separate
follow-ups because the right policy is non-obvious and needs an explicit
product decision rather than a guess from the implementer.

- [ ] **Login brute-force friction.** `POST /api/v1/auth/login` has no
  rate limit or lockout today. The audit log records every
  `login_failed` row with IP + UA + (truncated) attempted username, so
  detection is possible, but an attacker can still hammer the endpoint
  at line rate. Decide on a policy (per-IP token bucket vs. per-username
  exponential backoff vs. CAPTCHA after N failures) and implement; the
  share-link `verify` endpoint already has a 10-attempt / 5-minute /
  per-IP gate that could be reused as a starting point.
- [ ] **Sliding vs. absolute session expiry.** Sessions get a fixed
  30-day absolute lifetime on creation (`sessionDuration` in
  `internal/web/middleware/session.go`) and are never extended on
  activity. That is safer (a stolen cookie has a known expiry) but
  forces a re-login every 30 days even for active users. Decide whether
  to add sliding expiry on access and document the choice in
  `docs/architecture.md`.
- [ ] **Self password change does not invalidate other sessions.**
  `POST /api/v1/me/password` rotates the bcrypt hash but keeps the
  caller's current session as well as every other active session for
  the same user. The admin-initiated reset (`POST /users/{uid}/password`)
  now revokes everything; for the self path we want to keep the caller
  logged in but drop the rest, which needs a slightly different shape
  on `SessionManager.DeleteSessionsForUser` (skip-by-ID parameter).

## Authorization sweep — 2026-05-23

Notes left by [task-5588be0f](docs/specs/task-5588be0f-9cb9-4b7d-a626-a6bd544f153d.md).
The sweep itself enforced HasWriteAccess on every mutating route and
filled in the audit-log gaps it found; the items below are
documentation / follow-up that the dedicated API doc task should pick
up.

- [ ] **`docs/API.md` is missing the new audit actions.** The sweep
  introduced `face_compute`, `process_sync_cache`, `process_build_thumbs`,
  `upload_job_cancel`, `book_export_cancel`, the `book_chapter_*` /
  `book_section_*` / `book_page_*` / `book_slot_*` / `book_auto_layout`
  family, `text_check_save`, and `text_version_restore`. The frontend
  i18n labels in `web/src/i18n/locales/*/pages.json` need matching
  entries so the audit-log viewer renders human-readable verbs instead
  of raw action strings.

## File IO hardening sweep — 2026-05-23

Deferred from [task-d8674ee1](docs/specs/task-d8674ee1-7527-4eb1-9597-16a5afb57054.md).
The sweep fixed the unambiguous bugs (RFC 6266 Content-Disposition,
XMP-sidecar PID-collision, multipart-upload basename clobber, and the
concurrent-upload rollback that could delete the winner's file). The
items below were flagged during the audit but the right answer needs an
explicit product / ops decision rather than an in-line guess.

- [ ] **Edited-download memory pressure.** `GET /photos/{uid}/download`
  with stored `photo_edits` calls `imgedit.DecodeAndApply` + `EncodeJPEG`
  which pin the entire decoded image (potentially 100+ MB for a
  60-megapixel original) plus the encoded byte slice in RAM before the
  first byte is written to the wire. The HTTP server has no upstream cap
  on simultaneous renders. Options: (a) cap concurrent renders with a
  semaphore, (b) stage the rendered JPEG to a temp file and stream it
  via `http.ServeContent` so the GC can reclaim the in-memory image
  before the network handoff, (c) downscale the source to a maximum
  longest-side before applying edits when the request comes from a
  browser (keep the full-resolution path for an explicit
  `?max_side=original` opt-in). Pick one and document the trade-off.
- [ ] **Upload-race UX.** Two concurrent uploads of the same SHA256 now
  preserve catalogue integrity (the losing rollback no longer deletes
  the winner's file, see
  [`internal/photopipe/pipeline.go`](internal/photopipe/pipeline.go)
  `releaseOriginalIfOurs`), but the loser still gets a generic 500
  instead of the friendly `409 + DuplicateError` the single-upload path
  returns. Fix by mapping the `photos.file_hash` unique-violation in
  `persistPhoto` onto `*DuplicateError` so the handler can surface the
  existing photo UID.
- [ ] **`bufferAndHash` temp-file extension is user-controlled.** The
  extension from the multipart filename rides through `filepath.Ext`
  into the temp file pattern. Go's `os.CreateTemp` rejects path
  separators in the pattern, so traversal is blocked, but a
  pathological extension (e.g. extremely long, contains shell-special
  characters that downstream tools see when they shell out) could still
  produce surprising filenames in `/tmp`. Defense-in-depth: clamp to
  alphanumeric + dot + max-8-chars before composing the pattern.
