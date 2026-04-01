## Problem

In mixed page layouts (`1p_2l`, `2l_1p`), the H1 chapter-colored background box bleeds symmetrically (4mm each side), but it should only bleed outward (towards the page edge), not inward (towards the photo column gutter). This makes the H1 box visually wider than the body text below it.

**Example:** In a `1p_2l` layout (text left, photos right), the H1 green box extends 4mm to the right beyond where the body text ends, into the gutter area.

## Root Cause

In `internal/latex/markdown.go:formatHeading()` (line 216-218):
```latex
{\fboxsep=0pt\noindent\hspace{-4mm}\colorbox{chaptercolor}{\parbox{\dimexpr\linewidth+8mm}{...}}}
```

This always bleeds 4mm left + 4mm right. But in mixed layouts, `applyTextSlotPadding()` in `latex.go:748` adds `TextPadRight` or `TextPadLeft` to keep body text away from photos. The H1 bleed ignores this, extending into the gutter.

Specifically:
- Left column text slot (`1p_2l` slot 0): `TextPadRight=4mm` → body text is 4mm narrower on right, but H1 bleeds 4mm right anyway
- Right column text slot (`2l_1p` slot 2): `TextPadLeft=4mm` → body text is 4mm narrower on left, but H1 bleeds 4mm left anyway

## Solution

Make the H1 bleed directional by passing bleed parameters through the markdown converter.

### Step 1: Update `MarkdownToLatexWithColor` signature

In `internal/latex/markdown.go`, add bleed parameters:

```go
// Current:
func MarkdownToLatexWithColor(text, chapterColor string) string

// New:
func MarkdownToLatexWithColor(text, chapterColor string, bleedLeftMM, bleedRightMM float64) string
```

Pass these to `formatHeading()` as well.

### Step 2: Update `formatHeading` to use directional bleed

```go
func formatHeading(text string, level int, chapterColor string, bleedLeftMM, bleedRightMM float64) string
```

Change the LaTeX from:
```latex
\hspace{-4mm}\colorbox{chaptercolor}{\parbox{\dimexpr\linewidth+8mm}{...}}
```
To:
```latex
\hspace{-<bleedLeft>mm}\colorbox{chaptercolor}{\parbox{\dimexpr\linewidth+<bleedLeft+bleedRight>mm}{...}}
```

Also adjust the inner `\hspace{16pt}` padding: keep 16pt on the bleed side (the side with margin space), but use less (e.g., 8pt or match body text indent) on the non-bleed side so the H1 text aligns with the body text.

### Step 3: Update template function map and template call

In `internal/latex/latex.go`, update the template function:
```go
"markdownToLatexColor": func(text, color string, bleedL, bleedR float64) string {
    return MarkdownToLatexWithColor(text, color, bleedL, bleedR)
},
```

In `internal/latex/templates/book.tex` (line 96), pass the bleed values. The `TemplateSlot` struct needs `BleedLeftMM` and `BleedRightMM` fields.

### Step 4: Compute bleed values per slot

In the slot preparation code (where `applyTextSlotPadding` is called), set bleed values:

- **Full-width text** (`1_fullscreen`): `BleedLeftMM=4, BleedRightMM=4`
- **Left column text** (slot 0 in `1p_2l`, or all text in `2l_1p` if on left): `BleedLeftMM=4, BleedRightMM=0`
- **Right column text** (slot 2 in `2l_1p`, or all text in `1p_2l` if on right): `BleedLeftMM=0, BleedRightMM=4`
- **No chapter color** (chapterColor is empty): bleed values are irrelevant (no colored box rendered)

The logic: bleed towards the page edge (outside), not towards adjacent photo slots.

### Step 5: Update `MarkdownToLatex` (non-color version)

The non-color `MarkdownToLatex` doesn't render colored H1 boxes, so it needs no changes.

### Step 6: Update tests

Update `internal/latex/markdown_test.go` — any tests calling `MarkdownToLatexWithColor` need the new parameters. Add test cases:
- H1 with bleed left only (left column text)
- H1 with bleed right only (right column text) 
- H1 with bleed both sides (fullscreen)
- H1 with no bleed (0, 0) — should still render but flush with text

## Files to Modify

- `internal/latex/markdown.go` — `MarkdownToLatexWithColor`, `formatHeading` signatures + logic
- `internal/latex/latex.go` — `TemplateSlot` struct (add BleedLeftMM/BleedRightMM), slot preparation, template func map
- `internal/latex/templates/book.tex` — pass bleed values to `markdownToLatexColor`
- `internal/latex/markdown_test.go` — update existing tests, add new test cases
- `internal/latex/latex_test.go` — if any tests reference the color function