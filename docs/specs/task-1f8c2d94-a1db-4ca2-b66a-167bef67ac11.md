# WYSIWYG Text Preview Matching PDF Output

Add a realistic text preview to the TextSlotDialog that renders text at the exact same dimensions, font, and line height as the final PDF export. The user should be able to see how much space their text will occupy on the printed page without generating a PDF.

## Requirements

- Add EB Garamond as a Google Web Font to the frontend (it's the font used in the LaTeX PDF export)
- Create a shared typography configuration file (`web/src/constants/bookTypography.ts`) that centralizes all book typography settings. Both the WYSIWYG preview and PageLayoutPreview should read from this config. Document that when typography changes, the LaTeX template (`internal/latex/templates/book.tex`) must be updated separately.
- In the TextSlotDialog, add a WYSIWYG preview panel that renders the markdown content at PDF-accurate dimensions:
  - Font: EB Garamond, 12pt (16px), line-height 14.4pt (19.2px) — matching the LaTeX `\fontsize{12}{14.4}`
  - Width: exact slot width in mm (depends on page format and slot index — the data is already available in `PageLayoutPreview` constants)
  - Height: exact slot height in mm (172mm for full-height slots, 84mm for half-height)
  - Render using CSS `mm` units for width/height to get physical accuracy
  - The preview should be scrollable if text overflows, with a clear visual indicator showing where the slot boundary ends (e.g. a dashed line at the bottom edge)
- The preview should render markdown to HTML (using the existing `renderMarkdown` utility) with typography matching the PDF
- Show a fill percentage indicator (e.g. "~75% filled") based on rendered content height vs available slot height

## Typography Config Structure

The config must support not just font sizing but also visual styling (backgrounds, colors, padding) for headings and other elements. This allows the user to customize heading appearance (e.g. H1 as a colored box with white text) and have it reflected in both the WYSIWYG preview and (after manual LaTeX update) the PDF.

```typescript
// web/src/constants/bookTypography.ts
// NOTE: When changing these values, also update the LaTeX template at
// internal/latex/templates/book.tex to keep PDF output in sync.

export const BOOK_TYPOGRAPHY = {
  textSlot: {
    fontFamily: "'EB Garamond', serif",
    fontSize: '12pt',
    lineHeight: '14.4pt',
  },
  h1: {
    fontSize: '...',         // matching LaTeX \Large
    fontWeight: 700,
    color: undefined,        // text color (e.g. '#ffffff'), undefined = inherit
    backgroundColor: undefined, // box background (e.g. '#8B0000'), undefined = none
    padding: undefined,      // box padding (e.g. '4px 8px'), undefined = none
    borderRadius: undefined, // e.g. '2px'
    marginBottom: '4mm',     // matching LaTeX \vspace{4mm}
  },
  h2: {
    fontSize: '...',         // matching LaTeX \large
    fontWeight: 700,
    color: undefined,
    backgroundColor: undefined,
    padding: undefined,
    borderRadius: undefined,
    marginBottom: '4mm',
  },
  blockquote: {
    fontStyle: 'italic',
  },
  // ... other elements as needed
};
```

When the user later wants e.g. H1 as a full-width colored box with white text, they set `h1.color = '#fff'`, `h1.backgroundColor = '#8B0000'`, `h1.padding = '4px 12px'` in this config. The WYSIWYG preview picks it up automatically. Then they update the LaTeX template to use `\colorbox` or `tcolorbox` with matching values.

## UI Placement

The TextSlotDialog currently has a two-column layout: left (textarea + markdown preview) and right (miniature page layout preview). Options for the WYSIWYG preview:

- Replace or augment the existing markdown preview below the textarea. Add a toggle/tab to switch between "Editor preview" (current dark-themed markdown) and "Print preview" (WYSIWYG with PDF dimensions). Default to print preview.
- The miniature PageLayoutPreview on the right side should remain as-is (it shows page context).

## Edge Cases

- Text with no markdown (plain text) should still render correctly in the preview
- Tables in the preview should approximate LaTeX tabularx layout
- Very long text that overflows the slot: show the overflow area with reduced opacity or a different background, so the user can see exactly what fits and what doesn't
- Empty text: show the empty slot with its dimensions, maybe a subtle "empty" placeholder

## What NOT to Do

- Do not modify the LaTeX template or Go code — this is frontend-only
- Do not remove the existing markdown preview — add the WYSIWYG as an alternative view (toggle/tab)
- Do not hardcode typography values — use the shared config