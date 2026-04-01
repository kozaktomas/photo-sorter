package latex

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Inline formatting regexes — applied after LaTeX escaping.
// Only * / ** syntax is supported (not _ / __ since _ is escaped to \_ by latexEscapeRaw).
var (
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe = regexp.MustCompile(`\*(.+?)\*`)
)

// Alignment macros — detected on trimmed lines before inline formatting.
// ->text<- → center, ->text-> → right-align.
var (
	alignCenterRe = regexp.MustCompile(`^->\s*(.*?)\s*<-$`)
	alignRightRe  = regexp.MustCompile(`^->\s*(.*?)\s*->$`)
)

// MarkdownToLatex converts a subset of Markdown to LaTeX.
//
// Supported syntax:.
//   - # Heading       → {\fontsize{16}{19.2}\selectfont\sffamily\bfseries Heading}\par\vspace{4mm}
//   - ## Subheading   → {\fontsize{13}{15.6}\selectfont\bfseries Subheading}\par\vspace{4mm}
//   - **bold**        → \textbf{bold}
//   - *italic*        → \textit{italic}
//   - - item / * item → \begin{itemize}[nosep,leftmargin=1.5em] ... \end{itemize}
//   - 1. item         → \begin{enumerate}[nosep,leftmargin=1.5em] ... \end{enumerate}
//   - > quote         → \begin{quote}\itshape ... \end{quote}
//   - ->text<-        → {\centering text\par}
//   - ->text->        → {\raggedleft text\par}
//   - blank line      → \par\vspace{4mm}
//   - plain text      → escaped text (backward compatible)
func MarkdownToLatex(md string) string {
	return markdownToLatexInternal(md, "")
}

// MarkdownToLatexWithColor converts Markdown to LaTeX with chapter color applied to H1 headings.
// The color parameter is a hex color without # prefix (e.g. "8B0000"). Empty means no color.
func MarkdownToLatexWithColor(md, chapterColor string) string {
	return markdownToLatexInternal(md, chapterColor)
}

//nolint:funlen,gocognit,cyclop // Sequential markdown transformation pipeline.
func markdownToLatexInternal(md, chapterColor string) string {
	lines := strings.Split(md, "\n")
	var out []string
	i := 0

	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		// Blank line → paragraph break.
		if trimmed == "" {
			out = append(out, `\par\vspace{4mm}`)
			i++
			continue
		}

		// Heading ## (must check before #).
		if text, ok := strings.CutPrefix(trimmed, "## "); ok {
			out = append(out, formatHeading(text, 2, ""))
			i++
			continue
		}

		// Heading #.
		if text, ok := strings.CutPrefix(trimmed, "# "); ok {
			out = append(out, formatHeading(text, 1, chapterColor))
			i++
			continue
		}

		// Unordered list (- or *).
		if isUnorderedListItem(trimmed) {
			items, newI := collectListItems(lines, i, isUnorderedListItem, stripListMarker)
			out = append(out, `\begin{itemize}[nosep,leftmargin=1.5em]`)
			out = append(out, items...)
			out = append(out, `\end{itemize}`)
			i = newI
			continue
		}

		// Ordered list.
		if isOrderedListItem(trimmed) {
			items, newI := collectListItems(lines, i, isOrderedListItem, stripOrderedListMarker)
			out = append(out, `\begin{enumerate}[nosep,leftmargin=1.5em]`)
			out = append(out, items...)
			out = append(out, `\end{enumerate}`)
			i = newI
			continue
		}

		// Blockquote.
		if isBlockquoteLine(trimmed) {
			quoteLines, newI := collectBlockquote(lines, i)
			out = append(out, `\begin{quote}\itshape`)
			out = append(out, quoteLines...)
			out = append(out, `\end{quote}`)
			i = newI
			continue
		}

		// Table.
		if isTableLine(trimmed) && i+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[i+1])) {
			tableLines, newI := collectTable(lines, i)
			out = append(out, tableLines...)
			i = newI
			continue
		}

		// Center alignment: ->text<-
		if m := alignCenterRe.FindStringSubmatch(trimmed); m != nil {
			text := inlineFormat(latexEscapeRaw(m[1]))
			text = czechTypography(text)
			out = append(out, `{\centering `+text+`\par}`)
			i++
			continue
		}

		// Right alignment: ->text->
		if m := alignRightRe.FindStringSubmatch(trimmed); m != nil {
			text := inlineFormat(latexEscapeRaw(m[1]))
			text = czechTypography(text)
			out = append(out, `{\raggedleft `+text+`\par}`)
			i++
			continue
		}

		// Plain text paragraph.
		text := inlineFormat(latexEscapeRaw(trimmed))
		text = czechTypography(text)
		out = append(out, text)
		i++
	}

	return strings.Join(out, "\n")
}

