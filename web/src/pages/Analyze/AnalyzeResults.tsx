import { useTranslation } from 'react-i18next';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { getThumbnailUrl } from '../../api/client';
import type { SortSuggestion } from '../../types';

interface AnalyzeResultsProps {
  suggestions: SortSuggestion[];
}

export function AnalyzeResults({ suggestions }: AnalyzeResultsProps) {
  const { t } = useTranslation('pages');

  if (suggestions.length === 0) return null;

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">
          {t('analyze.analyzedPhotos')} ({suggestions.length})
        </h2>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {suggestions.map((suggestion: SortSuggestion) => (
            <div
              key={suggestion.PhotoUID}
              className="flex gap-4 p-3 bg-slate-800 rounded-lg"
            >
              {/* Thumbnail */}
              <div className="flex-shrink-0">
                <img
                  src={getThumbnailUrl(suggestion.PhotoUID, 'tile_224')}
                  alt=""
                  className="w-24 h-24 object-cover rounded"
                />
              </div>

              {/* Info */}
              <div className="flex-1 min-w-0 space-y-2">
                {/* Date */}
                {suggestion.EstimatedDate && (
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-medium text-blue-400 bg-blue-400/10 px-2 py-0.5 rounded">
                      {suggestion.EstimatedDate}
                    </span>
                  </div>
                )}

                {/* Description */}
                <p className="text-sm text-slate-300 line-clamp-3">
                  {suggestion.Description || t('analyze.noDescription')}
                </p>

                {/* Labels */}
                {suggestion.Labels.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {suggestion.Labels.map((label, idx) => (
                      <span
                        key={label.name || idx}
                        className="text-xs text-slate-400 bg-slate-700 px-2 py-0.5 rounded"
                        title={`Confidence: ${(label.confidence * 100).toFixed(0)}%`}
                      >
                        {label.name}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
