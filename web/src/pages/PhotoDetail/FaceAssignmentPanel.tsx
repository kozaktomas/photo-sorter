import { useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Loader2, UserPlus } from 'lucide-react';
import { Button } from '../../components/Button';
import { ACTION_DESCRIPTIVE_LABELS, ACTION_PANEL_STYLES } from '../../constants/actions';
import type { PhotoFace, FaceSuggestion, Subject } from '../../types';

interface FaceAssignmentPanelProps {
  selectedFace: PhotoFace;
  selectedFaceIndex: number;
  applyingFace: number | null;
  applyError: string | null;
  manualName: string;
  setManualName: (name: string) => void;
  showManualInput: boolean;
  setShowManualInput: (show: boolean) => void;
  filteredSubjects: Subject[];
  showAutocomplete: boolean;
  setShowAutocomplete: (show: boolean) => void;
  onApplySuggestion: (face: PhotoFace, suggestion: FaceSuggestion) => void;
  onManualAssign: (face: PhotoFace, personName: string) => void;
  onSelectAutocomplete: (subject: Subject) => void;
}

export function FaceAssignmentPanel({
  selectedFace,
  selectedFaceIndex,
  applyingFace,
  applyError,
  manualName,
  setManualName,
  showManualInput,
  setShowManualInput,
  filteredSubjects,
  showAutocomplete,
  setShowAutocomplete,
  onApplySuggestion,
  onManualAssign,
  onSelectAutocomplete,
}: FaceAssignmentPanelProps) {
  const { t } = useTranslation(['pages', 'common']);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (showManualInput) {
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [showManualInput]);

  return (
    <div className="p-4 border-b border-slate-700 bg-slate-800/30 shrink-0">
      <div className="flex items-center justify-between mb-3">
        <h4 className="text-sm font-medium text-white">
          Face #{selectedFaceIndex + 1}
        </h4>
        <span className={`text-xs px-2 py-0.5 rounded border ${ACTION_PANEL_STYLES[selectedFace.action]}`}>
          {selectedFace.marker_name || ACTION_DESCRIPTIVE_LABELS[selectedFace.action]}
        </span>
      </div>

      {selectedFace.action === 'already_done' ? (
        <div className="flex items-center gap-2 text-green-400 text-sm">
          <Check className="h-4 w-4" />
          <span>{t('pages:photoDetail.faceAssigned')}: {selectedFace.marker_name}</span>
        </div>
      ) : (
        <div className="space-y-3">
          {/* Suggestions */}
          {selectedFace.suggestions.length > 0 && (
            <div className="space-y-1.5">
              {selectedFace.suggestions.slice(0, 3).map((suggestion) => (
                <div
                  key={suggestion.person_name}
                  className="flex items-center justify-between p-2 bg-slate-800/50 rounded-lg border border-slate-700"
                >
                  <div className="flex-1 min-w-0">
                    <div className="font-medium text-white text-sm truncate">
                      {suggestion.person_name}
                    </div>
                    <div className="text-xs text-slate-400">
                      {(suggestion.confidence * 100).toFixed(0)}% match
                    </div>
                  </div>
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() => onApplySuggestion(selectedFace, suggestion)}
                    disabled={applyingFace === selectedFace.face_index}
                    className="ml-2"
                  >
                    {applyingFace === selectedFace.face_index ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Check className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              ))}
            </div>
          )}

          {/* Manual name input */}
          {!showManualInput ? (
            <button
              onClick={() => setShowManualInput(true)}
              className="w-full flex items-center justify-center gap-2 p-2 text-sm text-slate-400 hover:text-white border border-dashed border-slate-600 hover:border-slate-500 rounded-lg transition-colors"
            >
              <UserPlus className="h-4 w-4" />
              {t('pages:photoDetail.manualAssignment')}
            </button>
          ) : (
            <div className="space-y-2">
              <div className="relative">
                <input
                  ref={inputRef}
                  type="text"
                  value={manualName}
                  onChange={(e) => setManualName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && manualName.trim()) {
                      onManualAssign(selectedFace, manualName);
                    } else if (e.key === 'Escape') {
                      setShowManualInput(false);
                      setManualName('');
                      setShowAutocomplete(false);
                    }
                  }}
                  onBlur={() => {
                    setTimeout(() => setShowAutocomplete(false), 150);
                  }}
                  onFocus={() => {
                    if (filteredSubjects.length > 0) setShowAutocomplete(true);
                  }}
                  placeholder={t('pages:photoDetail.manualNamePlaceholder')}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-lg text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-blue-500 text-sm"
                />

                {showAutocomplete && filteredSubjects.length > 0 && (
                  <div className="absolute z-10 w-full mt-1 bg-slate-800 border border-slate-600 rounded-lg shadow-lg overflow-hidden">
                    {filteredSubjects.map((subject) => (
                      <button
                        key={subject.uid}
                        onClick={() => onSelectAutocomplete(subject)}
                        className="w-full text-left px-3 py-2 hover:bg-slate-700 transition-colors"
                      >
                        <div className="text-sm text-white">{subject.name}</div>
                        <div className="text-xs text-slate-400">{subject.photo_count} {t('pages:labels.photos').toLowerCase()}</div>
                      </button>
                    ))}
                  </div>
                )}
              </div>

              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => onManualAssign(selectedFace, manualName)}
                  disabled={!manualName.trim() || applyingFace === selectedFace.face_index}
                  className="flex-1"
                >
                  {applyingFace === selectedFace.face_index ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <>
                      <Check className="h-4 w-4 mr-1" />
                      {t('pages:faces.assign')}
                    </>
                  )}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setShowManualInput(false);
                    setManualName('');
                    setShowAutocomplete(false);
                  }}
                >
                  {t('common:buttons.cancel')}
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {applyError && (
        <p className="text-red-400 text-xs mt-2">{applyError}</p>
      )}
    </div>
  );
}
