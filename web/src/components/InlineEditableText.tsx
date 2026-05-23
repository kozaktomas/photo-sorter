import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../hooks/useAuth';
import { useToast } from './Toast';

interface InlineEditableTextProps {
  // Current canonical value (shown when not editing and after a successful save
  // when the parent state updates).
  value: string;
  // Called when the user commits a new (trimmed, non-empty) value. The promise
  // should resolve when the API call succeeds. On rejection, the component
  // rolls back to the previous value and shows an error toast.
  onSave: (newValue: string) => Promise<void>;
  // Class names for the static text node. Should match the surrounding
  // heading so the layout does not jump when entering edit mode.
  textClassName?: string;
  // Class names for the input element. Should match `textClassName` for font
  // size / weight; the component adds its own padding + focus ring.
  inputClassName?: string;
  // Accessibility label for the static text (which uses role="button" in
  // editable mode so screen readers announce it as actionable).
  ariaLabel?: string;
  // Hides the edit affordance entirely. Use this for viewers (HasWriteAccess
  // is checked internally too, but a caller may want to disable for other
  // reasons such as a placeholder/loading state).
  disabled?: boolean;
}

// InlineEditableText renders a heading-style label that flips into an
// editable input on double-click. Enter or blur commits; Esc cancels. The
// component performs an optimistic update — the displayed value flips to the
// new text immediately while `onSave` runs, and rolls back with an error
// toast if the save fails.
//
// Only renders the edit affordance when the current user has write access
// (admin or editor). Viewers see the same plain text.
export function InlineEditableText({
  value,
  onSave,
  textClassName,
  inputClassName,
  ariaLabel,
  disabled,
}: InlineEditableTextProps) {
  const { t } = useTranslation('common');
  const { user } = useAuth();
  const toast = useToast();
  const hasWriteAccess = user?.role === 'admin' || user?.role === 'editor';
  const editable = hasWriteAccess && !disabled;

  // `displayed` lets us render the optimistic value while the save is in
  // flight. After a successful save, the parent updates `value` and the
  // effect below re-syncs `displayed`.
  const [displayed, setDisplayed] = useState(value);
  useEffect(() => {
    setDisplayed(value);
  }, [value]);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement>(null);
  // Guard so blur + Enter don't both fire commit (the input loses focus on
  // Enter, which would otherwise trigger blur's handler a second time).
  const committedRef = useRef(false);

  // Focus + select-all on entering edit mode.
  useEffect(() => {
    if (!editing) return;
    const input = inputRef.current;
    if (input) {
      input.focus();
      input.select();
    }
  }, [editing]);

  const startEdit = useCallback(() => {
    if (!editable) return;
    setDraft(displayed);
    committedRef.current = false;
    setEditing(true);
  }, [editable, displayed]);

  const cancelEdit = useCallback(() => {
    committedRef.current = true;
    setEditing(false);
    setDraft(displayed);
  }, [displayed]);

  const commit = useCallback(async () => {
    if (committedRef.current) return;
    committedRef.current = true;

    const trimmed = draft.trim();
    if (trimmed === '') {
      toast.error(t('inlineEdit.emptyNotAllowed'));
      setEditing(false);
      setDraft(displayed);
      return;
    }
    if (trimmed === displayed) {
      setEditing(false);
      return;
    }

    const previous = displayed;
    setDisplayed(trimmed);
    setEditing(false);
    try {
      await onSave(trimmed);
    } catch (err) {
      setDisplayed(previous);
      toast.error(err instanceof Error ? err.message : t('inlineEdit.saveFailed'));
    }
  }, [draft, displayed, onSave, t, toast]);

  if (!editable) {
    return <span className={textClassName}>{displayed}</span>;
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        type="text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            void commit();
          } else if (e.key === 'Escape') {
            e.preventDefault();
            cancelEdit();
          }
        }}
        onBlur={() => void commit()}
        aria-label={ariaLabel}
        className={
          inputClassName ??
          // Fallback styling for callers that don't supply their own; mirrors
          // the standard slate-on-slate input look used elsewhere.
          'bg-slate-800 border border-slate-600 rounded px-2 py-0.5 text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500'
        }
      />
    );
  }

  return (
    <span
      role="button"
      tabIndex={0}
      onDoubleClick={startEdit}
      onKeyDown={(e) => {
        // F2 is the conventional rename shortcut — wire it up for keyboard
        // users since double-click is mouse-only.
        if (e.key === 'F2') {
          e.preventDefault();
          startEdit();
        }
      }}
      aria-label={ariaLabel}
      title={t('inlineEdit.doubleClickToEdit')}
      className={`${textClassName ?? ''} cursor-text hover:bg-slate-700/30 rounded px-1 -mx-1 transition-colors`}
    >
      {displayed}
    </span>
  );
}
