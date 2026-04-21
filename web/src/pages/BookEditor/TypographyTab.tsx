import { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { getFonts, updateBook, updateChapter } from '../../api/client';
import { BOOK_TYPOGRAPHY, PAGE_DIMENSIONS, setFontRegistry, getBookTypographyCSSVars } from '../../constants/bookTypography';
import type { BookDetail, FontInfo } from '../../types';
import { loadFontByInfo } from '../../utils/fontLoader';
import { MarkdownContent } from '../../utils/markdown';

interface TypographyTabProps {
  book: BookDetail;
  onRefresh: () => void;
}

// ── Typography Settings Section ────────────────────────────────

function useDebouncedSave() {
  const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  return useCallback((fn: () => Promise<void>, delay: number) => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => void fn(), delay);
  }, []);
}

function FontSelect({
  label,
  value,
  fonts,
  onChange,
}: {
  label: string;
  value: string;
  fonts: FontInfo[];
  onChange: (fontId: string) => void;
}) {
  const { t } = useTranslation('pages');
  const serifFonts = fonts.filter(f => f.category === 'serif');
  const sansFonts = fonts.filter(f => f.category === 'sans-serif');
  const selectedFont = fonts.find(f => f.id === value);

  return (
    <div className="bg-slate-800/50 rounded-lg p-5 border border-slate-700">
      <label className="text-sm font-medium text-slate-400 mb-2 block">{label}</label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full px-3 py-2 bg-slate-900 border border-slate-600 rounded text-white text-sm focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500 mb-3"
      >
        <optgroup label={t('books.editor.typography.serif')}>
          {serifFonts.map(f => (
            <option key={f.id} value={f.id}>{f.display_name}</option>
          ))}
        </optgroup>
        <optgroup label={t('books.editor.typography.sansSerif')}>
          {sansFonts.map(f => (
            <option key={f.id} value={f.id}>{f.display_name}</option>
          ))}
        </optgroup>
      </select>
      {selectedFont && (
        <p
          className="text-slate-200 leading-relaxed"
          style={{
            fontFamily: `'${selectedFont.display_name}', ${selectedFont.category}`,
            fontSize: '11pt',
            lineHeight: '15pt',
          }}
        >
          {t('books.editor.typography.sampleText')}
        </p>
      )}
    </div>
  );
}

function NumberInput({
  label,
  value,
  min,
  max,
  step,
  suffix,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  suffix?: string;
  onChange: (value: number) => void;
}) {
  const [localValue, setLocalValue] = useState(value);

  useEffect(() => { setLocalValue(value); }, [value]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = parseFloat(e.target.value);
    if (!isNaN(v)) {
      setLocalValue(v);
      onChange(v);
    }
  };

  return (
    <div>
      <label className="text-xs text-slate-400 mb-1 block">{label}</label>
      <div className="flex items-center gap-2">
        <input
          type="number"
          value={localValue}
          min={min}
          max={max}
          step={step}
          onChange={handleChange}
          className="w-full px-2 py-1.5 bg-slate-900 border border-slate-600 rounded text-white text-sm focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
        />
        {suffix && <span className="text-xs text-slate-500 shrink-0">{suffix}</span>}
      </div>
    </div>
  );
}

