import type { CSSProperties } from 'react';
import type { BookPage, PageFormat } from '../types';

export function pageFormatSlotCount(format: PageFormat): number {
  switch (format) {
    case '4_landscape': return 4;
    case '2l_1p': return 3;
    case '1p_2l': return 3;
    case '2_portrait': return 2;
    case '1_fullscreen': return 1;
    case '1_fullbleed': return 1;
  }
}

export function pageFormatLabelKey(format: PageFormat): string {
  switch (format) {
    case '4_landscape': return 'books.editor.formatShort4Landscape';
    case '2l_1p': return 'books.editor.formatShort2l1p';
    case '1p_2l': return 'books.editor.formatShort1p2l';
    case '2_portrait': return 'books.editor.formatShort2Portrait';
    case '1_fullscreen': return 'books.editor.formatShort1Fullscreen';
    case '1_fullbleed': return 'books.editor.formatShort1Fullbleed';
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
  return format !== '1_fullscreen' && format !== '1_fullbleed';
}

export function getGridColumnStyle(format: PageFormat, splitPosition: number | null): CSSProperties {
  if (format === '1_fullscreen' || format === '1_fullbleed') return {};
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
    case '1_fullbleed':
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

// Returns extra padding class for text slots adjacent to photos in mixed layouts.
// Mirrors applyTextSlotPadding in internal/latex/latex.go.
export function getTextSlotPaddingClass(page: BookPage, slotIndex: number): string {
  const format = page.format;
  if (format !== '1p_2l' && format !== '2l_1p') return '';

  // Only applies to text slots.
  const slot = page.slots.find(s => s.slot_index === slotIndex);
  if (!slot?.text_content) return '';

  // Only if there's at least one photo sibling.
  const hasPhoto = page.slots.some(s => s.photo_uid);
  if (!hasPhoto) return '';

  if (format === '1p_2l') {
    return slotIndex === 0 ? 'pr-4' : 'pl-4';
  }
  // 2l_1p
  return slotIndex <= 1 ? 'pr-4' : 'pl-4';
}

// Returns H1 bleed direction for a text slot. Mirrors isSlotOnLeftEdge/isSlotOnRightEdge in latex.go.
export function getSlotH1Bleed(format: string, slotIndex: number): { left: boolean; right: boolean } {
  switch (format) {
    case '1_fullscreen':
      return { left: true, right: true };
    case '1_fullbleed':
      return { left: true, right: true };
    case '2_portrait':
      return { left: slotIndex === 0, right: slotIndex === 1 };
    case '4_landscape':
      return { left: slotIndex === 0 || slotIndex === 2, right: slotIndex === 1 || slotIndex === 3 };
    case '1p_2l':
      return { left: slotIndex === 0, right: slotIndex === 1 || slotIndex === 2 };
    case '2l_1p':
      return { left: slotIndex === 0 || slotIndex === 1, right: slotIndex === 2 };
    default:
      return { left: true, right: true };
  }
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

export interface SlotRect {
  x: number; // mm from content left
  y: number; // mm from canvas top
  w: number; // mm
  h: number; // mm
}

/** Returns slot rectangles (position + size in mm) for all slots in a page format. */
export function getSlotRects(format: PageFormat, splitPosition: number | null): SlotRect[] {
  const availW = CONTENT_WIDTH - COLUMN_GUTTER;
  const sp = splitPosition ?? (format === '2l_1p' ? 2 / 3 : format === '1p_2l' ? 1 / 3 : 0.5);
  const leftW = availW * sp;
  const rightW = availW * (1 - sp);
  const rightX = leftW + COLUMN_GUTTER;

  switch (format) {
    case '1_fullscreen':
      return [{ x: 0, y: 0, w: CONTENT_WIDTH, h: CANVAS_HEIGHT }];
    case '1_fullbleed':
      return [{ x: 0, y: 0, w: CONTENT_WIDTH, h: CANVAS_HEIGHT }];
    case '2_portrait':
      return [
        { x: 0, y: 0, w: leftW, h: CANVAS_HEIGHT },
        { x: rightX, y: 0, w: rightW, h: CANVAS_HEIGHT },
      ];
    case '4_landscape':
      return [
        { x: 0, y: 0, w: leftW, h: HALF_CANVAS_HEIGHT },
        { x: rightX, y: 0, w: rightW, h: HALF_CANVAS_HEIGHT },
        { x: 0, y: HALF_CANVAS_HEIGHT + ROW_GAP, w: leftW, h: HALF_CANVAS_HEIGHT },
        { x: rightX, y: HALF_CANVAS_HEIGHT + ROW_GAP, w: rightW, h: HALF_CANVAS_HEIGHT },
      ];
    case '2l_1p':
      return [
        { x: 0, y: 0, w: leftW, h: HALF_CANVAS_HEIGHT },
        { x: 0, y: HALF_CANVAS_HEIGHT + ROW_GAP, w: leftW, h: HALF_CANVAS_HEIGHT },
        { x: rightX, y: 0, w: rightW, h: CANVAS_HEIGHT },
      ];
    case '1p_2l':
      return [
        { x: 0, y: 0, w: leftW, h: CANVAS_HEIGHT },
        { x: rightX, y: 0, w: rightW, h: HALF_CANVAS_HEIGHT },
        { x: rightX, y: HALF_CANVAS_HEIGHT + ROW_GAP, w: rightW, h: HALF_CANVAS_HEIGHT },
      ];
  }
}

/** Returns the physical slot dimensions [widthMm, heightMm] for a given slot in a page format. */
function getSlotDimensionsMm(format: PageFormat, slotIndex: number, splitPosition?: number | null): [number, number] {
  if (splitPosition != null && format !== '1_fullscreen' && format !== '1_fullbleed') {
    const availW = CONTENT_WIDTH - COLUMN_GUTTER;
    const leftW = availW * splitPosition;
    const rightW = availW * (1 - splitPosition);

    switch (format) {
      case '2_portrait':
        return [slotIndex === 0 ? leftW : rightW, CANVAS_HEIGHT];
      case '4_landscape':
        return [slotIndex % 2 === 0 ? leftW : rightW, HALF_CANVAS_HEIGHT];
      case '2l_1p':
        return slotIndex < 2
          ? [leftW, HALF_CANVAS_HEIGHT]
          : [rightW, CANVAS_HEIGHT];
      case '1p_2l':
        return slotIndex === 0
          ? [leftW, CANVAS_HEIGHT]
          : [rightW, HALF_CANVAS_HEIGHT];
    }
  }

  const halfW = colSpanWidth(6);
  switch (format) {
    case '1_fullscreen':
      return [CONTENT_WIDTH, CANVAS_HEIGHT];
    case '1_fullbleed':
      return [CONTENT_WIDTH, CANVAS_HEIGHT];
    case '2_portrait':
      return [halfW, CANVAS_HEIGHT];
    case '4_landscape':
      return [halfW, HALF_CANVAS_HEIGHT];
    case '2l_1p':
      return slotIndex < 2
        ? [colSpanWidth(8), HALF_CANVAS_HEIGHT]
        : [colSpanWidth(4), CANVAS_HEIGHT];
    case '1p_2l':
      return slotIndex === 0
        ? [colSpanWidth(4), CANVAS_HEIGHT]
        : [colSpanWidth(8), HALF_CANVAS_HEIGHT];
  }
}

/** Computes the effective print DPI for a photo in a given slot. */
export function computeEffectiveDpi(
  naturalW: number, naturalH: number,
  format: PageFormat, slotIndex: number,
  splitPosition?: number | null,
): number {
  const [slotW, slotH] = getSlotDimensionsMm(format, slotIndex, splitPosition);
  const dpiW = (naturalW / slotW) * 25.4;
  const dpiH = (naturalH / slotH) * 25.4;
  return Math.round(Math.min(dpiW, dpiH));
}

/** Returns the W/H aspect ratio for a given slot in a page format. */
export function getSlotAspectRatio(format: PageFormat, slotIndex: number, splitPosition?: number | null): number {
  // With custom split position
  if (splitPosition != null && format !== '1_fullscreen' && format !== '1_fullbleed') {
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
    case '1_fullbleed':
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
