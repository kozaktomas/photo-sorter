import type { CSSProperties } from 'react';
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

export function defaultSplitPosition(format: PageFormat): number {
  switch (format) {
    case '2l_1p': return 2 / 3;
    case '1p_2l': return 1 / 3;
    default: return 0.5;
  }
}

export function isMultiColumn(format: PageFormat): boolean {
  return format !== '1_fullscreen';
}

export function getGridColumnStyle(format: PageFormat, splitPosition: number | null): CSSProperties {
  if (format === '1_fullscreen') return {};
  const split = splitPosition ?? defaultSplitPosition(format);
  return { gridTemplateColumns: `${split}fr ${1 - split}fr` };
}

export function getGridClasses(format: PageFormat): string {
  switch (format) {
    case '4_landscape':
      return 'grid grid-rows-2';
    case '2l_1p':
      return 'grid grid-rows-2';
    case '1p_2l':
      return 'grid grid-rows-2';
    case '2_portrait':
      return 'grid grid-rows-1';
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

export function getSlotTextContent(page: BookPage, slotIndex: number): string {
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  return slot?.text_content || '';
}

export function getSlotCrop(page: BookPage, slotIndex: number): { cropX: number; cropY: number; cropScale: number } {
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  return { cropX: slot?.crop_x ?? 0.5, cropY: slot?.crop_y ?? 0.5, cropScale: slot?.crop_scale ?? 1.0 };
}

// Layout constants mirroring internal/latex/formats.go
const CONTENT_WIDTH = 265; // 297 - 20 - 12
const CANVAS_HEIGHT = 172;
const COLUMN_GUTTER = 4;
const ROW_GAP = 4;
const GRID_COLUMNS = 12;

const COL_WIDTH = (CONTENT_WIDTH - (GRID_COLUMNS - 1) * COLUMN_GUTTER) / GRID_COLUMNS;

function colSpanWidth(n: number): number {
  return n * COL_WIDTH + (n - 1) * COLUMN_GUTTER;
}

const HALF_CANVAS_HEIGHT = (CANVAS_HEIGHT - ROW_GAP) / 2;

/** Returns the W/H aspect ratio for a given slot in a page format. */
export function getSlotAspectRatio(format: PageFormat, slotIndex: number, splitPosition?: number | null): number {
  // With custom split position
  if (splitPosition != null && format !== '1_fullscreen') {
    const availW = CONTENT_WIDTH - COLUMN_GUTTER;
    const leftW = availW * splitPosition;
    const rightW = availW * (1 - splitPosition);

    switch (format) {
      case '2_portrait':
        return (slotIndex === 0 ? leftW : rightW) / CANVAS_HEIGHT;
      case '4_landscape':
        return (slotIndex % 2 === 0 ? leftW : rightW) / HALF_CANVAS_HEIGHT;
      case '2l_1p':
        return slotIndex < 2
          ? leftW / HALF_CANVAS_HEIGHT
          : rightW / CANVAS_HEIGHT;
      case '1p_2l':
        return slotIndex === 0
          ? leftW / CANVAS_HEIGHT
          : rightW / HALF_CANVAS_HEIGHT;
    }
  }

  // Default grid-based dimensions (no custom split)
  const halfW = colSpanWidth(6);

  switch (format) {
    case '1_fullscreen':
      return CONTENT_WIDTH / CANVAS_HEIGHT;
    case '2_portrait':
      return halfW / CANVAS_HEIGHT;
    case '4_landscape':
      return halfW / HALF_CANVAS_HEIGHT;
    case '2l_1p':
      return slotIndex < 2
        ? colSpanWidth(8) / HALF_CANVAS_HEIGHT
        : colSpanWidth(4) / CANVAS_HEIGHT;
    case '1p_2l':
      return slotIndex === 0
        ? colSpanWidth(4) / CANVAS_HEIGHT
        : colSpanWidth(8) / HALF_CANVAS_HEIGHT;
  }
}