function TypographySettingsSection({ book, onRefresh }: { book: BookDetail; onRefresh: () => void }) {
  const { t } = useTranslation('pages');
  const [fonts, setFonts] = useState<FontInfo[]>([]);
  const [saving, setSaving] = useState(false);
  const debouncedSave = useDebouncedSave();

  // Load font registry
  useEffect(() => {
    getFonts().then(f => {
      setFonts(f);
      setFontRegistry(f);
      // Preload current book's fonts
      const bodyFont = f.find(x => x.id === book.body_font);
      const headingFont = f.find(x => x.id === book.heading_font);
      if (bodyFont) loadFontByInfo(bodyFont);
      if (headingFont) loadFontByInfo(headingFont);
    }).catch(() => { /* ignore font loading errors */ });
  }, [book.body_font, book.heading_font]);

  const handleFontChange = async (field: 'body_font' | 'heading_font', fontId: string) => {
    const font = fonts.find(f => f.id === fontId);
    if (font) loadFontByInfo(font);
    setSaving(true);
    try {
      await updateBook(book.id, { [field]: fontId });
      onRefresh();
    } catch { /* silent */ } finally { setSaving(false); }
  };

  const handleNumberChange = (field: string, value: number) => {
    debouncedSave(async () => {
      setSaving(true);
      try {
        await updateBook(book.id, { [field]: value });
        onRefresh();
      } catch { /* silent */ } finally { setSaving(false); }
    }, 500);
  };

  // Compute live preview CSS vars from current book state
  const previewVars = getBookTypographyCSSVars(book);

  if (!fonts.length) return null;

  return (
    <section>
      <div className="flex items-center gap-3 mb-4">
        <h2 className="text-lg font-semibold text-white">{t('books.editor.typography.fontsTitle')}</h2>
        {saving && (
          <span className="text-xs text-slate-500">{t('books.editor.typography.saving')}</span>
        )}
      </div>

      {/* Font selectors */}
      <div className="grid gap-6 md:grid-cols-2 mb-6">
        <FontSelect
          label={t('books.editor.typography.bodyTextFont')}
          value={book.body_font}
          fonts={fonts}
          onChange={(id) => void handleFontChange('body_font', id)}
        />
        <FontSelect
          label={t('books.editor.typography.headingFont')}
          value={book.heading_font}
          fonts={fonts}
          onChange={(id) => void handleFontChange('heading_font', id)}
        />
      </div>

      {/* Size controls */}
      <div className="grid gap-4 grid-cols-2 md:grid-cols-5 bg-slate-800/50 rounded-lg p-5 border border-slate-700 mb-6">
        <NumberInput
          label={t('books.editor.typography.fontSize')}
          value={book.body_font_size}
          min={6} max={36} step={0.5}
          suffix="pt"
          onChange={(v) => handleNumberChange('body_font_size', v)}
        />
        <NumberInput
          label={t('books.editor.typography.lineHeight')}
          value={book.body_line_height}
          min={8} max={48} step={0.5}
          suffix="pt"
          onChange={(v) => handleNumberChange('body_line_height', v)}
        />
        <NumberInput
          label={t('books.editor.typography.h1Size')}
          value={book.h1_font_size}
          min={6} max={36} step={1}
          suffix="pt"
          onChange={(v) => handleNumberChange('h1_font_size', v)}
        />
        <NumberInput
          label={t('books.editor.typography.h2Size')}
          value={book.h2_font_size}
          min={6} max={36} step={1}
          suffix="pt"
          onChange={(v) => handleNumberChange('h2_font_size', v)}
        />
        <NumberInput
          label={t('books.editor.typography.captionOpacity')}
          value={Math.round(book.caption_opacity * 100)}
          min={0} max={100} step={5}
          suffix="%"
          onChange={(v) => handleNumberChange('caption_opacity', v / 100)}
        />
        <NumberInput
          label={t('books.editor.typography.captionFontSize')}
          value={book.caption_font_size}
          min={6} max={16} step={0.5}
          suffix="pt"
          onChange={(v) => handleNumberChange('caption_font_size', v)}
        />
        <NumberInput
          label={t('books.editor.typography.headingColorBleed')}
          value={book.heading_color_bleed}
          min={0} max={20} step={0.1}
          suffix="mm"
          onChange={(v) => handleNumberChange('heading_color_bleed', v)}
        />
        <NumberInput
          label={t('books.editor.typography.captionBadgeSize')}
          value={book.caption_badge_size}
          min={2} max={12} step={0.5}
          suffix="mm"
          onChange={(v) => handleNumberChange('caption_badge_size', v)}
        />
        <NumberInput
          label={t('books.editor.typography.bodyTextPad')}
          value={book.body_text_pad_mm}
          min={0} max={10} step={0.1}
          suffix="mm"
          onChange={(v) => handleNumberChange('body_text_pad_mm', v)}
        />
      </div>

      {/* Live preview */}
      <div className="bg-slate-800/50 rounded-lg border border-slate-700 overflow-hidden">
        <div className="px-5 py-3 border-b border-slate-700">
          <h3 className="text-sm font-medium text-slate-400">{t('books.editor.typography.livePreview')}</h3>
        </div>
        <div
          className="p-6 bg-white wysiwyg-preview"
          style={previewVars as React.CSSProperties}
        >
          <MarkdownContent
            content={`# ${t('books.editor.typography.previewHeading')}\n\n## ${t('books.editor.typography.previewSubheading')}\n\n${t('books.editor.typography.previewBody')}\n\n- First item\n- Second item\n\n> ${t('books.editor.typography.sampleText')}`}
            className="wysiwyg-preview"
          />
        </div>
      </div>
    </section>
  );
}

