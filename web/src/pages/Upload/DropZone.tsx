import { useState, useRef, useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Upload, X } from 'lucide-react';

const ACCEPTED_TYPES = new Set([
  'image/jpeg', 'image/png', 'image/gif', 'image/heic', 'image/heif',
  'image/webp', 'image/tiff', 'image/bmp', 'image/x-canon-cr2',
  'image/x-nikon-nef', 'image/x-sony-arw', 'image/x-adobe-dng',
]);

const ACCEPTED_EXTENSIONS = new Set([
  '.jpg', '.jpeg', '.png', '.gif', '.heic', '.heif', '.webp',
  '.tiff', '.tif', '.bmp', '.raw', '.cr2', '.nef', '.arw', '.dng',
]);

function isAcceptedFile(file: File): boolean {
  if (ACCEPTED_TYPES.has(file.type)) return true;
  const ext = '.' + file.name.split('.').pop()?.toLowerCase();
  return ACCEPTED_EXTENSIONS.has(ext);
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

interface DropZoneProps {
  files: File[];
  onFilesChange: (files: File[]) => void;
  disabled?: boolean;
}

export function DropZone({ files, onFilesChange, disabled }: DropZoneProps) {
  const { t } = useTranslation('pages');
  const [isDragOver, setIsDragOver] = useState(false);
  const dragCounter = useRef(0);
  const dropZoneRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Use native event listeners for reliable cross-browser drag-and-drop.
  // React synthetic drag events + Firefox have known interop issues.
  useEffect(() => {
    const zone = dropZoneRef.current;
    if (!zone) return;

    const onDragEnter = (e: DragEvent) => {
      e.preventDefault();
      dragCounter.current++;
      if (!disabled) setIsDragOver(true);
    };

    const onDragOver = (e: DragEvent) => {
      e.preventDefault();
      if (e.dataTransfer) {
        e.dataTransfer.dropEffect = 'copy';
      }
    };

    const onDragLeave = (e: DragEvent) => {
      e.preventDefault();
      dragCounter.current--;
      if (dragCounter.current === 0) {
        setIsDragOver(false);
      }
    };

    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      dragCounter.current = 0;
      setIsDragOver(false);
      if (!disabled && e.dataTransfer && e.dataTransfer.files.length > 0) {
        addFilesRef.current(e.dataTransfer.files);
      }
    };

    zone.addEventListener('dragenter', onDragEnter);
    zone.addEventListener('dragover', onDragOver);
    zone.addEventListener('dragleave', onDragLeave);
    zone.addEventListener('drop', onDrop);

    return () => {
      zone.removeEventListener('dragenter', onDragEnter);
      zone.removeEventListener('dragover', onDragOver);
      zone.removeEventListener('dragleave', onDragLeave);
      zone.removeEventListener('drop', onDrop);
    };
  }, [disabled]);

  // Prevent browser from opening dropped files outside the drop zone
  useEffect(() => {
    const prevent = (e: DragEvent) => { e.preventDefault(); };
    document.addEventListener('dragover', prevent);
    document.addEventListener('drop', prevent);
    return () => {
      document.removeEventListener('dragover', prevent);
      document.removeEventListener('drop', prevent);
    };
  }, []);

  const addFiles = useCallback((newFiles: FileList | File[]) => {
    const accepted = Array.from(newFiles).filter(isAcceptedFile);
    if (accepted.length === 0) return;
    // Deduplicate by name+size
    const existing = new Set(files.map(f => `${f.name}:${f.size}`));
    const unique = accepted.filter(f => !existing.has(`${f.name}:${f.size}`));
    if (unique.length > 0) {
      onFilesChange([...files, ...unique]);
    }
  }, [files, onFilesChange]);

  // Stable ref so native event listeners always call the latest addFiles
  const addFilesRef = useRef(addFiles);
  addFilesRef.current = addFiles;

  const handleClick = () => {
    if (!disabled) inputRef.current?.click();
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      addFiles(e.target.files);
      e.target.value = '';
    }
  };

  const removeFile = (index: number) => {
    onFilesChange(files.filter((_, i) => i !== index));
  };

  const totalSize = files.reduce((sum, f) => sum + f.size, 0);

  return (
    <div className="space-y-3">
      {/* Drop area */}
      <div
        ref={dropZoneRef}
        onClick={handleClick}
        className={`border-2 border-dashed rounded-lg p-8 text-center cursor-pointer transition-colors ${
          disabled
            ? 'border-slate-700 bg-slate-800/50 cursor-not-allowed opacity-50'
            : isDragOver
              ? 'border-emerald-400 bg-emerald-500/10'
              : 'border-slate-600 hover:border-slate-500 hover:bg-slate-800/50'
        }`}
      >
        <Upload className={`h-10 w-10 mx-auto mb-3 ${isDragOver ? 'text-emerald-400' : 'text-slate-500'}`} />
        <p className={`text-sm ${isDragOver ? 'text-emerald-400' : 'text-slate-400'}`}>
          {isDragOver ? t('upload.dropZoneActive') : t('upload.dropZone')}
        </p>
        <p className="text-xs text-slate-500 mt-1">{t('upload.supportedFormats')}</p>
      </div>

      <input
        ref={inputRef}
        type="file"
        multiple
        accept="image/*,.heic,.heif,.raw,.cr2,.nef,.arw,.dng"
        onChange={handleInputChange}
        className="hidden"
      />

      {/* File list */}
      {files.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm text-slate-400">
              {t('upload.selectedFiles', { count: files.length, size: formatSize(totalSize) })}
            </span>
            <button
              onClick={() => onFilesChange([])}
              disabled={disabled}
              className="text-xs text-slate-500 hover:text-slate-300 disabled:opacity-50"
            >
              {t('upload.clearFiles')}
            </button>
          </div>
          <div className="max-h-40 overflow-y-auto space-y-1">
            {files.map((file, index) => (
              <div key={`${file.name}-${file.size}`} className="flex items-center justify-between text-sm bg-slate-800 rounded px-3 py-1.5">
                <span className="text-slate-300 truncate mr-2">{file.name}</span>
                <div className="flex items-center space-x-2 shrink-0">
                  <span className="text-slate-500 text-xs">{formatSize(file.size)}</span>
                  {!disabled && (
                    <button onClick={() => removeFile(index)} className="text-slate-500 hover:text-red-400">
                      <X className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
