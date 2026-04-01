// Shared book typography configuration matching the LaTeX PDF export.
// NOTE: When changing these values, also update the LaTeX template at
// internal/latex/templates/book.tex to keep PDF output in sync.

export const BOOK_TYPOGRAPHY = {
  textSlot: {
    fontFamily: "'EB Garamond', serif",
    fontSize: '13pt',       // LaTeX \fontsize{13}{16}
    lineHeight: '16pt',
  },
  headingFontFamily: "'Source Sans 3', sans-serif",  // LaTeX \setsansfont{SourceSans3}
  h1: {
    fontSize: '18pt',       // LaTeX \fontsize{18}{22}
    fontWeight: 700,
    color: undefined as string | undefined,
    backgroundColor: undefined as string | undefined,
    padding: undefined as string | undefined,
    borderRadius: undefined as string | undefined,
    marginBottom: '4mm',    // LaTeX \vspace{4mm}
  },
  h2: {
    fontSize: '15pt',       // LaTeX \fontsize{15}{18}
    fontWeight: 700,
    color: undefined as string | undefined,
    backgroundColor: undefined as string | undefined,
    padding: undefined as string | undefined,
    borderRadius: undefined as string | undefined,
    marginBottom: '4mm',
  },
  h3: {
    fontSize: '13pt',
    fontWeight: 700,
    marginBottom: '2mm',
  },
  blockquote: {
    fontStyle: 'italic' as const,
  },
  paragraph: {
    marginBottom: '4mm',    // LaTeX \par\vspace{4mm} between paragraphs
  },
  list: {
    paddingLeft: '1.5em',   // LaTeX leftmargin=1.5em
    marginBottom: '4mm',
  },
} as const;

// Physical page dimensions in mm (shared with PageLayoutPreview)
export const PAGE_DIMENSIONS = {
  pageWidth: 297,           // A4 landscape
  pageHeight: 210,
  marginInside: 20,
  marginOutside: 12,
  headerHeight: 4,
  footerHeight: 8,
  canvasHeight: 172,
  contentWidth: 265,        // 297 - 20 - 12
  columnGutter: 4,
  rowGap: 4,
  halfCanvas: 84,           // (172 - 4) / 2
} as const;
