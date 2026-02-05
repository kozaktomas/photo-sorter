import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AlertCircle, ShieldCheck } from 'lucide-react';
import { applyFaceMatch } from '../../api/client';
import { PageHeader } from '../../components/PageHeader';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { useSubjectsAndConfig } from '../../hooks/useSubjectsAndConfig';
import { useScanAll } from './hooks/useScanAll';
import { ScanConfigPanel } from './ScanConfigPanel';
import { ScanResultsSummary } from './ScanResultsSummary';
import { PersonResultCard } from './PersonResultCard';
import { PageLoading } from '../../components/LoadingState';
import { Card, CardContent } from '../../components/Card';
import type { FaceMatch } from '../../types';

export function RecognitionPage() {
  const { t } = useTranslation(['pages', 'common']);
  const { subjects, config, isLoading, error } = useSubjectsAndConfig();

  const {
    confidence,
    setConfidence,
    isScanning,
    scanProgress,
    results,
    scanError,
    startScan,
    cancelScan,
    updatePersonResult,
    totalActionable,
    totalAlreadyDone,
  } = useScanAll();

  // Accept all state (per person)
  const [acceptingPerson, setAcceptingPerson] = useState<string | null>(null);
  const [acceptProgress, setAcceptProgress] = useState({ current: 0, total: 0 });

  const handleScan = () => {
    startScan(subjects);
  };

  const handleApprove = (personSlug: string, personName: string) => async (match: FaceMatch) => {
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
        updatePersonResult(personSlug, (prev) => ({
          ...prev,
          actionable: prev.actionable.filter(
            (m) => !(m.photo_uid === match.photo_uid && m.face_index === match.face_index)
          ),
          alreadyDone: prev.alreadyDone + 1,
        }));
      } else {
        alert(`Failed to apply: ${response.error}`);
      }
    } catch (err) {
      console.error('Failed to apply match:', err);
      alert('Failed to apply match');
    }
  };

  const handleReject = (personSlug: string) => (match: FaceMatch) => {
    updatePersonResult(personSlug, (prev) => ({
      ...prev,
      actionable: prev.actionable.filter(
        (m) => !(m.photo_uid === match.photo_uid && m.face_index === match.face_index)
      ),
    }));
  };

  const handleAcceptAllForPerson = (personSlug: string, personName: string) => async () => {
    const personResult = results.find((r) => r.slug === personSlug);
    if (!personResult) return;

    const toAccept = [...personResult.actionable];
    if (toAccept.length === 0) return;

    setAcceptingPerson(personSlug);
    setAcceptProgress({ current: 0, total: toAccept.length });

    for (let i = 0; i < toAccept.length; i++) {
      const match = toAccept[i];
      setAcceptProgress({ current: i + 1, total: toAccept.length });

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
          updatePersonResult(personSlug, (prev) => ({
            ...prev,
            actionable: prev.actionable.filter(
              (m) => !(m.photo_uid === match.photo_uid && m.face_index === match.face_index)
            ),
            alreadyDone: prev.alreadyDone + 1,
          }));
        }
      } catch (err) {
        console.error('Failed to apply match:', err);
      }
    }

    setAcceptingPerson(null);
  };

  if (isLoading) {
    return <PageLoading text={t('common:status.loading')} />;
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
        icon={PAGE_CONFIGS.recognition.icon}
        title={t('pages:recognition.title')}
        subtitle={t('pages:recognition.subtitle')}
        color="yellow"
        category="faces"
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <ScanConfigPanel
          subjects={subjects}
          confidence={confidence}
          setConfidence={setConfidence}
          isScanning={isScanning}
          scanProgress={scanProgress}
          scanError={scanError}
          onScan={handleScan}
          onCancel={cancelScan}
        />

        <ScanResultsSummary
          resultsCount={results.length}
          totalActionable={totalActionable}
          totalAlreadyDone={totalAlreadyDone}
          isScanning={isScanning}
        />
      </div>

      {/* Per-person results */}
      {results.map((personResult) => (
        <PersonResultCard
          key={personResult.slug}
          name={personResult.name}
          actionable={personResult.actionable}
          photoprismDomain={config?.photoprism_domain}
          isAccepting={acceptingPerson === personResult.slug}
          acceptProgress={acceptProgress}
          onAcceptAll={handleAcceptAllForPerson(personResult.slug, personResult.name)}
          onApprove={handleApprove(personResult.slug, personResult.name)}
          onReject={handleReject(personResult.slug)}
          disabled={acceptingPerson !== null}
        />
      ))}

      {/* Empty state after scan completes */}
      {!isScanning && scanProgress.total > 0 && results.length === 0 && (
        <Card>
          <CardContent>
            <div className="text-center py-12 text-slate-400">
              <ShieldCheck className="h-12 w-12 mx-auto mb-4 text-green-400" />
              <p className="text-lg text-white mb-1">{t('pages:recognition.allMatchesAssigned')}</p>
              <p className="text-sm">{t('pages:recognition.noActionableMatches', { confidence })}</p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
