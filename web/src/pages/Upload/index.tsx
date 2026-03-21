import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Upload, Play, Square, CheckCircle, XCircle, AlertCircle, RotateCcw } from 'lucide-react';
import { Card, CardContent, CardHeader } from '../../components/Card';
import { Button } from '../../components/Button';
import { Alert } from '../../components/Alert';
import { PageHeader } from '../../components/PageHeader';
import { FormCheckbox } from '../../components/FormCheckbox';
import { Combobox } from '../../components/Combobox';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { DropZone } from './DropZone';
import { useUploadJob } from './hooks/useUploadJob';
import { getAlbums, getLabels, getBooks, getBook, getThumbnailUrl } from '../../api/client';
import type { Album, Label, PhotoBook, BookDetail, BookSection } from '../../types';

const pageConfig = PAGE_CONFIGS.upload;

function AlbumCheckboxList({
  albums,
  selected,
  onToggle,
  filter,
  onFilterChange,
  placeholder,
  disabled,
}: {
  albums: Album[];
  selected: Set<string>;
  onToggle: (uid: string) => void;
  filter: string;
  onFilterChange: (v: string) => void;
  placeholder: string;
  disabled?: boolean;
}) {
  const { t } = useTranslation(['pages']);
  const filtered = filter
    ? albums.filter(a => a.title.toLowerCase().includes(filter.toLowerCase()))
    : albums;

  return (
    <div className="space-y-2">
      <input
        type="text"
        value={filter}
        onChange={e => onFilterChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-emerald-500"
      />
      <div className="max-h-40 overflow-y-auto space-y-1">
        {filtered.map(album => (
          <label
            key={album.uid}
            className="flex items-center space-x-2 px-2 py-1.5 rounded hover:bg-slate-700/50 cursor-pointer"
          >
            <input
              type="checkbox"
              checked={selected.has(album.uid)}
              onChange={() => onToggle(album.uid)}
              disabled={disabled}
              className="rounded bg-slate-700 border-slate-600 text-emerald-500 focus-visible:ring-emerald-500"
            />
            <span className="text-sm text-slate-300 truncate">{album.title}</span>
            <span className="text-xs text-slate-500 ml-auto shrink-0">{album.photo_count}</span>
          </label>
        ))}
        {filtered.length === 0 && (
          <p className="text-sm text-slate-500 px-2 py-1.5">{t('pages:albums.noAlbumsFound')}</p>
        )}
      </div>
    </div>
  );
}

function LabelTagInput({
  labels,
  selected,
  onAdd,
  onRemove,
  disabled,
}: {
  labels: Label[];
  selected: string[];
  onAdd: (name: string) => void;
  onRemove: (name: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation(['forms']);
  const [query, setQuery] = useState('');

  const suggestions = query.length > 0
    ? labels
        .filter(l => l.name.toLowerCase().includes(query.toLowerCase()))
        .filter(l => !selected.includes(l.name))
        .slice(0, 8)
    : [];

  const addLabel = (name: string) => {
    onAdd(name);
    setQuery('');
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && query.trim()) {
      e.preventDefault();
      addLabel(query.trim());
    }
  };

  return (
    <div className="space-y-2">
      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selected.map(name => (
            <span
              key={name}
              className="inline-flex items-center bg-emerald-500/20 text-emerald-400 text-xs px-2 py-1 rounded-full"
            >
              {name}
              {!disabled && (
                <button onClick={() => onRemove(name)} className="ml-1 hover:text-emerald-200">
                  &times;
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      <div className="relative">
        <input
          type="text"
          value={query}
          onChange={e => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={disabled}
          placeholder={t('forms:placeholders.addLabels')}
          className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-emerald-500"
        />
        {suggestions.length > 0 && (
          <ul className="absolute left-0 right-0 top-full mt-1 max-h-40 overflow-y-auto bg-slate-800 border border-slate-600 rounded-lg shadow-xl z-50">
            {suggestions.map(label => (
              <li
                key={label.uid}
                onMouseDown={e => {
                  e.preventDefault();
                  addLabel(label.name);
                }}
                className="px-3 py-1.5 text-sm text-slate-300 hover:bg-slate-700 cursor-pointer"
              >
                {label.name}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

const PHASE_LABELS: Record<string, string> = {
  uploading: 'phaseUploading',
  processing: 'phaseProcessing',
  detecting: 'phaseDetecting',
  labels: 'phaseLabels',
  albums: 'phaseAlbums',
  book: 'phaseBook',
  embeddings: 'phaseEmbeddings',
};

export function UploadPage() {
  const { t } = useTranslation(['pages', 'common', 'forms']);

  // Data loading
  const [albums, setAlbums] = useState<Album[]>([]);
  const [allLabels, setAllLabels] = useState<Label[]>([]);
  const [books, setBooks] = useState<PhotoBook[]>([]);
  const [bookDetail, setBookDetail] = useState<BookDetail | null>(null);

  // Form state
  const [files, setFiles] = useState<File[]>([]);
  const [selectedAlbums, setSelectedAlbums] = useState<Set<string>>(new Set());
  const [albumFilter, setAlbumFilter] = useState('');
  const [selectedLabels, setSelectedLabels] = useState<string[]>([]);
  const [selectedBookId, setSelectedBookId] = useState('');
  const [selectedSectionId, setSelectedSectionId] = useState('');
  const [autoProcess, setAutoProcess] = useState(true);

  // Upload job
  const {
    phase, progress, result, error,
    isRunning, isDone, isStarting,
    startUpload, cancelUpload, resetUpload,
  } = useUploadJob();

  // Load albums + labels + books
  useEffect(() => {
    void Promise.all([
      getAlbums({ count: 1000, order: 'name' }),
      getLabels({ count: 5000, all: true }),
      getBooks(),
    ]).then(([a, l, b]) => {
      setAlbums(a);
      setAllLabels(l);
      setBooks(b);
    });
  }, []);

  // Load book detail when book selected
  useEffect(() => {
    if (!selectedBookId) {
      setBookDetail(null);
      setSelectedSectionId('');
      return;
    }
    void getBook(selectedBookId).then(setBookDetail);
  }, [selectedBookId]);

  const toggleAlbum = useCallback((uid: string) => {
    setSelectedAlbums(prev => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  }, []);

  const addLabel = useCallback((name: string) => {
    setSelectedLabels(prev => prev.includes(name) ? prev : [...prev, name]);
  }, []);

  const removeLabel = useCallback((name: string) => {
    setSelectedLabels(prev => prev.filter(l => l !== name));
  }, []);

  const canUpload = files.length > 0 && selectedAlbums.size > 0 && !isRunning && !isStarting;

  const handleUpload = async () => {
    await startUpload(files, {
      album_uids: Array.from(selectedAlbums),
      labels: selectedLabels.length > 0 ? selectedLabels : undefined,
      book_section_id: selectedSectionId || undefined,
      auto_process: autoProcess,
    });
  };

  const handleReset = () => {
    resetUpload();
    setFiles([]);
  };

  const bookSections: BookSection[] = bookDetail?.sections ?? [];

  const progressPercent = progress && progress.total > 0
    ? Math.round((progress.current / progress.total) * 100)
    : 0;

  const phaseLabel = PHASE_LABELS[phase];

  return (
    <div className="max-w-6xl mx-auto space-y-6">
      <PageHeader
        icon={Upload}
        title={t('pages:upload.title')}
        subtitle={t('pages:upload.subtitle')}
        color={pageConfig.color}
        category={pageConfig.category}
      />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Left: Configuration */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">
              {t('pages:upload.configuration')}
            </h2>
          </CardHeader>
          <CardContent className="space-y-5">
            {/* Drop Zone */}
            <DropZone
              files={files}
              onFilesChange={setFiles}
              disabled={isRunning}
            />

            {/* Albums */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:upload.albums')}
              </label>
              <AlbumCheckboxList
                albums={albums}
                selected={selectedAlbums}
                onToggle={toggleAlbum}
                filter={albumFilter}
                onFilterChange={setAlbumFilter}
                placeholder={t('pages:upload.selectAlbums')}
                disabled={isRunning}
              />
              {selectedAlbums.size === 0 && files.length > 0 && (
                <p className="text-xs text-amber-400 mt-1">
                  {t('pages:upload.noAlbumsSelected')}
                </p>
              )}
            </div>

            {/* Labels */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:upload.labels')}
              </label>
              <LabelTagInput
                labels={allLabels}
                selected={selectedLabels}
                onAdd={addLabel}
                onRemove={removeLabel}
                disabled={isRunning}
              />
            </div>

            {/* Book Section */}
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                {t('pages:upload.bookSection')}
              </label>
              <div className="grid grid-cols-2 gap-2">
                <Combobox
                  value={selectedBookId}
                  onChange={v => {
                    setSelectedBookId(v);
                    setSelectedSectionId('');
                  }}
                  options={books.map(b => ({ value: b.id, label: b.title }))}
                  placeholder={t('pages:upload.selectBook')}
                  size="sm"
                  focusRingClass="focus-within:ring-1 focus-within:ring-emerald-500"
                />
                <Combobox
                  value={selectedSectionId}
                  onChange={setSelectedSectionId}
                  options={bookSections.map(s => ({ value: s.id, label: s.title }))}
                  placeholder={t('pages:upload.selectSection')}
                  size="sm"
                  focusRingClass="focus-within:ring-1 focus-within:ring-emerald-500"
                />
              </div>
            </div>

            {/* Auto-process */}
            <FormCheckbox
              label={t('pages:upload.autoProcess')}
              checked={autoProcess}
              onChange={e => setAutoProcess(e.target.checked)}
              disabled={isRunning}
            />

            {/* Upload button */}
            <div className="flex space-x-3">
              {!isRunning && !isDone && (
                <Button
                  variant="accent"
                  accentColor="emerald"
                  onClick={handleUpload}
                  disabled={!canUpload}
                  isLoading={isStarting}
                >
                  <Play className="h-4 w-4 mr-2" />
                  {t('pages:upload.startUpload')}
                </Button>
              )}
              {isRunning && (
                <Button variant="danger" onClick={cancelUpload}>
                  <Square className="h-4 w-4 mr-2" />
                  {t('common:buttons.cancel')}
                </Button>
              )}
              {isDone && (
                <Button variant="secondary" onClick={handleReset}>
                  <RotateCcw className="h-4 w-4 mr-2" />
                  {t('common:buttons.startNew')}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Right: Progress & Results */}
        <Card>
          <CardHeader>
            <h2 className="text-lg font-semibold text-white">
              {t('pages:upload.status')}
            </h2>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Idle state */}
            {phase === 'idle' && (
              <div className="text-center py-8">
                <Upload className="h-12 w-12 text-slate-600 mx-auto mb-3" />
                <p className="text-slate-500">{t('pages:upload.readyToUpload')}</p>
              </div>
            )}

            {/* Error */}
            {error && (
              <Alert variant="error">{error}</Alert>
            )}

            {/* Phase indicator */}
            {phaseLabel && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-slate-300 flex items-center">
                    <AlertCircle className="h-4 w-4 mr-2 text-emerald-400 animate-pulse" />
                    {t(`pages:upload.${phaseLabel}`)}
                  </span>
                  {progress && progress.total > 0 && (
                    <span className="text-sm text-slate-400">
                      {progress.current} / {progress.total}
                    </span>
                  )}
                </div>

                {/* Progress bar */}
                {progress && progress.total > 0 && (
                  <div className="w-full bg-slate-700 rounded-full h-2">
                    <div
                      className="bg-emerald-500 h-2 rounded-full transition-all duration-300"
                      style={{ width: `${progressPercent}%` }}
                    />
                  </div>
                )}

                {/* Current filename during upload */}
                {progress?.filename && (
                  <p className="text-xs text-slate-500 truncate">{progress.filename}</p>
                )}
              </div>
            )}

            {/* Completed */}
            {phase === 'completed' && (
              <div className="space-y-4">
                <div className="flex items-center text-emerald-400">
                  <CheckCircle className="h-5 w-5 mr-2" />
                  <span className="font-medium">{t('common:status.completed')}</span>
                </div>

                {result && (
                  <div className="grid grid-cols-2 gap-3">
                    <Stat label={t('pages:upload.uploaded')} value={result.uploaded} />
                    <Stat label={t('pages:upload.newPhotos')} value={result.new_photo_uids?.length ?? 0} />
                    {result.existing_count > 0 && (
                      <Stat label={t('pages:upload.existing')} value={result.existing_count} />
                    )}
                    {result.labels_applied > 0 && (
                      <Stat label={t('pages:upload.labelsApplied')} value={result.labels_applied} />
                    )}
                    {result.albums_applied > 0 && (
                      <Stat label={t('pages:upload.albumsApplied')} value={result.albums_applied} />
                    )}
                    {result.book_added > 0 && (
                      <Stat label={t('pages:upload.bookAdded')} value={result.book_added} />
                    )}
                  </div>
                )}

                {/* Thumbnail grid of uploaded photos */}
                {result?.new_photo_uids && result.new_photo_uids.length > 0 && (
                  <div>
                    <h3 className="text-sm font-medium text-slate-300 mb-2">
                      {t('pages:upload.uploadedPhotos')}
                    </h3>
                    <div className="grid grid-cols-4 sm:grid-cols-5 gap-2">
                      {result.new_photo_uids.slice(0, 20).map(uid => (
                        <a
                          key={uid}
                          href={`/photos/${uid}`}
                          className="aspect-square rounded-lg overflow-hidden bg-slate-700 hover:ring-2 hover:ring-emerald-400 transition-all"
                        >
                          <img
                            src={getThumbnailUrl(uid, 'tile_224')}
                            alt=""
                            className="w-full h-full object-cover"
                            loading="lazy"
                          />
                        </a>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* Failed */}
            {phase === 'failed' && !error && (
              <div className="flex items-center text-red-400">
                <XCircle className="h-5 w-5 mr-2" />
                <span className="font-medium">{t('common:status.failed')}</span>
              </div>
            )}

            {/* Cancelled */}
            {phase === 'cancelled' && (
              <div className="flex items-center text-amber-400">
                <AlertCircle className="h-5 w-5 mr-2" />
                <span className="font-medium">{t('common:status.cancelled')}</span>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="bg-slate-700/50 rounded-lg px-3 py-2">
      <p className="text-xs text-slate-400">{label}</p>
      <p className="text-lg font-semibold text-white">{value}</p>
    </div>
  );
}
