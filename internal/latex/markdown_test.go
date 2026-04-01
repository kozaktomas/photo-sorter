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
			contains: []string{`{\fontsize{18}{22}\selectfont\sffamily\bfseries Hello World}\par\vspace{4mm}`},
		},
		{
			name:     "h2",
			input:    "## Subheading",
			contains: []string{`{\fontsize{15}{18}\selectfont\bfseries Subheading}\par\vspace{4mm}`},
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
	if !strings.Contains(got, `\fontsize{18}{22}\selectfont\sffamily\bfseries Title}`) {
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
	if !strings.Contains(got, `\fontsize{18}{22}\selectfont\sffamily\bfseries`) {
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

func TestMarkdownToLatex_Table(t *testing.T) {
	input := "| Name | Age |\n| --- | --- |\n| Alice | 30 |\n| Bob | 25 |"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\begin{tabularx}{\linewidth}{|*{2}{X|}}`) {
		t.Errorf("expected tabularx with 2 columns, got %q", got)
	}
	if !strings.Contains(got, `\textbf{Name}`) {
		t.Errorf("expected bold header Name, got %q", got)
	}
	if !strings.Contains(got, `\textbf{Age}`) {
		t.Errorf("expected bold header Age, got %q", got)
	}
	if !strings.Contains(got, `Alice & 30`) {
		t.Errorf("expected data row Alice & 30, got %q", got)
	}
	if !strings.Contains(got, `Bob & 25`) {
		t.Errorf("expected data row Bob & 25, got %q", got)
	}
	if !strings.Contains(got, `\end{tabularx}`) {
		t.Errorf("expected \\end{tabularx}, got %q", got)
	}
}

func TestMarkdownToLatex_TableWithFormatting(t *testing.T) {
	input := "| Item | Note |\n| --- | --- |\n| **bold** | *italic* |"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\textbf{bold}`) {
		t.Errorf("expected bold in cell, got %q", got)
	}
	if !strings.Contains(got, `\textit{italic}`) {
		t.Errorf("expected italic in cell, got %q", got)
	}
}

func TestMarkdownToLatex_TableWithSpecialChars(t *testing.T) {
	input := "| Key | Value |\n| --- | --- |\n| Price & tax | 100% |"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `Price \& tax`) {
		t.Errorf("expected escaped & in cell, got %q", got)
	}
	if !strings.Contains(got, `100\%`) {
		t.Errorf("expected escaped %% in cell, got %q", got)
	}
}

func TestMarkdownToLatex_TableWithColumnWidths(t *testing.T) {
	input := "| Name | Age |\n|--- 60%---|--- 40%---|\n| Alice | 30 |"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\hsize=1.20\hsize`) {
		t.Errorf("expected 60%% column (1.20 multiplier), got %q", got)
	}
	if !strings.Contains(got, `\hsize=0.80\hsize`) {
		t.Errorf("expected 40%% column (0.80 multiplier), got %q", got)
	}
	if !strings.Contains(got, `Alice & 30`) {
		t.Errorf("expected data row, got %q", got)
	}
}

func TestMarkdownToLatex_TableWithPartialWidths(t *testing.T) {
	// Only first column has a percentage; second defaults to equal share.
	input := "| Name | Age |\n|--- 70%---|------|\n| Alice | 30 |"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\hsize=1.40\hsize`) {
		t.Errorf("expected 70%% column (1.40 multiplier), got %q", got)
	}
	if !strings.Contains(got, `\hsize=1.00\hsize`) {
		t.Errorf("expected default 50%% column (1.00 multiplier), got %q", got)
	}
}

