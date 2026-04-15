import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { BookDetail, SectionPhoto } from '../../types';
import { pageFormatSlotCount } from '../../utils/pageFormats';

interface BookStatsPanelProps {
  book: BookDetail;
  sectionPhotos: Record<string, SectionPhoto[]>;
}

const FORMAT_SHORT_KEYS: Record<string, string> = {
  '4_landscape': '4L',
  '2l_1p': '2L+1P',
  '1p_2l': '1P+2L',
  '2_portrait': '2P',
  '1_fullscreen': 'FS',
  '1_fullbleed': 'FB',
};

export function BookStatsPanel({ book, sectionPhotos }: BookStatsPanelProps) {
  const { t } = useTranslation('pages');

  const stats = useMemo(() => {
    const pages = book.pages;
    const sections = book.sections;

    // Total slots and filled slots
    let totalSlots = 0;
    let filledSlots = 0;
    const placedPhotoUids = new Set<string>();
    const formatCounts: Record<string, number> = {};

    for (const page of pages) {
      const slotCount = pageFormatSlotCount(page.format);
      totalSlots += slotCount;
      formatCounts[page.format] = (formatCounts[page.format] || 0) + 1;

      for (const slot of page.slots) {
        if (slot.photo_uid || slot.text_content) {
          filledSlots++;
        }
        if (slot.photo_uid) {
          placedPhotoUids.add(slot.photo_uid);
        }
      }
    }

    // Total section photos
    let totalSectionPhotos = 0;
    for (const section of sections) {
      totalSectionPhotos += section.id in sectionPhotos
        ? sectionPhotos[section.id].length
        : section.photo_count;
    }

    const unassignedPhotos = Math.max(0, totalSectionPhotos - placedPhotoUids.size);
    const fillPercent = totalSlots > 0 ? Math.round((filledSlots / totalSlots) * 100) : 0;
    const emptySections = sections.filter(s => !pages.some(p => p.section_id === s.id)).length;

    // Format distribution string
    const formatDist = Object.entries(formatCounts)
      .sort(([, a], [, b]) => b - a)
      .map(([fmt, count]) => `${FORMAT_SHORT_KEYS[fmt] ?? fmt}: ${count}`)
      .join(', ');

    return {
      pageCount: pages.length,
      photosPlaced: placedPhotoUids.size,
      photosUnassigned: unassignedPhotos,
      filledSlots,
      totalSlots,
      fillPercent,
      sectionCount: sections.length,
      emptySections,
      formatDist: formatDist || '—',
    };
  }, [book.pages, book.sections, sectionPhotos]);

  const fillColor = stats.fillPercent >= 80 ? 'text-green-400' : stats.fillPercent >= 50 ? 'text-amber-400' : 'text-red-400';

  return (
    <div className="bg-slate-800/50 border border-slate-700 rounded-lg p-4 mb-6">
      <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
        <StatCell label={t('books.editor.stats.pages')} value={stats.pageCount} />
        <StatCell label={t('books.editor.stats.photosPlaced')} value={stats.photosPlaced} />
        <StatCell label={t('books.editor.stats.photosUnassigned')} value={stats.photosUnassigned} />
        <StatCell
          label={t('books.editor.stats.slotsFilled')}
          value={`${stats.filledSlots}/${stats.totalSlots} (${stats.fillPercent}%)`}
          valueClassName={fillColor}
        />
        <StatCell label={t('books.editor.stats.formats')} value={stats.formatDist} />
        <StatCell
          label={t('books.editor.stats.sections')}
          value={stats.emptySections > 0
            ? `${stats.sectionCount} (${stats.emptySections} ${t('books.editor.stats.empty')})`
            : String(stats.sectionCount)}
        />
      </div>
    </div>
  );
}

function StatCell({ label, value, valueClassName }: { label: string; value: string | number; valueClassName?: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`text-lg font-semibold ${valueClassName ?? 'text-white'}`}>{value}</div>
    </div>
  );
}
