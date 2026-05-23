# Paste-to-upload (Ctrl+V) on Upload page

Let users paste images directly into the Upload page — great for screenshots and clipboard workflows.

## Requirements

- On `web/src/pages/Upload`, listen for `paste` events on the document while the Upload page is mounted.
- Walk `clipboardData.items`; any item whose `kind === 'file'` and `type` starts with `image/` is converted to a `File` and added to the same queue `DropZone` populates.
- Pasted files get an auto-generated filename if the clipboard didn't supply one: `pasted-<YYYYMMDD-HHMMSS>-<n>.<ext>` derived from the MIME type (`image/png` → `.png`, `image/jpeg` → `.jpg`, fallback `.bin`).
- Skip when focus is in an `input`/`textarea` so users can still paste into the album-picker filter or labels field.
- A short toast / inline hint confirms "Added N image(s) from clipboard." If no image was found in the paste, show "Clipboard did not contain an image." (i18n cs+en).
- The pasted file flows through the existing near-duplicate check and upload job flow with no special-casing downstream.

## Implementation Notes

- Listener must be cleaned up on unmount.
- HEIC paste is rare but if `type` is `image/heic`/`heif` it should still queue — the backend pipeline handles it.
