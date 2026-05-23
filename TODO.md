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
