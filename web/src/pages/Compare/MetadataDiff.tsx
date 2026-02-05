import { useTranslation } from 'react-i18next';
import type { Photo } from '../../types';

interface MetadataDiffProps {
  left: Photo;
  right: Photo;
}

interface DiffRow {
  label: string;
  leftValue: string;
  rightValue: string;
  leftBetter?: boolean;
  rightBetter?: boolean;
}

function formatMegapixels(w: number, h: number): string {
  if (!w || !h) return '-';
  return (w * h / 1_000_000).toFixed(1) + ' MP';
}

function formatDimensions(w: number, h: number): string {
  if (!w || !h) return '-';
  return `${w} x ${h}`;
}

export function MetadataDiff({ left, right }: MetadataDiffProps) {
  const { t } = useTranslation(['pages']);

  const leftMp = left.width * left.height;
  const rightMp = right.width * right.height;

  const rows: DiffRow[] = [
    {
      label: t('pages:duplicates.compare.dimensions'),
      leftValue: formatDimensions(left.width, left.height),
      rightValue: formatDimensions(right.width, right.height),
      leftBetter: leftMp > rightMp && rightMp > 0,
      rightBetter: rightMp > leftMp && leftMp > 0,
    },
    {
      label: t('pages:duplicates.compare.megapixels'),
      leftValue: formatMegapixels(left.width, left.height),
      rightValue: formatMegapixels(right.width, right.height),
      leftBetter: leftMp > rightMp && rightMp > 0,
      rightBetter: rightMp > leftMp && leftMp > 0,
    },
    {
      label: t('pages:duplicates.compare.date'),
      leftValue: left.taken_at || '-',
      rightValue: right.taken_at || '-',
    },
    {
      label: t('pages:duplicates.compare.camera'),
      leftValue: left.camera_model || '-',
      rightValue: right.camera_model || '-',
    },
    {
      label: t('pages:duplicates.compare.fileName'),
      leftValue: left.file_name || '-',
      rightValue: right.file_name || '-',
    },
    {
      label: t('pages:duplicates.compare.originalName'),
      leftValue: left.original_name || '-',
      rightValue: right.original_name || '-',
    },
    {
      label: t('pages:duplicates.compare.type'),
      leftValue: left.type || '-',
      rightValue: right.type || '-',
    },
    {
      label: t('pages:duplicates.compare.country'),
      leftValue: left.country && left.country !== 'zz' ? left.country : '-',
      rightValue: right.country && right.country !== 'zz' ? right.country : '-',
    },
    {
      label: t('pages:duplicates.compare.favorite'),
      leftValue: left.favorite ? '★' : '-',
      rightValue: right.favorite ? '★' : '-',
    },
  ];

  return (
    <div className="overflow-hidden rounded-lg border border-slate-700">
      <table className="w-full text-sm">
        <thead>
          <tr className="bg-slate-800">
            <th className="px-4 py-2 text-left text-slate-400 font-medium w-1/4">
              {t('pages:duplicates.compare.field')}
            </th>
            <th className="px-4 py-2 text-left text-slate-300 font-medium w-[37.5%]">
              {t('pages:duplicates.compare.photoLeft')}
            </th>
            <th className="px-4 py-2 text-left text-slate-300 font-medium w-[37.5%]">
              {t('pages:duplicates.compare.photoRight')}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const isDifferent = row.leftValue !== row.rightValue;
            return (
              <tr
                key={row.label}
                className={isDifferent ? 'bg-amber-500/5' : 'bg-slate-900/50'}
              >
                <td className="px-4 py-1.5 text-slate-400 font-medium">
                  {row.label}
                </td>
                <td
                  className={`px-4 py-1.5 font-mono text-xs ${
                    row.leftBetter
                      ? 'text-green-400'
                      : isDifferent
                      ? 'text-amber-300'
                      : 'text-slate-300'
                  }`}
                >
                  {row.leftValue}
                </td>
                <td
                  className={`px-4 py-1.5 font-mono text-xs ${
                    row.rightBetter
                      ? 'text-green-400'
                      : isDifferent
                      ? 'text-amber-300'
                      : 'text-slate-300'
                  }`}
                >
                  {row.rightValue}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
