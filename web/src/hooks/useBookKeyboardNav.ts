import { useEffect, useCallback } from 'react';

interface Options<T> {
  items: T[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  getId: (item: T) => string;
  getChapterId: (item: T) => string;
  chapters: { id: string }[];
  enabled?: boolean;
}

/**
 * Keyboard navigation for book editor lists (pages or sections).
 * W/S = prev/next item, E/D = prev/next chapter.
 */
export function useBookKeyboardNav<T>({
  items,
  selectedId,
  onSelect,
  getId,
  getChapterId,
  chapters,
  enabled = true,
}: Options<T>) {
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (!enabled || items.length === 0) return;

    // Don't fire when typing in inputs
    const tag = (document.activeElement?.tagName ?? '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
    // Also skip contenteditable elements
    if (document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable) return;

    const key = e.key.toLowerCase();
    if (key !== 'w' && key !== 's' && key !== 'e' && key !== 'd') return;

    e.preventDefault();

    const currentIndex = items.findIndex(item => getId(item) === selectedId);

    if (key === 'w') {
      // Previous item
      const newIndex = currentIndex <= 0 ? items.length - 1 : currentIndex - 1;
      onSelect(getId(items[newIndex]));
    } else if (key === 's') {
      // Next item
      const newIndex = currentIndex < 0 || currentIndex >= items.length - 1 ? 0 : currentIndex + 1;
      onSelect(getId(items[newIndex]));
    } else {
      // Chapter navigation (E/D)
      if (chapters.length === 0) return;

      const currentItem = currentIndex >= 0 ? items[currentIndex] : null;
      const currentChapterId = currentItem ? getChapterId(currentItem) : '';

      // Build ordered chapter IDs including '' for uncategorized items
      const chapterIds: string[] = [];
      // Check if any items have no chapter
      const hasUncategorized = items.some(item => !getChapterId(item));
      if (hasUncategorized) chapterIds.push('');
      chapters.forEach(ch => chapterIds.push(ch.id));

      const chapterIndex = chapterIds.indexOf(currentChapterId);
      if (chapterIndex < 0) return;

      let targetChapterIndex: number;
      if (key === 'e') {
        // Previous chapter
        if (chapterIndex <= 0) return;
        targetChapterIndex = chapterIndex - 1;
      } else {
        // Next chapter
        if (chapterIndex >= chapterIds.length - 1) return;
        targetChapterIndex = chapterIndex + 1;
      }

      const targetChapterId = chapterIds[targetChapterIndex];
      const firstItem = items.find(item => getChapterId(item) === targetChapterId);
      if (firstItem) {
        onSelect(getId(firstItem));
      }
    }
  }, [enabled, items, selectedId, onSelect, getId, getChapterId, chapters]);

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);
}
