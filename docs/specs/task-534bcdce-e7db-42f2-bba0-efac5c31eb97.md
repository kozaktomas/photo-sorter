# Hover quick-actions on PhotoCard

Show a small action toolbar on photo cards on hover so single-photo actions don't require navigating into PhotoDetail.

## Requirements

- On `PhotoCard`, when the pointer hovers over the card, a small toolbar fades in over the bottom-right corner of the image with three buttons: favorite (★ toggle), archive (🗑, with confirm), add-to-album (➕ opens a small album picker popover).
- Buttons must NOT trigger the card's main click (which opens the photo detail). Click handlers must stopPropagation.
- Favorite uses the existing `PUT /api/v1/photos/{uid}` (or the batch edit endpoint with a single UID — match what `BulkActionBar` does).
- Archive uses `POST /api/v1/photos/batch/archive` with a single UID. After success the card is removed from the current grid (same UX as bulk archive).
- Add-to-album popover: searchable list of albums (reuse the Combobox + the same data source `BulkActionBar` uses); selecting one calls `POST /api/v1/albums/{uid}/photos`.
- Toolbar must not be visible while the card is in a selected state (selection UI wins) and not on touch devices (no hover concept — hidden by media query / pointer:coarse).
- Tooltip on each button via the existing tooltip pattern; i18n for labels (cs + en).

## Implementation Notes

- Reuse `usePhotoSelection`'s mutation paths where possible so success/error toast handling stays consistent (see toast task).
- Keep the toolbar absolutely positioned inside PhotoCard; ensure the parent has `position: relative` (per the existing bbox-positioning pitfall noted in CLAUDE.md).
