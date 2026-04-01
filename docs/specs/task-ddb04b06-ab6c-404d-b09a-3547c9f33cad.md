# MCP Section Tools for Photo Book

Add section management MCP tools to the existing MCP server (created in a previous task). These tools allow AI to manage sections within books and assign photos to sections.

## Prerequisites

The MCP server infrastructure, auth, and book/chapter tools already exist in `internal/mcp/`. This task adds section-related tools.

## MCP Tools — Sections

1. **`create_section`** — Create a section in a book
   - Parameters: `book_id` (int, required), `title` (string, required), `chapter_id` (int, optional — assign to chapter)
   - Returns: created section with id, title, position

2. **`update_section`** — Update section title or chapter assignment
   - Parameters: `section_id` (int, required), `title` (string, optional), `chapter_id` (int, optional — 0 to unassign from chapter)
   - Returns: updated section

3. **`delete_section`** — Delete a section
   - Parameters: `section_id` (int, required)
   - Returns: success confirmation

4. **`reorder_sections`** — Reorder sections in a book
   - Parameters: `book_id` (int, required), `section_ids` (array of ints, required — new order)
   - Returns: success confirmation

5. **`list_section_photos`** — List photos assigned to a section
   - Parameters: `section_id` (int, required)
   - Returns: array of photos with uid, description, note, and position

6. **`add_photos_to_section`** — Add photos to a section
   - Parameters: `section_id` (int, required), `photo_uids` (array of strings, required)
   - Returns: success confirmation with count of added photos

7. **`remove_photos_from_section`** — Remove photos from a section
   - Parameters: `section_id` (int, required), `photo_uids` (array of strings, required)
   - Returns: success confirmation

8. **`update_section_photo`** — Update a photo's description or note within a section
   - Parameters: `section_id` (int, required), `photo_uid` (string, required), `description` (string, optional), `note` (string, optional)
   - Returns: updated photo entry

## Implementation Notes

- Add tools in `internal/mcp/sections.go`
- Register tools in the existing server setup in `server.go`
- Reuse existing `BookReader`/`BookWriter` repository methods
- For photo operations, the existing `SectionPhotos`, `AddPhotosToSection`, `RemovePhotosFromSection` methods should be available in the book repository