import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ArrowLeft, Loader2, AlertCircle, Images, ScanFace, Copy, ExternalLink, User, RefreshCw } from 'lucide-react';
import { Button } from '../../components/Button';
import { usePhotoData } from './hooks/usePhotoData';
import { useFacesData } from './hooks/useFacesData';
import { useFaceAssignment } from './hooks/useFaceAssignment';
import { usePhotoNavigation } from './hooks/usePhotoNavigation';
import { EmbeddingsStatus } from './EmbeddingsStatus';
import { PhotoDisplay } from './PhotoDisplay';
import { FacesList } from './FacesList';
import { FaceAssignmentPanel } from './FaceAssignmentPanel';

export function PhotoDetailPage() {
  const { t } = useTranslation(['pages', 'common']);
  const { uid } = useParams<{ uid: string }>();
  const navigate = useNavigate();

  const {
    photo,
    loading: photoLoading,
    error: photoError,
    config,
    embeddingsStatus,
    updateEmbeddingsStatus,
  } = usePhotoData(uid);

  const {
    facesData,
    subjects,
    loading: facesLoading,
    error: facesError,
    facesNotComputed,
    facesLoaded,
    isComputing,
    computeError,
    loadFaces,
    computeFacesForPhoto,
    setFacesData,
  } = useFacesData(uid, updateEmbeddingsStatus);

  const {
    selectedFaceIndex,
    setSelectedFaceIndex,
    applyingFace,
    applyError,
    manualName,
    setManualName,
    showManualInput,
    setShowManualInput,
    filteredSubjects,
    showAutocomplete,
    setShowAutocomplete,
    handleApplySuggestion,
    handleManualAssign,
    handleSelectAutocomplete,
    selectFirstUnassignedFace,
  } = useFaceAssignment(uid, facesData, subjects, setFacesData);

  const {
    hasPrev,
    hasNext,
    goToPrev,
    goToNext,
    currentIndex,
    totalPhotos,
  } = usePhotoNavigation(uid);

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ignore if user is typing in an input field
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement ||
        (e.target as HTMLElement).isContentEditable
      ) {
        return;
      }

      if (e.key === 'ArrowLeft' && hasPrev) {
        e.preventDefault();
        goToPrev();
      } else if (e.key === 'ArrowRight' && hasNext) {
        e.preventDefault();
        goToNext();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hasPrev, hasNext, goToPrev, goToNext]);

  // Auto-select first unassigned face when faces are loaded
  useEffect(() => {
    if (facesData?.faces && facesData.faces.length > 0) {
      selectFirstUnassignedFace(facesData.faces);
    }
  }, [facesData?.faces]);

  const handleFindSimilar = () => {
    navigate(`/similar?photo=${uid}`);
  };

  const handleCopyUid = () => {
    if (uid) navigator.clipboard.writeText(uid);
  };

  const handleOpenInPhotoprism = () => {
    if (config?.photoprism_domain && uid) {
      const url = `${config.photoprism_domain}/library/browse?view=cards&order=oldest&q=uid:${uid}`;
      window.open(url, '_blank');
    }
  };

  const selectedFace = selectedFaceIndex !== null ? facesData?.faces[selectedFaceIndex] : null;

  if (photoLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-8 w-8 animate-spin text-blue-500" />
      </div>
    );
  }

  if (photoError || !photo) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center">
          <AlertCircle className="h-12 w-12 text-red-500 mx-auto mb-4" />
          <p className="text-red-400">{photoError || t('common:errors.photoNotFound')}</p>
          <Button variant="ghost" onClick={() => navigate(-1)} className="mt-4">
            <ArrowLeft className="h-4 w-4 mr-2" />
            {t('common:buttons.goBack')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-slate-700 shrink-0">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
            <ArrowLeft className="h-4 w-4 mr-1" />
            {t('pages:photoDetail.back')}
          </Button>
          <div>
            <h1 className="text-xl font-semibold text-white">{photo.title || photo.file_name}</h1>
            <p className="text-sm text-slate-400">
              {photo.taken_at ? new Date(photo.taken_at).toLocaleDateString() : t('common:time.noDate')}
              {photo.width && photo.height && ` - ${photo.width}x${photo.height}`}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={handleCopyUid} title={t('pages:photoDetail.copyUid')}>
            <Copy className="h-4 w-4 mr-1" />
            {t('pages:photoDetail.copyUid')}
          </Button>
          {config?.photoprism_domain && (
            <Button variant="ghost" size="sm" onClick={handleOpenInPhotoprism} title="Open in PhotoPrism">
              <ExternalLink className="h-4 w-4 mr-1" />
              {t('pages:photoDetail.photoprism')}
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={handleFindSimilar} title={t('pages:photoDetail.similar')}>
            <Images className="h-4 w-4 mr-1" />
            {t('pages:photoDetail.similar')}
          </Button>
          <Button
            variant={facesLoaded ? 'primary' : 'ghost'}
            size="sm"
            onClick={loadFaces}
            disabled={facesLoading}
            title={t('pages:photoDetail.faces')}
          >
            <ScanFace className="h-4 w-4 mr-1" />
            {t('pages:photoDetail.faces')}
          </Button>
        </div>
      </div>

      {/* Embeddings status banner */}
      <EmbeddingsStatus
        status={embeddingsStatus}
        onCompute={computeFacesForPhoto}
        isComputing={isComputing}
      />

      {/* Content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left: Photo with face boxes */}
        <PhotoDisplay
          photo={photo}
          faces={facesData?.faces}
          selectedFaceIndex={selectedFaceIndex}
          onFaceSelect={setSelectedFaceIndex}
          hasPrev={hasPrev}
          hasNext={hasNext}
          onPrev={goToPrev}
          onNext={goToNext}
          currentIndex={currentIndex}
          totalPhotos={totalPhotos}
        />

        {/* Right: Face details and suggestions */}
        <div className="w-80 border-l border-slate-700 flex flex-col overflow-hidden shrink-0">
          {/* Header */}
          <div className="p-4 border-b border-slate-700 shrink-0">
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-medium text-white">{t('pages:photoDetail.faces')}</h3>
              {facesLoaded && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={computeFacesForPhoto}
                  isLoading={isComputing}
                  disabled={isComputing}
                  title={t('common:buttons.rescan')}
                >
                  <RefreshCw className="h-4 w-4" />
                </Button>
              )}
            </div>
            <p className="text-sm text-slate-400">
              {facesLoading ? t('common:status.loading') : facesLoaded ? t('pages:photoDetail.facesDetected', { count: facesData?.faces.length || 0 }) : t('pages:photoDetail.clickToLoadFaces')}
            </p>
            {facesLoaded && facesData && (
              <div className="flex gap-3 mt-1 text-xs text-slate-500">
                <span>{facesData.embeddings_count} {t('pages:photoDetail.embeddings')}</span>
                <span>{facesData.markers_count} {t('pages:photoDetail.markers')}</span>
              </div>
            )}
            {computeError && (
              <p className="text-red-400 text-xs mt-2">{computeError}</p>
            )}
          </div>

          {/* Not loaded yet */}
          {!facesLoading && !facesLoaded && !facesError && (
            <div className="flex-1 flex items-center justify-center text-slate-500">
              <div className="text-center">
                <ScanFace className="h-12 w-12 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t('pages:photoDetail.clickToLoadFaces')}</p>
              </div>
            </div>
          )}

          {facesLoading && (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-blue-500" />
            </div>
          )}

          {facesError && (
            <div className="flex items-center gap-2 text-red-400 p-4">
              <AlertCircle className="h-5 w-5" />
              <span className="text-sm">{facesError}</span>
            </div>
          )}

          {!facesLoading && facesLoaded && !facesError && facesData?.faces.length === 0 && (
            <div className="text-center py-8 px-4 text-slate-400">
              <User className="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p className="mb-4">{t('pages:photoDetail.noFacesDetected')}</p>
              <Button
                variant="ghost"
                size="sm"
                onClick={computeFacesForPhoto}
                isLoading={isComputing}
                disabled={isComputing}
              >
                <ScanFace className="h-4 w-4 mr-2" />
                {t('common:buttons.rescan')}
              </Button>
              {computeError && (
                <p className="text-red-400 text-xs mt-2">{computeError}</p>
              )}
            </div>
          )}

          {!facesLoading && facesLoaded && !facesError && !facesNotComputed && facesData && facesData.faces.length > 0 && (
            <>
              {/* Assignment section - fixed at top */}
              {selectedFace && (
                <FaceAssignmentPanel
                  selectedFace={selectedFace}
                  selectedFaceIndex={selectedFaceIndex!}
                  applyingFace={applyingFace}
                  applyError={applyError}
                  manualName={manualName}
                  setManualName={setManualName}
                  showManualInput={showManualInput}
                  setShowManualInput={setShowManualInput}
                  filteredSubjects={filteredSubjects}
                  showAutocomplete={showAutocomplete}
                  setShowAutocomplete={setShowAutocomplete}
                  onApplySuggestion={handleApplySuggestion}
                  onManualAssign={handleManualAssign}
                  onSelectAutocomplete={handleSelectAutocomplete}
                />
              )}

              {/* Face list - scrollable */}
              <FacesList
                faces={facesData.faces}
                selectedFaceIndex={selectedFaceIndex}
                onFaceSelect={setSelectedFaceIndex}
              />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