// ── Chapter Colors Section ─────────────────────────────────────

function ChapterColorRow({
  chapter,
  onColorChange,
  onHideFromTOCChange,
}: {
  chapter: { id: string; title: string; color: string; hide_from_toc: boolean };
  onColorChange: (color: string) => void;
  onHideFromTOCChange: (hide: boolean) => void;
}) {
  const { t } = useTranslation('pages');
  const colorRef = useRef<HTMLInputElement>(null);
  const typo = BOOK_TYPOGRAPHY;

  return (
    <div className="flex items-center gap-4 bg-slate-800/50 rounded-lg p-4 border border-slate-700">
      {/* Color swatch + picker */}
      <button
        onClick={() => colorRef.current?.click()}
        className="relative h-8 w-8 rounded border border-slate-500 shrink-0"
        style={{ backgroundColor: chapter.color || '#6b7280' }}
        title={t('books.editor.typography.hexCode')}
      >
        <input
          ref={colorRef}
          type="color"
          value={chapter.color || '#6b7280'}
          onChange={(e) => onColorChange(e.target.value)}
          className="absolute inset-0 opacity-0 w-full h-full cursor-pointer"
        />
      </button>

      {/* Show in TOC toggle — right next to the color picker. */}
      <label
        className="flex items-center gap-2 shrink-0 cursor-pointer select-none"
        title={t('books.editor.typography.showInTocTitle')}
      >
        <input
          type="checkbox"
          checked={!chapter.hide_from_toc}
          onChange={(e) => onHideFromTOCChange(!e.target.checked)}
          className="h-4 w-4 rounded border-slate-600 bg-slate-900 accent-rose-500 focus:outline-none focus-visible:ring-1 focus-visible:ring-rose-500"
        />
        <span className="text-xs text-slate-400">{t('books.editor.typography.showInToc')}</span>
      </label>

      {/* Chapter name + hex */}
      <div className="flex-1 min-w-0">
        <div className="text-sm text-white font-medium truncate">{chapter.title}</div>
        <div className="text-xs text-slate-400">
          {chapter.color ? chapter.color : t('books.editor.typography.noColor')}
        </div>
      </div>

      {/* Mini H1 preview */}
      {chapter.color && (
        <div
          className="px-3 py-1 rounded text-white text-sm font-bold shrink-0"
          style={{
            backgroundColor: chapter.color,
            fontFamily: typo.headingFontFamily,
            fontWeight: typo.h1.fontWeight,
          }}
        >
          {chapter.title}
        </div>
      )}
    </div>
  );
}

function ChapterColorsSection({ book, onRefresh }: { book: BookDetail; onRefresh: () => void }) {
  const { t } = useTranslation('pages');

  const handleColorChange = async (chapterId: string, color: string) => {
    try {
      await updateChapter(chapterId, { color });
      onRefresh();
    } catch {
      /* silent */
    }
  };

  const handleHideFromTOCChange = async (chapterId: string, hide: boolean) => {
    try {
      await updateChapter(chapterId, { hide_from_toc: hide });
      onRefresh();
    } catch {
      /* silent */
    }
  };

  if (!book.chapters.length) {
    return (
      <section>
        <h2 className="text-lg font-semibold text-white mb-4">{t('books.editor.typography.chapterColorsTitle')}</h2>
        <p className="text-slate-500 text-sm">{t('books.editor.typography.noChapters')}</p>
      </section>
    );
  }

  return (
    <section>
      <h2 className="text-lg font-semibold text-white mb-4">{t('books.editor.typography.chapterColorsTitle')}</h2>
      <div className="grid gap-3 md:grid-cols-2">
        {book.chapters.map((ch) => (
          <ChapterColorRow
            key={ch.id}
            chapter={ch}
            onColorChange={(color) => void handleColorChange(ch.id, color)}
            onHideFromTOCChange={(hide) => void handleHideFromTOCChange(ch.id, hide)}
          />
        ))}
      </div>
    </section>
  );
}

// ── Text Styles Section ────────────────────────────────────────

