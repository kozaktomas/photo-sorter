import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { FolderOpen } from 'lucide-react';
import { getPhotoAlbumMemberships } from '../../api/client';
import type { PhotoAlbumMembership } from '../../types';

interface Props {
  uid: string | undefined;
}

export function AlbumMembership({ uid }: Props) {
  const { t } = useTranslation('pages');
  const navigate = useNavigate();
  const [memberships, setMemberships] = useState<PhotoAlbumMembership[]>([]);

  useEffect(() => {
    if (!uid) return;

    let cancelled = false;
    getPhotoAlbumMemberships(uid)
      .then((result) => {
        if (!cancelled) setMemberships(result);
      })
      .catch(() => {
        if (!cancelled) setMemberships([]);
      });
    return () => { cancelled = true; };
  }, [uid]);

  if (memberships.length === 0) return null;

  return (
    <div className="p-4 border-b border-slate-700 shrink-0">
      <div className="flex items-center gap-2 mb-2">
        <FolderOpen className="h-4 w-4 text-slate-400" />
        <span className="text-sm text-slate-400">{t('photoDetail.inAlbums')}</span>
      </div>
      <div className="space-y-1">
        {memberships.map((m) => (
          <button
            key={m.uid}
            onClick={() => navigate(`/albums/${m.uid}`)}
            className="w-full text-left text-sm px-2 py-1 rounded hover:bg-slate-700/50 transition-colors"
          >
            <span className="text-white">{m.title}</span>
            <span className="text-slate-500"> ({t('common:units.photo', { count: m.photo_count })})</span>
          </button>
        ))}
      </div>
    </div>
  );
}
