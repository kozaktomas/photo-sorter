import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertCircle } from 'lucide-react';
import { applyFaceMatch } from '../../api/client';
import { Alert } from '../../components/Alert';
import { PageHeader } from '../../components/PageHeader';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { useSubjectsAndConfig } from '../../hooks/useSubjectsAndConfig';
import { useFaceSearch } from './hooks/useFaceSearch';
import { FacesConfigPanel } from './FacesConfigPanel';
import { FacesResultsSummary } from './FacesResultsSummary';
import { FacesMatchGridCard } from './FacesMatchGrid';
import { PageLoading } from '../../components/LoadingState';
import type { FaceMatch } from '../../types';

export function FacesPage() {
  const { t } = useTranslation('pages');
  const { subjects, config, isLoading, error } = useSubjectsAndConfig();

  const {
    selectedPerson,
    setSelectedPerson,
    threshold,
    setThreshold,
    limit,
    setLimit,
    result,
    isSearching,
    searchError,
    activeFilter,
    setActiveFilter,
    handleSearch,
    actionableCount,
    updateMatchToAlreadyDone,
    removeMatch,
  } = useFaceSearch();

  // Accept all state
  const [isAcceptingAll, setIsAcceptingAll] = useState(false);
  const [acceptAllProgress, setAcceptAllProgress] = useState({ current: 0, total: 0 });
  const [applyError, setApplyError] = useState<string | null>(null);

  const handlePhotoClick = (match: { photo_uid: string }) => {
    if (config?.photoprism_domain) {
      const url = `${config.photoprism_domain}/library/browse?view=cards&order=oldest&q=uid:${match.photo_uid}`;
      window.open(url, '_blank');
    }
  };

  const getPersonName = () => {
    const subject = subjects.find((s) => s.slug === selectedPerson);
    return subject?.name || selectedPerson;
  };

  const handleApprove = async (match: FaceMatch) => {
    const personName = getPersonName();
    setApplyError(null);

    try {
      const response = await applyFaceMatch({
        photo_uid: match.photo_uid,
        person_name: personName,
        action: match.action,
        marker_uid: match.marker_uid,
        file_uid: match.file_uid,
        bbox_rel: match.bbox_rel,
        face_index: match.face_index,
      });

      if (response.success) {
        updateMatchToAlreadyDone(match);
      } else {
        setApplyError(`Failed to apply: ${response.error}`);
      }
    } catch (err) {
      console.error('Failed to apply match:', err);
      setApplyError('Failed to apply match');
    }
  };

  const handleReject = (match: FaceMatch) => {
    removeMatch(match);
  };

  const handleAcceptAll = async () => {
    if (!result) return;

    const personName = getPersonName();

    // Get matches that are visible based on current filter and are actionable
    const actionableMatches = result.matches.filter((m) => {
      if (m.action === 'already_done') return false;
      if (activeFilter !== 'all' && m.action !== activeFilter) return false;
      return true;
    });

    if (actionableMatches.length === 0) return;

    setIsAcceptingAll(true);
    setAcceptAllProgress({ current: 0, total: actionableMatches.length });

    for (let i = 0; i < actionableMatches.length; i++) {
      const match = actionableMatches[i];
      setAcceptAllProgress({ current: i + 1, total: actionableMatches.length });

      try {
        const response = await applyFaceMatch({
          photo_uid: match.photo_uid,
          person_name: personName,
          action: match.action,
          marker_uid: match.marker_uid,
          file_uid: match.file_uid,
          bbox_rel: match.bbox_rel,
          face_index: match.face_index,
        });

        if (response.success) {
          updateMatchToAlreadyDone(match);
        }
      } catch (err) {
        console.error('Failed to apply match:', err);
      }
    }

    setIsAcceptingAll(false);
  };

  if (isLoading) {
    return <PageLoading text="Loading..." />;
  }

  if (error) {
    return (
      <div className="text-center py-12">
        <AlertCircle className="h-12 w-12 text-red-400 mx-auto mb-4" />
        <p className="text-red-400">{error}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={PAGE_CONFIGS.faces.icon}
        title={t('faces.title')}
        subtitle={t('faces.subtitle')}
        color="amber"
        category="faces"
      />

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        <FacesConfigPanel
          subjects={subjects}
          selectedPerson={selectedPerson}
          setSelectedPerson={setSelectedPerson}
          threshold={threshold}
          setThreshold={setThreshold}
          limit={limit}
          setLimit={setLimit}
          isSearching={isSearching}
          searchError={searchError}
          onSearch={handleSearch}
        />

        <FacesResultsSummary result={result} />
      </div>

      {applyError && (
        <Alert variant="error">{applyError}</Alert>
      )}

      {result && result.matches.length > 0 && (
        <FacesMatchGridCard
          matches={result.matches}
          summary={result.summary}
          photoprismDomain={config?.photoprism_domain}
          activeFilter={activeFilter}
          setActiveFilter={setActiveFilter}
          actionableCount={actionableCount}
          isAcceptingAll={isAcceptingAll}
          acceptAllProgress={acceptAllProgress}
          onAcceptAll={handleAcceptAll}
          onApprove={handleApprove}
          onReject={handleReject}
          onPhotoClick={handlePhotoClick}
        />
      )}
    </div>
  );
}
