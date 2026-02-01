import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Search, ExternalLink } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { FormInput } from '../../components/FormInput';
import { FormSelect } from '../../components/FormSelect';
import type { Subject } from '../../types';

interface FacesConfigPanelProps {
  subjects: Subject[];
  selectedPerson: string;
  setSelectedPerson: (value: string) => void;
  threshold: number;
  setThreshold: (value: number) => void;
  limit: number;
  setLimit: (value: number) => void;
  isSearching: boolean;
  searchError: string | null;
  onSearch: () => void;
}

export function FacesConfigPanel({
  subjects,
  selectedPerson,
  setSelectedPerson,
  threshold,
  setThreshold,
  limit,
  setLimit,
  isSearching,
  searchError,
  onSearch,
}: FacesConfigPanelProps) {
  const { t } = useTranslation(['pages', 'common']);

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold text-white">{t('pages:faces.configuration')}</h2>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Person selection */}
        <div>
          <label className="block text-sm font-medium text-slate-300 mb-2">
            {t('pages:faces.person')}
          </label>
          <div className="flex items-center space-x-2">
            <FormSelect
              value={selectedPerson}
              onChange={(e) => setSelectedPerson(e.target.value)}
              disabled={isSearching}
              className="flex-1"
            >
              <option value="">{t('pages:faces.selectPerson')}</option>
              {subjects.map((subject) => (
                <option key={subject.uid} value={subject.slug}>
                  {subject.name} ({subject.photo_count} {t('pages:labels.photos').toLowerCase()})
                </option>
              ))}
            </FormSelect>
            {selectedPerson && (
              <Link
                to={`/subjects/${subjects.find(s => s.slug === selectedPerson)?.uid}`}
                className="text-slate-400 hover:text-blue-400 p-2"
                title="View subject details"
              >
                <ExternalLink className="h-4 w-4" />
              </Link>
            )}
          </div>
        </div>

        {/* Threshold slider */}
        <div>
          <label className="block text-sm font-medium text-slate-300 mb-2">
            {t('pages:faces.minMatch')}: {threshold} %
          </label>
          <input
            type="range"
            min="20"
            max="80"
            step="5"
            value={threshold}
            onChange={(e) => setThreshold(parseInt(e.target.value))}
            disabled={isSearching}
            className="w-full h-2 bg-slate-700 rounded-lg appearance-none cursor-pointer"
          />
          <div className="flex justify-between text-xs text-slate-500 mt-1">
            <span>{t('pages:faces.moreResults')}</span>
            <span>{t('pages:faces.betterMatches')}</span>
          </div>
        </div>

        {/* Limit */}
        <FormInput
          label={t('pages:faces.limit')}
          type="number"
          value={limit}
          onChange={(e) => setLimit(parseInt(e.target.value) || 0)}
          disabled={isSearching}
          min={0}
        />

        {/* Search button */}
        <Button
          onClick={onSearch}
          disabled={!selectedPerson}
          isLoading={isSearching}
          className="w-full"
        >
          <Search className="h-4 w-4 mr-2" />
          {t('common:buttons.search')}
        </Button>

        {/* Error */}
        {searchError && (
          <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
            {searchError}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
