import TurndownService from 'turndown';
import { gfm } from 'turndown-plugin-gfm';

const turndown = new TurndownService({
  headingStyle: 'atx',
  bulletListMarker: '-',
  codeBlockStyle: 'fenced',
});
turndown.use(gfm as TurndownService.Plugin);

/**
 * Handle paste events on textareas, converting HTML clipboard content to Markdown.
 * If no HTML is present, falls through to default paste behavior.
 */
export function handleMarkdownPaste(
  e: React.ClipboardEvent<HTMLTextAreaElement>,
  setValue: (v: string) => void,
): void {
  const html = e.clipboardData.getData('text/html');
  if (!html) return;

  e.preventDefault();

  const markdown = turndown.turndown(html);
  const textarea = e.currentTarget;
  const { selectionStart, selectionEnd, value } = textarea;
  const newValue = value.slice(0, selectionStart) + markdown + value.slice(selectionEnd);
  setValue(newValue);

  // Restore cursor position after the inserted text
  requestAnimationFrame(() => {
    const pos = selectionStart + markdown.length;
    textarea.setSelectionRange(pos, pos);
  });
}
