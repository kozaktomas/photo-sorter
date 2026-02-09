import type { BookPage, PageFormat } from '../types';

export function pageFormatSlotCount(format: PageFormat): number {
  switch (format) {
    case '4_landscape': return 4;
    case '2l_1p': return 3;
    case '1p_2l': return 3;
    case '2_portrait': return 2;
    case '1_fullscreen': return 1;
  }
}

export function pageFormatLabelKey(format: PageFormat): string {
  switch (format) {
    case '4_landscape': return 'books.editor.formatShort4Landscape';
    case '2l_1p': return 'books.editor.formatShort2l1p';
    case '1p_2l': return 'books.editor.formatShort1p2l';
    case '2_portrait': return 'books.editor.formatShort2Portrait';
    case '1_fullscreen': return 'books.editor.formatShort1Fullscreen';
  }
}

export function getGridClasses(format: PageFormat): string {
  switch (format) {
    case '4_landscape':
      return 'grid grid-cols-2 grid-rows-2';
    case '2l_1p':
      return 'grid grid-cols-[2fr_1fr] grid-rows-2';
    case '1p_2l':
      return 'grid grid-cols-[1fr_2fr] grid-rows-2';
    case '2_portrait':
      return 'grid grid-cols-2 grid-rows-1';
    case '1_fullscreen':
      return 'grid grid-cols-1 grid-rows-1';
  }
}

export function getSlotClasses(format: PageFormat, slotIndex: number): string {
  if (format === '2l_1p') {
    if (slotIndex === 2) return 'col-start-2 row-start-1 row-span-2';
    return '';
  }
  if (format === '1p_2l') {
    if (slotIndex === 0) return 'row-span-2';
    return '';
  }
  return '';
}

export function getSlotPhotoUid(page: BookPage, slotIndex: number): string {
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  return slot?.photo_uid || '';
}
