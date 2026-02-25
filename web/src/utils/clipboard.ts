/**
 * Copy text to clipboard with fallback for non-secure contexts (plain HTTP).
 * navigator.clipboard.writeText() requires HTTPS or localhost.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  // Try modern Clipboard API first (only available in secure contexts)
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition
  if (navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to fallback
    }
  }

  // Fallback: hidden textarea + execCommand (deprecated but works over plain HTTP)
  try {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    textarea.style.top = '-9999px';
    document.body.appendChild(textarea);
    textarea.select();
    // eslint-disable-next-line @typescript-eslint/no-deprecated
    const ok = document.execCommand('copy');
    document.body.removeChild(textarea);
    return ok;
  } catch {
    return false;
  }
}
