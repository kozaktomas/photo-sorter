package latex

import (
	"regexp"
	"strings"
)

// Inline formatting regexes — applied after LaTeX escaping.
// Only * / ** syntax is supported (not _ / __ since _ is escaped to \_ by latexEscapeRaw).
var (
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe = regexp.MustCompile(`\*(.+?)\*`)
)

// MarkdownToLatex converts a subset of Markdown to LaTeX.
//
// Supported syntax:
//   - # Heading       → {\Large\bfseries Heading}\par\vspace{4mm}
//   - ## Subheading   → {\large\bfseries Subheading}\par\vspace{4mm}
//   - **bold**        → \textbf{bold}
//   - *italic*        → \textit{italic}
//   - - item / * item → \begin{itemize}[nosep,leftmargin=1.5em] ... \end{itemize}
//   - 1. item         → \begin{enumerate}[nosep,leftmargin=1.5em] ... \end{enumerate}
//   - > quote         → \begin{quote}\itshape ... \end{quote}
//   - blank line      → \par\vspace{4mm}
//   - plain text      → escaped text (backward compatible)
func MarkdownToLatex(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	i := 0

	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		// Blank line → paragraph break
		if trimmed == "" {
			out = append(out, `\par\vspace{4mm}`)
			i++
			continue
		}

		// Heading ## (must check before #)
		if text, ok := strings.CutPrefix(trimmed, "## "); ok {
			out = append(out, formatHeading(text, `\large`))
			i++
			continue
		}

		// Heading #
		if text, ok := strings.CutPrefix(trimmed, "# "); ok {
			out = append(out, formatHeading(text, `\Large`))
			i++
			continue
		}

		// Unordered list (- or *)
		if isUnorderedListItem(trimmed) {
			items, newI := collectListItems(lines, i, isUnorderedListItem, stripListMarker)
			out = append(out, `\begin{itemize}[nosep,leftmargin=1.5em]`)
			out = append(out, items...)
			out = append(out, `\end{itemize}`)
			i = newI
			continue
		}

		// Ordered list
		if isOrderedListItem(trimmed) {
			items, newI := collectListItems(lines, i, isOrderedListItem, stripOrderedListMarker)
			out = append(out, `\begin{enumerate}[nosep,leftmargin=1.5em]`)
			out = append(out, items...)
			out = append(out, `\end{enumerate}`)
			i = newI
			continue
		}

		// Blockquote
		if isBlockquoteLine(trimmed) {
			quoteLines, newI := collectBlockquote(lines, i)
			out = append(out, `\begin{quote}\itshape`)
			out = append(out, quoteLines...)
			out = append(out, `\end{quote}`)
			i = newI
			continue
		}

		// Plain text paragraph
		text := inlineFormat(latexEscapeRaw(trimmed))
		text = czechTypography(text)
		out = append(out, text)
		i++
	}

	return strings.Join(out, "\n")
}

// formatHeading formats a heading line with the given LaTeX size command.
func formatHeading(text, sizeCmd string) string {
	text = inlineFormat(latexEscapeRaw(text))
	text = czechTypography(text)
	return `{` + sizeCmd + `\bfseries ` + text + `}\par\vspace{4mm}`
}

// collectListItems consumes consecutive list items and returns formatted LaTeX items.
func collectListItems(lines []string, start int, isItem func(string) bool, stripMarker func(string) string) ([]string, int) {
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
	// Bold first (** before *)
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
