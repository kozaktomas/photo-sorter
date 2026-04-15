import { useTranslation } from 'react-i18next';
import type { TextSuggestion } from '../../api/client';

interface Props {
  suggestions: TextSuggestion[] | undefined;
}

export function CheckSuggestionsList({ suggestions }: Props) {
  const { t } = useTranslation('pages');
  if (!suggestions || suggestions.length === 0) return null;
  return (
    <div className="pt-1 border-t border-indigo-500/20">
      <p className="text-xs font-medium text-slate-400 mb-1.5">{t('books.editor.readabilitySuggestions')}:</p>
      <ul className="space-y-1">
        {suggestions.map((s, i) => (
          <li
            key={i}
            className={`text-xs flex items-start gap-1.5 rounded px-2 py-1 ${
              s.severity === 'major'
                ? 'bg-red-950/40 text-red-200 border border-red-500/30'
                : 'bg-amber-950/30 text-amber-200 border border-amber-500/20'
            }`}
          >
            <span
              className={`font-semibold uppercase text-[10px] tracking-wide mt-0.5 ${
                s.severity === 'major' ? 'text-red-400' : 'text-amber-400'
              }`}
            >
              {s.severity === 'major' ? t('books.editor.suggestionMajor') : t('books.editor.suggestionMinor')}
            </span>
            <span className="flex-1">{s.message}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
