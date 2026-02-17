import DOMPurify from 'dompurify';
import { Marked } from 'marked';
import React from 'react';

const marked = new Marked({
  breaks: true,
  gfm: true,
});

export function renderMarkdown(md: string): string {
  return DOMPurify.sanitize(marked.parse(md) as string);
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