func TestMarkdownToLatex_TableInMixedContent(t *testing.T) {
	input := "# Title\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\nSome text after"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `\fontsize{18}{22}\selectfont\sffamily\bfseries Title}`) {
		t.Errorf("expected heading before table, got %q", got)
	}
	if !strings.Contains(got, `\begin{tabularx}`) {
		t.Errorf("expected table, got %q", got)
	}
	if !strings.Contains(got, "Some text after") {
		t.Errorf("expected text after table, got %q", got)
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

func TestMarkdownToLatex_AlignCenter(t *testing.T) {
	got := MarkdownToLatex("->Centered text<-")
	want := `{\centering Centered text\par}`
	if got != want {
		t.Errorf("center alignment: got %q, want %q", got, want)
	}
}

func TestMarkdownToLatex_AlignRight(t *testing.T) {
	got := MarkdownToLatex("->Right aligned->")
	want := `{\raggedleft Right aligned\par}`
	if got != want {
		t.Errorf("right alignment: got %q, want %q", got, want)
	}
}

func TestMarkdownToLatex_AlignCenterWithBold(t *testing.T) {
	got := MarkdownToLatex("->**bold centered**<-")
	want := `{\centering \textbf{bold centered}\par}`
	if got != want {
		t.Errorf("center+bold: got %q, want %q", got, want)
	}
}

func TestMarkdownToLatex_AlignRightWithItalic(t *testing.T) {
	got := MarkdownToLatex("->*italic right*->")
	want := `{\raggedleft \textit{italic right}\par}`
	if got != want {
		t.Errorf("right+italic: got %q, want %q", got, want)
	}
}

func TestMarkdownToLatex_AlignCenterWithSpecialChars(t *testing.T) {
	got := MarkdownToLatex("->Price & 100%<-")
	if !strings.Contains(got, `{\centering`) {
		t.Errorf("expected centering, got %q", got)
	}
	if !strings.Contains(got, `Price \& 100\%`) {
		t.Errorf("expected escaped chars, got %q", got)
	}
}

func TestMarkdownToLatex_AlignInMixedContent(t *testing.T) {
	input := "Normal text\n\n->Centered<-\n\nMore text"
	got := MarkdownToLatex(input)
	if !strings.Contains(got, `{\centering Centered\par}`) {
		t.Errorf("expected centered in mixed content, got %q", got)
	}
	if !strings.Contains(got, "Normal text") {
		t.Errorf("expected normal text, got %q", got)
	}
	if !strings.Contains(got, "More text") {
		t.Errorf("expected more text, got %q", got)
	}
}

func TestMarkdownToLatex_ArrowInMiddleNotMacro(t *testing.T) {
	// Arrows in middle of text should NOT be treated as alignment macros.
	got := MarkdownToLatex("Go from A -> B -> C")
	if strings.Contains(got, `\centering`) || strings.Contains(got, `\raggedleft`) {
		t.Errorf("should not detect alignment in middle-of-text arrows, got %q", got)
	}
}

func TestMarkdownToLatex_HorizontalRule(t *testing.T) {
	got := MarkdownToLatex("---")
	expected := `\vspace{2mm}\noindent\rule{\linewidth}{0.4pt}\vspace{2mm}`
	if got != expected {
		t.Errorf("horizontal rule:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_HorizontalRuleInContext(t *testing.T) {
	got := MarkdownToLatex("Above\n\n---\n\nBelow")
	if !strings.Contains(got, `\rule{\linewidth}{0.4pt}`) {
		t.Errorf("should contain horizontal rule, got %q", got)
	}
	if !strings.Contains(got, "Above") || !strings.Contains(got, "Below") {
		t.Errorf("should contain surrounding text, got %q", got)
	}
}

func TestMarkdownToLatex_SmallCaps(t *testing.T) {
	got := MarkdownToLatex("^^Prague^^")
	expected := `\textsc{Prague}`
	if got != expected {
		t.Errorf("small caps:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_SmallCapsWithBold(t *testing.T) {
	got := MarkdownToLatex("^^**bold small caps**^^")
	expected := `\textsc{\textbf{bold small caps}}`
	if got != expected {
		t.Errorf("bold small caps:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_ForcedLineBreak(t *testing.T) {
	got := MarkdownToLatex(`first\nsecond`)
	expected := `first\\second`
	if got != expected {
		t.Errorf("line break:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_Tilde(t *testing.T) {
	got := MarkdownToLatex("k~tomu")
	expected := `k~tomu`
	if got != expected {
		t.Errorf("tilde non-breaking space:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_EscapedTilde(t *testing.T) {
	got := MarkdownToLatex(`\~`)
	expected := `\textasciitilde{}`
	if got != expected {
		t.Errorf("escaped tilde:\ngot:  %q\nwant: %q", got, expected)
	}
}

func TestMarkdownToLatex_TildeMixed(t *testing.T) {
	// \~ should produce literal tilde, ~ should produce non-breaking space.
	got := MarkdownToLatex(`price \~100~Kč`)
	if !strings.Contains(got, `\textasciitilde{}`) {
		t.Errorf("should contain literal tilde, got %q", got)
	}
	if !strings.Contains(got, `100~Kč`) {
		t.Errorf("should contain non-breaking space tilde, got %q", got)
	}
}

func TestMarkdownToLatex_MacroCombinations(t *testing.T) {
	got := MarkdownToLatex(`^^Name^^ lived k~obci\nand worked`)
	if !strings.Contains(got, `\textsc{Name}`) {
		t.Errorf("should contain small caps, got %q", got)
	}
	if !strings.Contains(got, `k~obci`) {
		t.Errorf("should contain non-breaking space, got %q", got)
	}
	if !strings.Contains(got, `\\`) {
		t.Errorf("should contain line break, got %q", got)
	}
}

func TestRelativeLuminance(t *testing.T) {
	tests := []struct {
		hex  string
		want float64
		tol  float64
	}{
		{"000000", 0.0, 0.001},
		{"FFFFFF", 1.0, 0.001},
		{"FF0000", 0.2126, 0.001}, // pure red
		{"00FF00", 0.7152, 0.001}, // pure green
		{"0000FF", 0.0722, 0.001}, // pure blue
		{"808080", 0.216, 0.01},   // mid gray
		{"8B0000", 0.05, 0.01},    // dark red
		{"FFFF00", 0.9278, 0.001}, // yellow (bright)
	}
	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			got := RelativeLuminance(tt.hex)
			if got < tt.want-tt.tol || got > tt.want+tt.tol {
				t.Errorf("RelativeLuminance(%q) = %f, want ~%f", tt.hex, got, tt.want)
			}
		})
	}
}

func TestContrastTextColorLatex(t *testing.T) {
	tests := []struct {
		hex  string
		want string
	}{
		{"000000", "white"}, // black bg → white text
		{"FFFFFF", "black"}, // white bg → black text
		{"8B0000", "white"}, // dark red → white text
		{"FFFF00", "black"}, // yellow → black text
		{"1a1a2e", "white"}, // dark navy → white text
		{"F5F5DC", "black"}, // beige → black text
	}
	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			got := contrastTextColorLatex(tt.hex)
			if got != tt.want {
				t.Errorf("contrastTextColorLatex(%q) = %q, want %q", tt.hex, got, tt.want)
			}
		})
	}
}

func TestMarkdownToLatexWithColor_DarkBackground(t *testing.T) {
	got := MarkdownToLatexWithColor("# Title", "8B0000")
	if !strings.Contains(got, `\textcolor{white}{Title}`) {
		t.Errorf("dark bg should use white text, got %q", got)
	}
}

func TestMarkdownToLatexWithColor_LightBackground(t *testing.T) {
	got := MarkdownToLatexWithColor("# Title", "FFFF00")
	if !strings.Contains(got, `\textcolor{black}{Title}`) {
		t.Errorf("light bg should use black text, got %q", got)
	}
}
