## Bug

In the photo book PDF export, colored H1 headings (chapters with color set) appear visually offset/indented compared to the following paragraph text. There seems to be extra margin or padding that shifts the colorbox.

## Cause

In `internal/latex/markdown.go`, function `formatHeading` (around line 128), the colored H1 uses:

```latex
\noindent\colorbox{chaptercolor}{\parbox{\dimexpr\linewidth-2\fboxsep}{...}}
```

The `\colorbox` adds `\fboxsep` padding around the content by default (3pt on each side). This makes the colored box content appear indented relative to normal text below it. The `\parbox` compensates for width but the box itself still has outer padding from `\fboxsep`.

## Fix

1. **Remove outer offset** — the colorbox must be flush with the text column edges (no horizontal shift vs following paragraph text)
2. **Add explicit inner padding** — `10pt` vertical, `12pt` horizontal inside the colorbox

Set `\fboxsep=0pt` locally so the box is flush, then use a `\parbox` with inner `\hspace`/`\vspace` for the desired padding:

```latex
{\fboxsep=0pt\noindent\colorbox{chaptercolor}{\parbox{\linewidth}{\vspace{10pt}\hspace{12pt}\sffamily\bfseries\textcolor{white}{Text}\hspace{12pt}\vspace{10pt}}}}\par\vspace{4mm}
```

Or use `\rule{0pt}{...}` for vertical spacing. The key point: outer box flush with column, inner padding 10pt top/bottom and 12pt left/right.

## Files to modify

- `internal/latex/markdown.go` — function `formatHeading`, the colored H1 branch (~line 127-133)

## Important

- Work-in-progress book exists — only change LaTeX output formatting, no schema changes
- Test with and without chapter color to ensure non-colored H1 is unaffected

## Verification

1. `./dev.sh` — rebuild
2. Export PDF with colored H1 headings — verify the colorbox is flush with the text column (no extra horizontal offset)
3. Verify inner padding is visible (~10pt vertical, ~12pt horizontal)
4. Verify non-colored H1 headings are unaffected
5. `make lint && make test`