// Shared book typography configuration matching the LaTeX PDF export.
// NOTE: When changing these values, also update the LaTeX template at
// internal/latex/templates/book.tex to keep PDF output in sync.

import type { BookDetail, FontInfo } from '../types';

// Font registry cache (loaded from API)
let fontRegistryCache: FontInfo[] | null = null;

export function setFontRegistry(fonts: FontInfo[]): void {
  fontRegistryCache = fonts;
}

export function getFontRegistry(): FontInfo[] {
  return fontRegistryCache ?? [];
}

export function findFont(id: string): FontInfo | undefined {
  return getFontRegistry().find(f => f.id === id);
}

export function getBookTypography(book: BookDetail) {
  const bodyFont = findFont(book.body_font);
  const headingFont = findFont(book.heading_font);

  const bodyFontFamily = bodyFont
    ? `'${bodyFont.display_name}', ${bodyFont.category}`
    : BOOK_TYPOGRAPHY.textSlot.fontFamily;
  const headingFontFamily = headingFont
    ? `'${headingFont.display_name}', ${headingFont.category}`
    : BOOK_TYPOGRAPHY.headingFontFamily;

  return {
    textSlot: {
      fontFamily: bodyFontFamily,
      fontSize: `${book.body_font_size}pt`,
      lineHeight: `${book.body_line_height}pt`,
    },
    headingFontFamily,
    h1: { ...BOOK_TYPOGRAPHY.h1, fontSize: `${book.h1_font_size}pt` },
    h2: { ...BOOK_TYPOGRAPHY.h2, fontSize: `${book.h2_font_size}pt` },
    h3: BOOK_TYPOGRAPHY.h3,
    blockquote: BOOK_TYPOGRAPHY.blockquote,
    paragraph: BOOK_TYPOGRAPHY.paragraph,
    list: BOOK_TYPOGRAPHY.list,
  };
}

// Returns CSS custom properties to set on a container for typography inheritance
export function getBookTypographyCSSVars(book: BookDetail): Record<string, string> {
  const bodyFont = findFont(book.body_font);
  const headingFont = findFont(book.heading_font);

  return {
    '--book-body-font': bodyFont
      ? `'${bodyFont.display_name}', ${bodyFont.category}`
      : "'PT Serif', serif",
    '--book-heading-font': headingFont
      ? `'${headingFont.display_name}', ${headingFont.category}`
      : "'Source Sans 3', sans-serif",
    '--book-body-size': `${book.body_font_size}pt`,
    '--book-body-line-height': `${book.body_line_height}pt`,
    '--book-h1-size': `${book.h1_font_size}pt`,
    '--book-h2-size': `${book.h2_font_size}pt`,
  };
}

export const BOOK_TYPOGRAPHY = {
  textSlot: {
    fontFamily: "'PT Serif', serif",
    fontSize: '11pt',       // LaTeX \fontsize{11}{15}
    lineHeight: '15pt',
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
    fontSize: '13pt',       // LaTeX \fontsize{13}{16} — Source Sans 3
    fontWeight: 700,
    color: undefined as string | undefined,
    backgroundColor: undefined as string | undefined,
    padding: undefined as string | undefined,
    borderRadius: undefined as string | undefined,
    marginBottom: '4mm',
  },
  h3: {
    fontSize: '11pt',       // Source Sans 3
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
  bleed: 3,                 // mirrors BleedMM in internal/latex/formats.go
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
