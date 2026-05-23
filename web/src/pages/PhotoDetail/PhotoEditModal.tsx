import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { RotateCcw, RotateCw, Save, X } from 'lucide-react';
import Cropper from 'react-easy-crop';
import type { Area, Point } from 'react-easy-crop';
import { Button } from '../../components/Button';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { useToast } from '../../components/Toast';
import {
  deletePhotoEdits,
  getPhotoEdits,
  getThumbnailUrl,
  savePhotoEdits,
} from '../../api/client';
import type { Photo, PhotoEdits, PhotoEditsCrop } from '../../types';

interface PhotoEditModalProps {
  photo: Photo;
  onClose: () => void;
  onSaved: () => void;
}

type Rotation = 0 | 90 | 180 | 270;

function isRotation(value: number): value is Rotation {
  return value === 0 || value === 90 || value === 180 || value === 270;
}

// imageRotationFromEdits returns the next clockwise rotation step after
// adding `delta` 90° turns, normalised to one of 0/90/180/270.
function rotateBy(current: Rotation, delta: number): Rotation {
  const next = (((current + delta) % 360) + 360) % 360;
  return isRotation(next) ? next : 0;
}

// areaToRelativeCrop maps the react-easy-crop pixel Area against the
// pre-rotated image dimensions to 0..1 relative crop coordinates. The
// Cropper component already accounts for the configured rotation, so the
// returned coordinates are in the post-rotation (display) coordinate
// frame — exactly the space the API expects.
function areaToRelativeCrop(area: Area, mediaW: number, mediaH: number): PhotoEditsCrop | null {
  if (mediaW <= 0 || mediaH <= 0) return null;
  return {
    x: Math.max(0, Math.min(1, area.x / mediaW)),
    y: Math.max(0, Math.min(1, area.y / mediaH)),
    w: Math.max(0, Math.min(1, area.width / mediaW)),
    h: Math.max(0, Math.min(1, area.height / mediaH)),
  };
}

