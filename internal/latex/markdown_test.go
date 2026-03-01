package latex

import (
	"strings"
	"testing"
)

func TestMarkdownToLatex_Headings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "h1",
			input:    "# Hello World",
			contains: []string{`{\Large\bfseries Hello World}\par\vspace{4mm}`},
		},
		{
			name:     "h2",
			input:    "## Subheading",
			contains: []string{`{\large\bfseries Subheading}\par\vspace{4mm}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkdownToLatex(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("MarkdownToLatex(%q) = %q, want it to contain %q", tt.input, got, want)
				}
			}
		})
	}
}

func TestMarkdownToLatex_Bold(t *testing.T) {
	got := MarkdownToLatex("This is **bold** text")
	if !strings.Contains(got, `\textbf{bold}`) {
		t.Errorf("expected \\textbf{bold}, got %q", got)
	}
}

func TestMarkdownToLatex_Italic(t *testing.T) {
	got := MarkdownToLatex("This is *italic* text")
	if !strings.Contains(got, `\textit{italic}`) {
		t.Errorf("expected \\textit{italic}, got %q", got)
	}
}

func TestMarkdownToLatex_BoldAndItalic(t *testing.T) {
	got := MarkdownToLatex("**bold** and *italic*")
	if !strings.Contains(got, `\textbf{bold}`) {
		t.Errorf("expected \\textbf{bold}, got %q", got)
	}
	if !strings.Contains(got, `\textit{italic}`) {
		t.Errorf("expected \\textit{italic}, got %q", got)
	}
}

func TestMarkdownToLatex_UnorderedList(t *testing.T) {
	input := "- First item\n- Second item\n- Third item"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\begin{itemize}[nosep,leftmargin=1.5em]`) {
		t.Errorf("expected \\begin{itemize}, got %q", got)
	}
	if !strings.Contains(got, `\item First item`) {
		t.Errorf("expected \\item First item, got %q", got)
	}
	if !strings.Contains(got, `\item Third item`) {
		t.Errorf("expected \\item Third item, got %q", got)
	}
	if !strings.Contains(got, `\end{itemize}`) {
		t.Errorf("expected \\end{itemize}, got %q", got)
	}
}

func TestMarkdownToLatex_UnorderedListAsterisk(t *testing.T) {
	input := "* First\n* Second"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\begin{itemize}`) {
		t.Errorf("expected \\begin{itemize}, got %q", got)
	}
	if !strings.Contains(got, `\item First`) {
		t.Errorf("expected \\item First, got %q", got)
	}
}

func TestMarkdownToLatex_OrderedList(t *testing.T) {
	input := "1. Alpha\n2. Beta\n3. Gamma"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\begin{enumerate}[nosep,leftmargin=1.5em]`) {
		t.Errorf("expected \\begin{enumerate}, got %q", got)
	}
	if !strings.Contains(got, `\item Alpha`) {
		t.Errorf("expected \\item Alpha, got %q", got)
	}
	if !strings.Contains(got, `\end{enumerate}`) {
		t.Errorf("expected \\end{enumerate}, got %q", got)
	}
}