// formatHeading formats a heading line with an explicit font size.
// H1 (level 1) = 16pt, H2 (level 2) = 13pt. Line spacing is 1.2× the font size.
// H1 uses \sffamily to render in Source Sans 3 (the sans-serif font).
// If chapterColor is non-empty (hex without #), H1 renders with a colored background and white text.
func formatHeading(text string, level int, chapterColor string) string {
	text = inlineFormat(latexEscapeRaw(text))
	text = czechTypography(text)

	sizeCmd := `\fontsize{13}{15.6}\selectfont`
	fontCmd := `\bfseries `
	if level == 1 {
		sizeCmd = `\fontsize{16}{19.2}\selectfont`
		fontCmd = `\sffamily\bfseries `
	}

	if level == 1 && chapterColor != "" {
		colorDef := fmt.Sprintf(`\definecolor{chaptercolor}{HTML}{%s}`, chapterColor)
		box := fmt.Sprintf(
			`\noindent\colorbox{chaptercolor}{\parbox{\dimexpr\linewidth-2\fboxsep}{%s%s\textcolor{white}{%s}}}`,
			sizeCmd, fontCmd, text,
		)
		return colorDef + "\n" + box + `\par\vspace{4mm}`
	}
	return `{` + sizeCmd + fontCmd + text + `}\par\vspace{4mm}`
}

// collectListItems consumes consecutive list items and returns formatted LaTeX items.
func collectListItems(
	lines []string, start int,
	isItem func(string) bool, stripMarker func(string) string,
) ([]string, int) {
	var items []string
	i := start
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if !isItem(t) {
			break
		}
		item := stripMarker(t)
		item = inlineFormat(latexEscapeRaw(item))
		item = czechTypography(item)
		items = append(items, `\item `+item)
		i++
	}
	return items, i
}

// collectBlockquote consumes consecutive blockquote lines and returns formatted text.
func collectBlockquote(lines []string, start int) ([]string, int) {
	var quoteLines []string
	i := start
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if !isBlockquoteLine(t) {
			break
		}
		text := stripBlockquoteMarker(t)
		text = inlineFormat(latexEscapeRaw(text))
		text = czechTypography(text)
		quoteLines = append(quoteLines, text)
		i++
	}
	return quoteLines, i
}

// inlineFormat applies bold and italic formatting.
// Must be called AFTER latexEscapeRaw so that * chars (not LaTeX special) are still present.
func inlineFormat(s string) string {
	// Bold first (** before *).
	s = boldRe.ReplaceAllString(s, `\textbf{$1}`)
	s = italicRe.ReplaceAllString(s, `\textit{$1}`)
	return s
}

var unorderedListRe = regexp.MustCompile(`^[-*]\s+`)
var orderedListRe = regexp.MustCompile(`^\d+\.\s+`)

func isUnorderedListItem(s string) bool {
	return unorderedListRe.MatchString(s)
}

func isOrderedListItem(s string) bool {
	return orderedListRe.MatchString(s)
}

func stripListMarker(s string) string {
	return unorderedListRe.ReplaceAllString(s, "")
}

func stripOrderedListMarker(s string) string {
	return orderedListRe.ReplaceAllString(s, "")
}

var blockquoteRe = regexp.MustCompile(`^>\s?`)

func isBlockquoteLine(s string) bool {
	return strings.HasPrefix(s, "> ") || s == ">"
}