export function PhotoEditModal({ photo, onClose, onSaved }: PhotoEditModalProps) {
  const { t } = useTranslation(['pages', 'common']);
  const toast = useToast();

  // react-easy-crop state
  const [crop, setCrop] = useState<Point>({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [croppedAreaPixels, setCroppedAreaPixels] = useState<Area | null>(null);

  // Edit-parameter state
  const [rotation, setRotation] = useState<Rotation>(0);
  const [brightness, setBrightness] = useState(0);
  const [contrast, setContrast] = useState(0);
  const [enableCrop, setEnableCrop] = useState(false);

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [warning, setWarning] = useState<string | null>(null);
  const [confirmRestore, setConfirmRestore] = useState(false);

  // Display size — post-rotation. The crop coordinates the API stores
  // are 0..1 against the rotated image, which is also what
  // react-easy-crop uses internally.
  const displayWidth =
    rotation === 90 || rotation === 270 ? photo.height : photo.width;
  const displayHeight =
    rotation === 90 || rotation === 270 ? photo.width : photo.height;

  // Load existing edits.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const stored = await getPhotoEdits(photo.uid);
        if (cancelled || !stored) {
          setLoading(false);
          return;
        }
        if (isRotation(stored.rotation)) setRotation(stored.rotation);
        setBrightness(stored.brightness);
        setContrast(stored.contrast);
        if (stored.crop) {
          setEnableCrop(true);
          setCrop({ x: 0, y: 0 });
          // We'll wait for the Cropper to mount and let the user adjust
          // again — round-tripping the exact stored crop requires
          // converting back to pixel-space which only works after the
          // image dimensions are known. Store it as the initial
          // croppedAreaPixels so saving without further interaction
          // preserves the existing crop.
          const w = rotation === 90 || rotation === 270 ? photo.height : photo.width;
          const h = rotation === 90 || rotation === 270 ? photo.width : photo.height;
          setCroppedAreaPixels({
            x: stored.crop.x * w,
            y: stored.crop.y * h,
            width: stored.crop.w * w,
            height: stored.crop.h * h,
          });
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [photo.uid]);

  // Close on Escape.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  const onCropComplete = useCallback((_: Area, areaPixels: Area) => {
    setCroppedAreaPixels(areaPixels);
  }, []);

  const handleRotateLeft = () => {
    setRotation((r) => rotateBy(r, -90));
    // Clearing the crop on rotation avoids carrying over a crop that
    // referenced the previous orientation. The user can re-crop after
    // rotating if they want both transforms.
    setCroppedAreaPixels(null);
    setEnableCrop(false);
  };
  const handleRotateRight = () => {
    setRotation((r) => rotateBy(r, 90));
    setCroppedAreaPixels(null);
    setEnableCrop(false);
  };

  const buildSavePayload = (): {
    payload: Omit<PhotoEdits, 'updated_at'>;
    cropPx: { w: number; h: number } | null;
  } => {
    let cropPayload: PhotoEditsCrop | null = null;
    let cropPx: { w: number; h: number } | null = null;
    if (enableCrop && croppedAreaPixels) {
      cropPayload = areaToRelativeCrop(croppedAreaPixels, displayWidth, displayHeight);
      if (cropPayload) {
        cropPx = {
          w: Math.round(cropPayload.w * displayWidth),
          h: Math.round(cropPayload.h * displayHeight),
        };
      }
    }
    return {
      payload: {
        crop: cropPayload,
        rotation,
        brightness,
        contrast,
      },
      cropPx,
    };
  };

  const handleSave = async () => {
    setError(null);
    setWarning(null);
    const { payload, cropPx } = buildSavePayload();
    if (cropPx && (cropPx.w < 100 || cropPx.h < 100)) {
      setWarning(t('pages:edit.minCropWarning'));
      return;
    }
    setSaving(true);
    try {
      await savePhotoEdits(photo.uid, payload);
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleRestore = async () => {
    setConfirmRestore(false);
    setError(null);
    setSaving(true);
    try {
      await deletePhotoEdits(photo.uid);
      toast.success(t('common:toasts.photo.editsCleared'));
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  // The preview image. We pull the largest cached thumb the server
  // typically holds (fit_2560) so the cropper has enough pixels but we
  // don't download a multi-megabyte original.
  const imageUrl = `${getThumbnailUrl(photo.uid, 'fit_2560')}?original=true&cb=${photo.uid}`;

  const previewFilter = `brightness(${1 + brightness}) contrast(${1 + contrast})`;

  return (
    <div
      className="fixed inset-0 z-50 flex items-stretch sm:items-center justify-center bg-black/80"
      role="dialog"
      aria-modal="true"
      aria-labelledby="photo-edit-title"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="bg-slate-900 sm:rounded-lg sm:max-w-6xl w-full sm:m-4 flex flex-col max-h-screen overflow-hidden">
        {/* Header */}
        <div className="flex items-start justify-between px-4 py-3 border-b border-slate-700 shrink-0">
          <div>
            <h2 id="photo-edit-title" className="text-lg font-semibold text-white">
              {t('pages:edit.title')}
            </h2>
            <p className="text-slate-400 text-xs mt-0.5">{t('pages:edit.subtitle')}</p>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-white p-1"
            aria-label={t('common:buttons.close', { defaultValue: 'Close' })}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 flex flex-col md:flex-row overflow-hidden">
          {/* Preview */}
          <div className="relative flex-1 bg-black min-h-[300px] md:min-h-[500px]">
            {loading ? (
              <div className="absolute inset-0 flex items-center justify-center text-slate-300 text-sm">
                {t('pages:edit.loading')}
              </div>
            ) : (
              <Cropper
                image={imageUrl}
                crop={crop}
                zoom={zoom}
                rotation={rotation}
                aspect={undefined}
                onCropChange={setCrop}
                onZoomChange={setZoom}
                onCropComplete={onCropComplete}
                onCropAreaChange={(_, areaPx) => {
                  if (!enableCrop) {
                    setEnableCrop(true);
                  }
                  setCroppedAreaPixels(areaPx);
                }}
                style={{
                  mediaStyle: { filter: previewFilter },
                  containerStyle: { background: '#000' },
                }}
                restrictPosition={true}
                showGrid={true}
              />
            )}
          </div>

          {/* Controls */}
          <div className="w-full md:w-80 shrink-0 border-l border-slate-700 p-4 space-y-4 overflow-y-auto">
            {/* Rotation */}
            <div>
              <div className="text-sm font-medium text-slate-200 mb-1">
                {t('pages:edit.rotation')} ({rotation}°)
              </div>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleRotateLeft}
                  title={t('pages:edit.rotateLeft')}
                >
                  <RotateCcw className="h-4 w-4 mr-1" />
                  {t('pages:edit.rotateLeft')}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={handleRotateRight}
                  title={t('pages:edit.rotateRight')}
                >
                  <RotateCw className="h-4 w-4 mr-1" />
                  {t('pages:edit.rotateRight')}
                </Button>
              </div>
            </div>

            {/* Brightness */}
            <div>
              <label className="block text-sm font-medium text-slate-200 mb-1">
                {t('pages:edit.brightness')} ({Math.round(brightness * 100)})
              </label>
              <input
                type="range"
                min={-100}
                max={100}
                step={1}
                value={Math.round(brightness * 100)}
                onChange={(e) => setBrightness(Number(e.target.value) / 100)}
                className="w-full"
              />
            </div>

            {/* Contrast */}
            <div>
              <label className="block text-sm font-medium text-slate-200 mb-1">
                {t('pages:edit.contrast')} ({Math.round(contrast * 100)})
              </label>
              <input
                type="range"
                min={-100}
                max={100}
                step={1}
                value={Math.round(contrast * 100)}
                onChange={(e) => setContrast(Number(e.target.value) / 100)}
                className="w-full"
              />
            </div>

            {/* Crop info */}
            <div className="text-xs text-slate-400">
              {enableCrop && croppedAreaPixels ? (
                <span>
                  {t('pages:edit.crop')}: {Math.round(croppedAreaPixels.width)}×
                  {Math.round(croppedAreaPixels.height)} px
                </span>
              ) : (
                <span>{t('pages:edit.crop')}: —</span>
              )}
            </div>

            {warning && (
              <div className="text-amber-300 text-sm bg-amber-900/30 border border-amber-700 rounded px-2 py-1">
                {warning}
              </div>
            )}
            {error && (
              <div className="text-red-300 text-sm bg-red-900/30 border border-red-700 rounded px-2 py-1">
                {error}
              </div>
            )}

            <div className="pt-2 border-t border-slate-700 flex flex-col gap-2">
              <Button onClick={() => void handleSave()} isLoading={saving} disabled={loading}>
                <Save className="h-4 w-4 mr-1" />
                {saving ? t('pages:edit.saving') : t('pages:edit.save')}
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => setConfirmRestore(true)}
                disabled={loading || saving}
              >
                {t('pages:edit.restoreOriginal')}
              </Button>
              <Button variant="ghost" size="sm" onClick={onClose} disabled={saving}>
                {t('pages:edit.cancel')}
              </Button>
            </div>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={confirmRestore}
        title={t('pages:edit.restoreOriginal')}
        message={t('pages:edit.restoreConfirm')}
        confirmLabel={t('pages:edit.restoreOriginal')}
        cancelLabel={t('common:buttons.cancel')}
        variant="danger"
        onConfirm={() => void handleRestore()}
        onCancel={() => setConfirmRestore(false)}
      />
    </div>
  );
}
