import { useMemo, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { BarChart, Bar, XAxis, YAxis, Tooltip, Brush, ResponsiveContainer } from 'recharts';
import type { HistogramBucket } from '../../types';

interface BrowseTimelineProps {
  buckets: HistogramBucket[];
  bucket: 'month' | 'year';
  // The active date range, expressed as ISO date strings. Either may be empty
  // when the user has not constrained that end of the range yet.
  takenFrom?: string;
  takenTo?: string;
  onRangeChange: (from: string | undefined, to: string | undefined) => void;
}

// chartRow is the recharts-friendly row shape: x is the bucket start as an
// ISO date, y is the count. label is the human-readable label drawn on the
// x-axis tick. uses ms-since-epoch as the numeric primary key so brushing
// integrates naturally with recharts' default x-scale.
interface ChartRow {
  start: number;
  end: number;
  count: number;
  label: string;
}

// formatLabel returns the short axis label for a bucket. Year buckets show
// just the year; month buckets show YYYY-MM so the user can tell adjacent
// months apart without zooming.
function formatLabel(d: Date, bucket: 'month' | 'year'): string {
  const y = d.getUTCFullYear();
  if (bucket === 'year') return String(y);
  const m = String(d.getUTCMonth() + 1).padStart(2, '0');
  return `${y}-${m}`;
}

export function BrowseTimeline({
  buckets,
  bucket,
  takenFrom,
  takenTo,
  onRangeChange,
}: BrowseTimelineProps) {
  const { t } = useTranslation('pages');

  const data = useMemo<ChartRow[]>(
    () =>
      buckets.map(b => {
        const start = new Date(b.start);
        return {
          start: start.getTime(),
          end: new Date(b.end).getTime(),
          count: b.count,
          label: formatLabel(start, bucket),
        };
      }),
    [buckets, bucket],
  );

  // Recharts' Brush component reports indices into the data array. We
  // translate those back into ISO date strings before calling the parent.
  // We also debounce so a fast drag doesn't fire a hundred URL updates.
  const debounceRef = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    };
  }, []);

  const handleBrushChange = (range: { startIndex?: number; endIndex?: number }) => {
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => {
      const startIdx = range.startIndex ?? 0;
      const endIdx = range.endIndex ?? data.length - 1;
      if (data.length === 0) {
        onRangeChange(undefined, undefined);
        return;
      }
      const fromIso = new Date(data[startIdx].start).toISOString();
      // Use the bucket's *end* as taken_to (exclusive boundary) so the
      // bucket the user dragged to is included in the result set.
      const toIso = new Date(data[endIdx].end).toISOString();
      // When the brush spans the entire range, clear the filter so the
      // back button reading the URL doesn't see a redundant constraint.
      const isFullRange = startIdx === 0 && endIdx === data.length - 1;
      onRangeChange(isFullRange ? undefined : fromIso, isFullRange ? undefined : toIso);
    }, 200);
  };

  // Compute the brush's initial indices from the URL-driven date range so
  // bookmarked links restore the previously-selected window.
  const { brushStart, brushEnd } = useMemo(() => {
    if (data.length === 0) return { brushStart: 0, brushEnd: 0 };
    let startIdx = 0;
    let endIdx = data.length - 1;
    if (takenFrom) {
      const fromMs = new Date(takenFrom).getTime();
      const found = data.findIndex(r => r.start >= fromMs);
      if (found >= 0) startIdx = found;
    }
    if (takenTo) {
      const toMs = new Date(takenTo).getTime();
      let last = -1;
      for (let i = 0; i < data.length; i++) {
        if (data[i].end <= toMs) last = i;
      }
      if (last >= 0) endIdx = last;
    }
    if (endIdx < startIdx) endIdx = startIdx;
    return { brushStart: startIdx, brushEnd: endIdx };
  }, [data, takenFrom, takenTo]);

  if (data.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-slate-500 text-sm">
        {t('browse.loading')}
      </div>
    );
  }

  return (
    <div className="h-full w-full">
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} margin={{ top: 10, right: 16, bottom: 4, left: 0 }}>
          <XAxis
            dataKey="label"
            tick={{ fill: '#94a3b8', fontSize: 11 }}
            tickLine={{ stroke: '#475569' }}
            axisLine={{ stroke: '#475569' }}
            minTickGap={24}
          />
          <YAxis
            tick={{ fill: '#94a3b8', fontSize: 11 }}
            tickLine={{ stroke: '#475569' }}
            axisLine={{ stroke: '#475569' }}
            width={40}
            allowDecimals={false}
          />
          <Tooltip
            cursor={{ fill: 'rgba(20, 184, 166, 0.08)' }}
            contentStyle={{
              background: '#0f172a',
              border: '1px solid #334155',
              borderRadius: 6,
              fontSize: 12,
              color: '#e2e8f0',
            }}
            labelStyle={{ color: '#f1f5f9', fontWeight: 600 }}
          />
          <Bar dataKey="count" fill="#14b8a6" radius={[2, 2, 0, 0]} />
          <Brush
            dataKey="label"
            height={28}
            stroke="#14b8a6"
            fill="rgba(20, 184, 166, 0.08)"
            travellerWidth={10}
            startIndex={brushStart}
            endIndex={brushEnd}
            onChange={handleBrushChange}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
