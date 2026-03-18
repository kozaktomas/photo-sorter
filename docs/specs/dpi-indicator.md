# DPI Quality Indicator on Page Slots

Show a resolution quality badge on each photo slot in the Pages tab so users know if a photo will print well before exporting.

## Requirements

- Each photo slot in PagesTab displays a small DPI quality badge in the bottom-left corner
- Badge colors: green (>= 300 DPI), amber (200-299 DPI), red (< 200 DPI)
- Badge shows the numeric DPI value (e.g. "285")
- DPI is calculated as: `min(naturalWidth / slotWidthMm, naturalHeight / slotHeightMm) * 25.4`
- Slot physical dimensions come from `getSlotAspectRatio()` in `pageFormats.ts` combined with the layout constants already defined there (CONTENT_WIDTH=265mm, CANVAS_HEIGHT=172mm)
- Photo pixel dimensions come from the `<img>` `naturalWidth`/`naturalHeight` (already detected for orientation)
- Badge only appears on photo slots (not text slots or empty slots)
- Badge has `bg-black/60` background with colored text, `text-[10px]` size, `rounded px-1` padding

## Implementation Notes

- Modify `web/src/pages/BookEditor/PageSlot.tsx` to accept `format`, `slotIndex`, and `splitPosition` props (already available from parent)
- Add a helper function `computeEffectiveDpi(naturalW, naturalH, format, slotIndex, splitPosition)` to `web/src/utils/pageFormats.ts` that returns the effective DPI number
- Use the existing `onLoad` callback that already reads `naturalWidth`/`naturalHeight` for orientation detection - extend it to also compute DPI
- Store DPI in component state alongside orientation
- Add i18n key `books.editor.dpiLabel` for tooltip (e.g. "Effective print resolution")
