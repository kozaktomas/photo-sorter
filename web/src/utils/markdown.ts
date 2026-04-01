import DOMPurify from 'dompurify';
import { Marked } from 'marked';
import React from 'react';

const marked = new Marked({
  breaks: true,
  gfm: true,
});

const sepRowRe = /^(\s*\|[\s:%-\d]+(?:\|[\s:%-\d]+)*\|)$/gm;
const pctRe = /(\d+)%/;

// Extract column width percentages from table separator rows.
// Returns an array of widths per table found, e.g. [[60, 40], [30, 70]].
function extractTableWidths(md: string): number[][] {
  const tables: number[][] = [];
  for (const match of md.matchAll(sepRowRe)) {
    const cells = match[1].trim().replace(/^\||\|$/g, '').split('|');
    const widths = cells.map((c) => {
      const m = pctRe.exec(c);
      return m ? parseInt(m[1], 10) : 0;
    });
    if (widths.some((w) => w > 0)) {
      tables.push(widths);
    }
  }
  return tables;
}

// Strip percentage width hints from table separator rows so marked.js
// recognizes them as valid GFM tables. E.g. "|--- 60%---|" → "|---|"
function stripTableWidthHints(md: string): string {
  return md.replace(sepRowRe, (line) => line.replace(/\s*\d+%\s*/g, ''));
}

// Inject <colgroup> with width styles into rendered HTML tables.
function applyTableWidths(html: string, widths: number[][]): string {
  let tableIndex = 0;
  return html.replace(/<table>/g, (tag) => {
    if (tableIndex < widths.length) {
      const cols = widths[tableIndex++]
        .map((w) => (w > 0 ? `<col style="width:${w}%">` : '<col>'))
        .join('');
      return `<table><colgroup>${cols}</colgroup>`;
    }
    return tag;
  });
}

// Alignment macros: ->text<- (center), ->text-> (right-align).
// Applied per-line as a pre-processing step before marked.js.
const alignCenterRe = /^->\s*(.*?)\s*<-$/;
const alignRightRe = /^->\s*(.*?)\s*->$/;

function processAlignmentMacros(md: string): string {
  return md
    .split('\n')
    .map((line) => {
      const trimmed = line.trim();
      const centerMatch = alignCenterRe.exec(trimmed);
      if (centerMatch) {
        // Use parseInline so **bold**/*italic* inside the macro get rendered.
        // marked.parse would treat <p> as an HTML block and skip inline processing.
        const inner = marked.parseInline(centerMatch[1]) as string;
        return `<p style="text-align:center">${inner}</p>`;
      }
      const rightMatch = alignRightRe.exec(trimmed);
      if (rightMatch) {
        const inner = marked.parseInline(rightMatch[1]) as string;
        return `<p style="text-align:right">${inner}</p>`;
      }
      return line;
    })
    .join('\n');
}

// Small caps: ^^text^^ → <span style="font-variant:small-caps">text</span>
const smallCapsRe = /\^\^(.+?)\^\^/g;

// Inline macro post-processing applied after marked.js rendering.
// Handles: ^^small caps^^, \n (line break).
// Note: ~ (non-breaking space) and \~ (literal tilde) are handled via pre/post-processing
// around marked.parse() to prevent GFM strikethrough from consuming ~~ pairs.
function processInlineMacros(html: string): string {
  // Small caps.
  html = html.replace(smallCapsRe, '<span style="font-variant:small-caps">$1</span>');

  // Forced line break: literal \n → <br />.
  html = html.replace(/\\n/g, '<br />');

  return html;
}

// Placeholders for tilde pre-processing (protect from GFM strikethrough).
const ESCAPED_TILDE = '\x00ESC_TILDE\x00';
const TILDE_PLACEHOLDER = '\x00TILDE\x00';

export function renderMarkdown(md: string): string {
  const widths = extractTableWidths(md);

  // Pre-process: protect tildes from GFM strikethrough (~~text~~ → <del>).
  // Escaped \~ first, then bare ~, so marked.js never sees ~~ pairs.
  let preprocessed = md.replace(/\\~/g, ESCAPED_TILDE);
  preprocessed = preprocessed.replace(/~/g, TILDE_PLACEHOLDER);

  let html = marked.parse(processAlignmentMacros(stripTableWidthHints(preprocessed))) as string;

  // Post-process: restore tildes as &nbsp; (or literal ~ for escaped).
  html = html.replaceAll(TILDE_PLACEHOLDER, '&nbsp;');
  html = html.replaceAll(ESCAPED_TILDE, '~');

  const withWidths = widths.length > 0 ? applyTableWidths(html, widths) : html;
  const withMacros = processInlineMacros(withWidths);
  return DOMPurify.sanitize(withMacros, {
    ADD_ATTR: ['style'],
  });
}

// Luminance threshold for switching between white and dark H1 text on chapter-colored backgrounds.
const LUMINANCE_THRESHOLD = 0.5;

// Compute WCAG 2.0 relative luminance from a hex color string (e.g. "#8B0000" or "8B0000").
// Returns a value between 0 (black) and 1 (white).
export function relativeLuminance(hex: string): number {
  const h = hex.replace(/^#/, '');
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const linearize = (c: number) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));
  return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b);
}

// Return white or dark text color based on background luminance.
export function contrastTextColor(bgHex: string): string {
  return relativeLuminance(bgHex) < LUMINANCE_THRESHOLD ? '#ffffff' : '#1a1a1a';
}

interface MarkdownContentProps {
  content: string;
  className?: string;
  chapterColor?: string;
}

export function MarkdownContent({ content, className, chapterColor }: MarkdownContentProps) {
  const html = renderMarkdown(content);
  const style: Record<string, string> = {};
  if (chapterColor) {
    style['--chapter-color'] = chapterColor;
    style['--chapter-text-color'] = contrastTextColor(chapterColor);
  }
  return React.createElement('div', {
    className: `markdown-content ${className || ''}`,
    style: chapterColor ? style as unknown as React.CSSProperties : undefined,
    dangerouslySetInnerHTML: { __html: html },
  });
}