const STYLE_EXAMPLES = [
  { label: 'H1', md: '# Heading 1' },
  { label: 'H2', md: '## Heading 2' },
  { label: 'Body', md: 'Regular paragraph text with **bold** and *italic* words mixed in.' },
  { label: 'Lists', md: '- First item\n- Second item\n\n1. Numbered one\n2. Numbered two' },
  { label: 'Blockquote', md: '> This is a blockquote, used for oral history and citations.' },
  {
    label: 'Table',
    md: '| Column A | Column B |\n|---|---|\n| Cell 1 | Cell 2 |\n| Cell 3 | Cell 4 |',
  },
  { label: 'Center', md: '->centered text<-' },
  { label: 'Right', md: '->right-aligned text->' },
  { label: 'Rule', md: 'Above the line\n\n---\n\nBelow the line' },
  { label: 'Small Caps', md: '^^Small Caps Text^^' },
  { label: 'Line Break', md: 'First line\\nSecond line (no new paragraph)' },
  { label: 'Non-breaking Space', md: 'Jan~Novák (non-breaking~space, \\~ for literal tilde)' },
];

function TextStylesSection() {
  const { t } = useTranslation('pages');
  const typo = BOOK_TYPOGRAPHY;

  return (
    <section>
      <h2 className="text-lg font-semibold text-white mb-4">{t('books.editor.typography.textStylesTitle')}</h2>
      <div className="space-y-4">
        {STYLE_EXAMPLES.map((ex) => (
          <div key={ex.label} className="bg-slate-800/50 rounded-lg border border-slate-700 overflow-hidden">
            <div className="grid md:grid-cols-2 divide-x divide-slate-700">
              {/* Markdown source */}
              <div className="p-4">
                <div className="text-xs text-slate-500 mb-2">{t('books.editor.typography.markdownSource')}</div>
                <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono">{ex.md}</pre>
              </div>
              {/* Rendered preview */}
              <div className="p-4">
                <div className="text-xs text-slate-500 mb-2">{t('books.editor.typography.renderedPreview')}</div>
                <div
                  style={{
                    fontFamily: typo.textSlot.fontFamily,
                    fontSize: typo.textSlot.fontSize,
                    lineHeight: typo.textSlot.lineHeight,
                  }}
                >
                  <MarkdownContent content={ex.md} className="typography-preview" />
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

// ── Page Dimensions Section ────────────────────────────────────

function PageDimensionsSection() {
  const { t } = useTranslation('pages');
  const dim = PAGE_DIMENSIONS;

  const rows = [
    { label: t('books.editor.typography.pageSize'), value: t('books.editor.typography.pageSizeValue') },
    {
      label: t('books.editor.typography.margins'),
      value: t('books.editor.typography.marginsValue', { inside: dim.marginInside, outside: dim.marginOutside }),
    },
    {
      label: t('books.editor.typography.topBottom'),
      // Layout: margin_top(10) + header(4) + canvas(172) + footer(8) + margin_bottom(16) = 210
      value: t('books.editor.typography.topBottomValue', { top: 10, bottom: 16 }),
    },
    {
      label: t('books.editor.typography.contentArea'),
      value: t('books.editor.typography.contentAreaValue', { width: dim.contentWidth, height: dim.canvasHeight }),
    },
    {
      label: t('books.editor.typography.canvasZone'),
      value: t('books.editor.typography.canvasZoneValue', { height: dim.canvasHeight }),
    },
  ];

  return (
    <section>
      <h2 className="text-lg font-semibold text-white mb-4">{t('books.editor.typography.pageDimensionsTitle')}</h2>
      <div className="bg-slate-800/50 rounded-lg border border-slate-700 overflow-hidden">
        <table className="w-full text-sm">
          <tbody>
            {rows.map((row, i) => (
              <tr key={row.label} className={i < rows.length - 1 ? 'border-b border-slate-700' : ''}>
                <td className="px-4 py-3 text-slate-400 w-1/3">{row.label}</td>
                <td className="px-4 py-3 text-slate-200">{row.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

// ── Main Component ─────────────────────────────────────────────

export function TypographyTab({ book, onRefresh }: TypographyTabProps) {
  return (
    <div className="space-y-8 max-w-4xl">
      <TypographySettingsSection book={book} onRefresh={onRefresh} />
      <ChapterColorsSection book={book} onRefresh={onRefresh} />
      <TextStylesSection />
      <PageDimensionsSection />
    </div>
  );
}
