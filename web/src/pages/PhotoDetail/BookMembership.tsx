import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { BookOpen } from 'lucide-react';
import { getPhotoBookMemberships } from '../../api/client';
import type { PhotoBookMembership } from '../../types';

interface Props {
  uid: string | undefined;
  refreshKey?: number;
}

export function BookMembership({ uid, refreshKey }: Props) {
  const { t } = useTranslation('pages');
  const navigate = useNavigate();
  const [memberships, setMemberships] = useState<PhotoBookMembership[]>([]);

  useEffect(() => {
    if (!uid) return;

    let cancelled = false;
    getPhotoBookMemberships(uid)
      .then((result) => {
        if (!cancelled) setMemberships(result);
      })
      .catch(() => {
        if (!cancelled) setMemberships([]);
      });
    return () => { cancelled = true; };
  }, [uid, refreshKey]);

  if (memberships.length === 0) return null;

  return (
    <div className="p-4 border-b border-slate-700 shrink-0">
      <div className="flex items-center gap-2 mb-2">
        <BookOpen className="h-4 w-4 text-slate-400" />
        <span className="text-sm text-slate-400">{t('photoDetail.inBooks')}</span>
      </div>
      <div className="space-y-1">
        {memberships.map((m) => (
          <button
            key={`${m.book_id}-${m.section_id}`}
            onClick={() => navigate(`/books/${m.book_id}`)}
            className="w-full text-left text-sm px-2 py-1 rounded hover:bg-slate-700/50 transition-colors"
          >
            <span className="text-white">{m.book_title}</span>
            <span className="text-slate-500"> / {m.section_title}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
