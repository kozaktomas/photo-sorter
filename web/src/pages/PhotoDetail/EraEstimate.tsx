import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Calendar, ChevronDown, ChevronUp, Loader2 } from 'lucide-react';
import { estimateEra } from '../../api/client';
import type { EraEstimateResponse } from '../../types';

interface EraEstimateProps {
  uid: string | undefined;
}

export function EraEstimate({ uid }: EraEstimateProps) {
  const { t } = useTranslation('pages');
  const [data, setData] = useState<EraEstimateResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    if (!uid) return;

    let cancelled = false;
    setLoading(true);
    setData(null);
    setExpanded(false);

    estimateEra(uid)
      .then((result) => {
        if (!cancelled) setData(result);
      })
      .catch(() => {
        if (!cancelled) setData(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [uid]);

  if (loading) {
    return (
      <div className="p-4 border-b border-slate-700 shrink-0">
        <div className="flex items-center gap-2 text-slate-400 text-sm">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span>{t('photoDetail.estimatedEra')}</span>
        </div>
      </div>
    );
  }

  if (!data?.best_match || data.top_matches.length === 0) {
    return null;
  }

  const best = data.best_match;
  const rest = data.top_matches.slice(1);
  const maxSimilarity = data.top_matches[0].similarity;

  return (
    <div className="p-4 border-b border-slate-700 shrink-0">
      <div
        className="flex items-center justify-between cursor-pointer"
        onClick={() => rest.length > 0 && setExpanded(!expanded)}
      >
        <div className="flex items-center gap-2">
          <Calendar className="h-4 w-4 text-slate-400" />
          <span className="text-sm text-slate-400">{t('photoDetail.estimatedEra')}:</span>
          <span className="text-sm font-medium text-white">{best.era_name}</span>
        </div>
        <div className="flex items-center gap-1">
          <span className="text-sm font-medium text-blue-400">{Math.round(best.confidence)}%</span>
          {rest.length > 0 && (
            expanded
              ? <ChevronUp className="h-3.5 w-3.5 text-slate-500" />
              : <ChevronDown className="h-3.5 w-3.5 text-slate-500" />
          )}
        </div>
      </div>
      {expanded && rest.length > 0 && (
        <div className="mt-2 space-y-1">
          {rest.map((match) => (
            <div key={match.era_slug} className="flex items-center gap-2 text-xs">
              <span className="w-16 shrink-0 text-slate-400">
                {match.era_name}
              </span>
              <div className="flex-1 h-1.5 bg-slate-700 rounded-full overflow-hidden">
                <div
                  className="h-full rounded-full bg-slate-500"
                  style={{ width: `${(match.similarity / maxSimilarity) * 100}%` }}
                />
              </div>
              <span className="w-8 text-right shrink-0 text-slate-500">
                {Math.round(match.confidence)}%
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