func stripBlockquoteMarker(s string) string {
	return blockquoteRe.ReplaceAllString(s, "")
}

var tableSepRe = regexp.MustCompile(`^\|[\s:%0-9-]+(\|[\s:%0-9-]+)*\|$`)
var pctRe = regexp.MustCompile(`([0-9]+)%`)

func isTableLine(s string) bool {
	return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && strings.Count(s, "|") >= 2
}

func isTableSeparator(s string) bool {
	return tableSepRe.MatchString(s)
}

// parseColumnWidths extracts percentage widths from a separator row.
// Returns nil if no percentages are found. E.g. "|--- 60%---|--- 40%---|" → [60, 40].
func parseColumnWidths(separator string) []int {
	cells := parseTableCells(separator)
	widths := make([]int, len(cells))
	found := false
	for i, cell := range cells {
		if m := pctRe.FindStringSubmatch(cell); m != nil {
			w, _ := strconv.Atoi(m[1])
			widths[i] = w
			found = true
		}
	}
	if !found {
		return nil
	}
	return widths
}

// buildColSpec builds a tabularx column spec from optional percentage widths.
func buildColSpec(numCols int, widths []int) string {
	if widths == nil {
		return fmt.Sprintf(`|*{%d}{X|}`, numCols)
	}
	// Compute hsize multipliers: multiplier = pct * numCols / 100.
	var parts []string
	for i := range numCols {
		pct := 100 / numCols // default: equal
		if i < len(widths) && widths[i] > 0 {
			pct = widths[i]
		}
		multiplier := float64(pct) * float64(numCols) / 100.0
		parts = append(parts, fmt.Sprintf(`>{\hsize=%.2f\hsize}X`, multiplier))
	}
	return "|" + strings.Join(parts, "|") + "|"
}

// parseTableCells splits a table row into cells, trimming whitespace.
func parseTableCells(line string) []string {
	// Strip leading/trailing whitespace and pipes.
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// collectTable consumes a GFM pipe table and returns LaTeX tabularx lines.
func collectTable(lines []string, start int) ([]string, int) {
	i := start
	headerCells := parseTableCells(lines[i])
	numCols := len(headerCells)
	i++ // skip header row
	widths := parseColumnWidths(lines[i])
	i++ // skip separator row

	// Format header cells.
	for j, cell := range headerCells {
		cell = inlineFormat(latexEscapeRaw(cell))
		cell = czechTypography(cell)
		headerCells[j] = `\textbf{` + cell + `}`
	}

	colSpec := buildColSpec(numCols, widths)

	out := []string{
		`\begin{tabularx}{\linewidth}{` + colSpec + `}`,
		`\hline`,
		strings.Join(headerCells, " & ") + ` \\`,
		`\hline`,
	}

	// Data rows.
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if !isTableLine(trimmed) {
			break
		}
		cells := parseTableCells(trimmed)
		// Pad or truncate to match header column count.
		for len(cells) < numCols {
			cells = append(cells, "")
		}
		cells = cells[:numCols]
		for j, cell := range cells {
			cell = inlineFormat(latexEscapeRaw(cell))
			cell = czechTypography(cell)
			cells[j] = cell
		}
		out = append(out, strings.Join(cells, " & ")+` \\`)
		i++
	}

	out = append(out, `\hline`, `\end{tabularx}`)
	return out, i
}

// DetectTextType auto-detects the text slot type from markdown content.
//   - "T3" (oral history quote): contains blockquote lines (> text)
//   - "T2" (fact box): all non-blank lines are list items (-, *, 1.)
//   - "T1" (explanation): everything else
func DetectTextType(md string) string {
	lines := strings.Split(md, "\n")
	hasBlockquote := false
	allList := true
	hasNonBlank := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		hasNonBlank = true
		if isBlockquoteLine(trimmed) {
			hasBlockquote = true
		}
		if !isUnorderedListItem(trimmed) && !isOrderedListItem(trimmed) {
			allList = false
		}
	}

	if hasBlockquote {
		return "T3"
	}
	if hasNonBlank && allList {
		return "T2"
	}
	return "T1"
}
