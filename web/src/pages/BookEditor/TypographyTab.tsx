import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { updateChapter } from '../../api/client';
import { BOOK_TYPOGRAPHY, PAGE_DIMENSIONS } from '../../constants/bookTypography';
import type { BookDetail } from '../../types';
import { MarkdownContent } from '../../utils/markdown';

interface TypographyTabProps {
  book: BookDetail;
  onRefresh: () => void;
}

// ── Fonts Section ──────────────────────────────────────────────

function FontsSection() {
  const { t } = useTranslation('pages');
  const typo = BOOK_TYPOGRAPHY;

  return (
    <section>
      <h2 className="text-lg font-semibold text-white mb-4">{t('books.editor.typography.fontsTitle')}</h2>
      <div className="grid gap-6 md:grid-cols-2">
        {/* Body text font */}
        <div className="bg-slate-800/50 rounded-lg p-5 border border-slate-700">
          <h3 className="text-sm font-medium text-slate-400 mb-1">{t('books.editor.typography.bodyTextFont')}</h3>
          <div className="text-white font-semibold mb-3" style={{ fontFamily: typo.textSlot.fontFamily }}>
            EB Garamond
          </div>
          <div className="grid grid-cols-3 gap-2 text-xs text-slate-400 mb-4">
            <div>
              <span className="text-slate-500">{t('books.editor.typography.fontSize')}:</span>{' '}
              <span className="text-slate-300">{typo.textSlot.fontSize}</span>
            </div>
            <div>
              <span className="text-slate-500">{t('books.editor.typography.lineHeight')}:</span>{' '}
              <span className="text-slate-300">{typo.textSlot.lineHeight}</span>
            </div>
            <div>
              <span className="text-slate-500">{t('books.editor.typography.fontWeight')}:</span>{' '}
              <span className="text-slate-300">400, 700</span>
            </div>
          </div>
          <p
            className="text-slate-200 leading-relaxed"
            style={{
              fontFamily: typo.textSlot.fontFamily,
              fontSize: typo.textSlot.fontSize,
              lineHeight: typo.textSlot.lineHeight,
            }}
          >
            {t('books.editor.typography.sampleText')}
          </p>
        </div>

        {/* Heading font */}
        <div className="bg-slate-800/50 rounded-lg p-5 border border-slate-700">
          <h3 className="text-sm font-medium text-slate-400 mb-1">{t('books.editor.typography.headingFont')}</h3>
          <div className="text-white font-semibold mb-3" style={{ fontFamily: typo.headingFontFamily }}>
            Source Sans 3
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs text-slate-400 mb-4">
            <div>
              <span className="text-slate-500">H1:</span>{' '}
              <span className="text-slate-300">{typo.h1.fontSize} / {t('books.editor.typography.fontWeight')} {typo.h1.fontWeight}</span>
            </div>
            <div>
              <span className="text-slate-500">H2:</span>{' '}
              <span className="text-slate-300">{typo.h2.fontSize} / {t('books.editor.typography.fontWeight')} {typo.h2.fontWeight}</span>
            </div>
          </div>
          <div className="space-y-3">
            <div
              style={{
                fontFamily: typo.headingFontFamily,
                fontSize: typo.h1.fontSize,
                fontWeight: typo.h1.fontWeight,
              }}
              className="text-white"
            >
              Heading 1
            </div>
            <div
              style={{
                fontFamily: typo.headingFontFamily,
                fontSize: typo.h2.fontSize,
                fontWeight: typo.h2.fontWeight,
              }}
              className="text-white"
            >
              Heading 2
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

// ── Chapter Colors Section ─────────────────────────────────────

function ChapterColorRow({
  chapter,
  onColorChange,
}: {
  chapter: { id: string; title: string; color: string };
  onColorChange: (color: string) => void;
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
      <FontsSection />
      <ChapterColorsSection book={book} onRefresh={onRefresh} />
      <TextStylesSection />
      <PageDimensionsSection />
    </div>
  );
}
