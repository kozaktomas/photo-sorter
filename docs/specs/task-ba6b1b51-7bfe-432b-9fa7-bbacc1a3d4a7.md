# Text Version History

Track text changes so users can revert accidental overwrites or undo accepted AI suggestions.

## Requirements

### 1. Backend versioning

Create a new database table `text_versions` to store text history:

```sql
CREATE TABLE text_versions (
  id SERIAL PRIMARY KEY,
  source_type TEXT NOT NULL,        -- 'section_photo' or 'page_slot'
  source_id TEXT NOT NULL,          -- section_id:photo_uid or page_id:slot_index
  field TEXT NOT NULL,              -- 'description', 'note', or 'text_content'
  content TEXT NOT NULL,
  changed_by TEXT DEFAULT 'user',   -- 'user' or 'ai'
  created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_text_versions_source ON text_versions(source_type, source_id, field);
```

### 2. Automatic version creation

Whenever a text field is updated via the API, save the **previous** value as a version before applying the new value. This means:
- `PUT /api/v1/sections/{id}/photos/{photoUid}/description` — save old description/note before update
- `PUT /api/v1/pages/{id}/slots/{index}` — save old text_content before update

Only create a version if the content actually changed (old != new).

### 3. Version history API

Add endpoints:
- `GET /api/v1/text-versions?source_type=section_photo&source_id={sectionId}:{photoUid}&field=description` — list versions for a text field, ordered by created_at DESC, limit 20
- `POST /api/v1/text-versions/{id}/restore` — restore a specific version (updates the current text to that version's content, creating a new version of the current text first)

### 4. Frontend UI

Add a small "Historie" (History) button/link next to each textarea in PhotoDescriptionDialog and TextSlotDialog. On click, show a dropdown or small panel listing recent versions:
- Each entry shows: truncated content preview (~50 chars), timestamp, changed_by badge (user/ai)
- Click "Obnovit" (Restore) to revert to that version
- After restore, the textarea updates and the dialog stays open (user can then save)

### 5. i18n

Add keys:
- `books.editor.history`: "Historie" (cs) / "History" (en)
- `books.editor.restore`: "Obnovit" (cs) / "Restore" (en)
- `books.editor.changedByUser`: "uživatel" (cs) / "user" (en)
- `books.editor.changedByAi`: "AI" (cs) / "AI" (en)
- `books.editor.noHistory`: "Žádná historie" (cs) / "No history" (en)

## Files to create/modify

- `internal/database/postgres/migrations/017_text_versions.sql` — new migration
- `internal/database/postgres/text_versions.go` — repository implementation
- `internal/database/types.go` — TextVersion type
- `internal/database/repository.go` — TextVersionReader/Writer interfaces
- `internal/web/handlers/text_versions.go` — new handler for version API
- `internal/web/handlers/books.go` — add version saving before text updates
- `internal/web/server.go` — register new routes
- `web/src/api/client.ts` — add version API methods
- `web/src/pages/BookEditor/PhotoDescriptionDialog.tsx` — add history button and panel
- `web/src/pages/BookEditor/PagesTab.tsx` — add history button and panel in TextSlotDialog
- `web/src/i18n/locales/cs/pages.json` — add i18n keys
- `web/src/i18n/locales/en/pages.json` — add i18n keys