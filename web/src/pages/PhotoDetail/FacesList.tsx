import { useTranslation } from 'react-i18next';
import { ACTION_PANEL_STYLES } from '../../constants/actions';
import type { PhotoFace, MatchAction } from '../../types';

interface FacesListProps {
  faces: PhotoFace[];
  selectedFaceIndex: number | null;
  onFaceSelect: (index: number) => void;
}

export function FacesList({ faces, selectedFaceIndex, onFaceSelect }: FacesListProps) {
  const { t } = useTranslation('pages');

  const actionLabels: Record<MatchAction, string> = {
    create_marker: t('photoDetail.unassigned'),
    assign_person: t('photoDetail.unassigned'),
    already_done: t('photoDetail.assigned'),
    unassign_person: t('outliers.suspiciousFaces'),
  };

  return (
    <div className="flex-1 overflow-auto p-4">
      <h4 className="text-xs font-medium text-slate-500 uppercase tracking-wide mb-2">{t('photoDetail.facesList')}</h4>
      <div className="space-y-2">
        {faces.map((face, index) => (
          <button
            key={face.face_index}
            onClick={() => onFaceSelect(index)}
            className={`w-full text-left p-2.5 rounded-lg border transition-colors ${
              selectedFaceIndex === index
                ? 'bg-blue-500/10 border-blue-500/50'
                : 'bg-slate-800/50 border-slate-700 hover:border-slate-600'
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium text-white text-sm shrink-0">Face #{index + 1}</span>
              <span className={`text-xs px-1.5 py-0.5 rounded border truncate ${ACTION_PANEL_STYLES[face.action]}`}>
                {face.marker_name || actionLabels[face.action]}
              </span>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
