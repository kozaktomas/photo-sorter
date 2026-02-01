import { PhotoCard } from './PhotoCard';
import type { FaceMatch, MatchAction } from '../types';

interface FaceMatchGridProps {
  matches: FaceMatch[];
  filter?: MatchAction | 'all';
  photoprismDomain?: string;
  onApprove?: (match: FaceMatch) => Promise<void>;
  onReject?: (match: FaceMatch) => void;
  onPhotoClick?: (match: FaceMatch) => void;
}

export function FaceMatchGrid({
  matches,
  filter = 'all',
  photoprismDomain,
  onApprove,
  onReject,
}: FaceMatchGridProps) {
  const filteredMatches =
    filter === 'all'
      ? matches
      : matches.filter((m) => m.action === filter);

  if (filteredMatches.length === 0) {
    return (
      <div className="text-center py-12 text-slate-400">
        No matches found
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
      {filteredMatches.map((match) => (
        <PhotoCard
          key={`${match.photo_uid}-${match.face_index}`}
          photoUid={match.photo_uid}
          photoprismDomain={photoprismDomain}
          matchPercent={Math.round((1 - match.distance) * 100)}
          thumbnailSize="fit_720"
          aspectRatio="auto"
          bboxRel={match.bbox_rel}
          action={match.action}
          onApprove={onApprove ? () => onApprove(match) : undefined}
          onReject={onReject ? () => onReject(match) : undefined}
        />
      ))}
    </div>
  );
}
