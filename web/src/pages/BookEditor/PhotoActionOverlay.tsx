import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Eye, Search, Copy, Check } from 'lucide-react';

interface Props {
  photoUid: string;
}

export function PhotoActionOverlay({ photoUid }: Props) {
  const navigate = useNavigate();
  const [copied, setCopied] = useState(false);

  const handleOpenDetail = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    if (e.metaKey || e.ctrlKey) {
      window.open(`/photos/${photoUid}`, '_blank');
    } else {
      void navigate(`/photos/${photoUid}`);
    }
  };

  const handleFindSimilar = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    if (e.metaKey || e.ctrlKey) {
      window.open(`/similar?photo=${photoUid}`, '_blank');
    } else {
      void navigate(`/similar?photo=${photoUid}`);
    }
  };

  const handleCopyId = (e: React.MouseEvent) => {
    e.stopPropagation();
    e.preventDefault();
    void navigator.clipboard.writeText(photoUid);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/80 to-transparent p-2 opacity-0 group-hover:opacity-100 transition-opacity">
      <div className="flex gap-1 justify-end">
        <button
          onClick={handleOpenDetail}
          onPointerDown={(e) => e.stopPropagation()}
          className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
          title="View details"
        >
          <Eye className="h-3 w-3" />
        </button>
        <button
          onClick={handleFindSimilar}
          onPointerDown={(e) => e.stopPropagation()}
          className="p-1.5 bg-black/60 rounded text-white hover:bg-black/80 transition-colors"
          title="Find similar"
        >
          <Search className="h-3 w-3" />
        </button>
        <button
          onClick={handleCopyId}
          onPointerDown={(e) => e.stopPropagation()}
          className={`p-1.5 rounded text-white transition-colors ${
            copied ? 'bg-green-600' : 'bg-black/60 hover:bg-black/80'
          }`}
          title={copied ? 'Copied!' : 'Copy photo ID'}
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        </button>
      </div>
    </div>
  );
}
