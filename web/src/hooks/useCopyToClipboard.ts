import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { copyToClipboard } from '../utils/clipboard';
import { useToast } from '../components/Toast';

// useCopyToClipboard wraps the clipboard utility with toast feedback. Pass
// an optional `successMessage` to override the generic "Copied to clipboard"
// label (e.g. "Share URL copied" for the ShareModal).
export function useCopyToClipboard() {
  const { t } = useTranslation('common');
  const toast = useToast();

  return useCallback(
    async (text: string, successMessage?: string): Promise<boolean> => {
      const ok = await copyToClipboard(text);
      if (ok) {
        toast.success(successMessage ?? t('toasts.clipboard.copied'));
      } else {
        toast.error(t('toasts.clipboard.copyFailed'));
      }
      return ok;
    },
    [toast, t],
  );
}