func TestMarkdownToLatex_Paragraphs(t *testing.T) {
	input := "First paragraph\n\nSecond paragraph"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\par\vspace{4mm}`) {
		t.Errorf("expected paragraph break, got %q", got)
	}
	if !strings.Contains(got, "First paragraph") {
		t.Errorf("expected first paragraph text, got %q", got)
	}
	if !strings.Contains(got, "Second paragraph") {
		t.Errorf("expected second paragraph text, got %q", got)
	}
}

func TestMarkdownToLatex_MixedContent(t *testing.T) {
	input := "# Title\n\nSome **bold** text\n\n- item one\n- item two"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `{\Large\bfseries Title}`) {
		t.Errorf("expected heading, got %q", got)
	}
	if !strings.Contains(got, `\textbf{bold}`) {
		t.Errorf("expected bold, got %q", got)
	}
	if !strings.Contains(got, `\begin{itemize}`) {
		t.Errorf("expected itemize, got %q", got)
	}
}

func TestMarkdownToLatex_PlainTextBackwardCompat(t *testing.T) {
	input := "Just some plain text without any formatting"
	got := MarkdownToLatex(input)
	if got != "Just some plain text without any formatting" {
		t.Errorf("plain text should pass through unchanged, got %q", got)
	}
}

func TestMarkdownToLatex_LaTeXSpecialChars(t *testing.T) {
	input := "Price is 100% & tax is $5"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `100\%`) {
		t.Errorf("expected escaped %%, got %q", got)
	}
	if !strings.Contains(got, `\&`) {
		t.Errorf("expected escaped &, got %q", got)
	}
	if !strings.Contains(got, `\$`) {
		t.Errorf("expected escaped $, got %q", got)
	}
}

func TestMarkdownToLatex_CzechTypography(t *testing.T) {
	input := "Byl v lese s kamarady"
	got := MarkdownToLatex(input)
	// Single-letter Czech prepositions should get non-breaking spaces.
	if !strings.Contains(got, "v~") {
		t.Errorf("expected Czech typography for 'v', got %q", got)
	}
	if !strings.Contains(got, "s~") {
		t.Errorf("expected Czech typography for 's', got %q", got)
	}
}

func TestMarkdownToLatex_HeadingWithSpecialChars(t *testing.T) {
	input := "# Price & Cost: 100%"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\Large\bfseries`) {
		t.Errorf("expected heading markup, got %q", got)
	}
	if !strings.Contains(got, `\&`) {
		t.Errorf("expected escaped &, got %q", got)
	}
	if !strings.Contains(got, `\%`) {
		t.Errorf("expected escaped %%, got %q", got)
	}
}

func TestMarkdownToLatex_ListWithInlineFormatting(t *testing.T) {
	input := "- **bold item**\n- *italic item*"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\item \textbf{bold item}`) {
		t.Errorf("expected bold list item, got %q", got)
	}
	if !strings.Contains(got, `\item \textit{italic item}`) {
		t.Errorf("expected italic list item, got %q", got)
	}
}

func TestMarkdownToLatex_Blockquote(t *testing.T) {
	input := "> This is a quote\n> Second line"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\begin{quote}\itshape`) {
		t.Errorf("expected \\begin{quote}\\itshape, got %q", got)
	}
	if !strings.Contains(got, "This is a~quote") {
		t.Errorf("expected quote text, got %q", got)
	}
	if !strings.Contains(got, `\end{quote}`) {
		t.Errorf("expected \\end{quote}, got %q", got)
	}
}

func TestMarkdownToLatex_BlockquoteWithInlineFormatting(t *testing.T) {
	input := "> This is **bold** in a quote"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\textbf{bold}`) {
		t.Errorf("expected bold in blockquote, got %q", got)
	}
}

func TestDetectTextType_T1_PlainText(t *testing.T) {
	if got := DetectTextType("Just some text"); got != "T1" {
		t.Errorf("expected T1 for plain text, got %q", got)
	}
}

func TestDetectTextType_T1_HeadingsAndText(t *testing.T) {
	if got := DetectTextType("# Title\nSome text"); got != "T1" {
		t.Errorf("expected T1 for headings + text, got %q", got)
	}
}

func TestDetectTextType_T2_AllListItems(t *testing.T) {
	if got := DetectTextType("- Item one\n- Item two\n- Item three"); got != "T2" {
		t.Errorf("expected T2 for all list items, got %q", got)
	}
}

func TestDetectTextType_T2_OrderedList(t *testing.T) {
	if got := DetectTextType("1. First\n2. Second"); got != "T2" {
		t.Errorf("expected T2 for ordered list, got %q", got)
	}
}

func TestDetectTextType_T3_Blockquote(t *testing.T) {
	if got := DetectTextType("> A quote\n> More quote"); got != "T3" {
		t.Errorf("expected T3 for blockquote, got %q", got)
	}
}

func TestDetectTextType_T3_MixedWithBlockquote(t *testing.T) {
	if got := DetectTextType("Some text\n> A quote"); got != "T3" {
		t.Errorf("expected T3 when blockquote present, got %q", got)
	}
}
