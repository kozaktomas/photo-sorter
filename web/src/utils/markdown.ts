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

export function renderMarkdown(md: string): string {
  const widths = extractTableWidths(md);
  const html = marked.parse(stripTableWidthHints(md)) as string;
  const withWidths = widths.length > 0 ? applyTableWidths(html, widths) : html;
  return DOMPurify.sanitize(withWidths);
}

interface MarkdownContentProps {
  content: string;
  className?: string;
}

export function MarkdownContent({ content, className }: MarkdownContentProps) {
  const html = renderMarkdown(content);
  return React.createElement('div', {
    className: `markdown-content ${className || ''}`,
    dangerouslySetInnerHTML: { __html: html },
  });
}
