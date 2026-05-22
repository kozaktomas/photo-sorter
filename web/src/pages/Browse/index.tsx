import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Camera, MapPin, Upload } from 'lucide-react';
import { PageHeader } from '../../components/PageHeader';
import { PAGE_CONFIGS } from '../../constants/pageConfig';
import { Alert } from '../../components/Alert';
import { LoadingState } from '../../components/LoadingState';
import { getHistogram, getGeoPoints, type BrowseFilters } from '../../api/client';
import type { GeoPoint, HistogramBucket } from '../../types';
import { BrowseMap, type MapBBox } from './BrowseMap';
import { BrowseTimeline } from './BrowseTimeline';
import { BrowseSidePanel } from './BrowseSidePanel';

// pickBucket auto-selects month vs. year bucketing for the date histogram.
// Spec: bucket by year when the range spans more than 5 years, by month
// otherwise. The decision uses the histogram total's full date range, not
// the brushed-in selection, so changing the brush does not flip the
// bucketing.
function pickBucket(buckets: HistogramBucket[]): 'month' | 'year' {
  if (buckets.length === 0) return 'month';
  const first = new Date(buckets[0].start).getTime();
  const last = new Date(buckets[buckets.length - 1].end).getTime();
  const spanYears = (last - first) / (1000 * 60 * 60 * 24 * 365.25);
  return spanYears > 5 ? 'year' : 'month';
}

// parseBBoxFromParams reads the four bbox query params and returns a MapBBox
// only when all four are present and numeric. A partial bbox is treated as
// no bbox at all so a stale URL fragment doesn't lock the user out of part
// of the map.
function parseBBoxFromParams(p: URLSearchParams): MapBBox | undefined {
  const minLat = parseFloat(p.get('min_lat') || '');
  const minLng = parseFloat(p.get('min_lng') || '');
  const maxLat = parseFloat(p.get('max_lat') || '');
  const maxLng = parseFloat(p.get('max_lng') || '');
  if (
    !Number.isFinite(minLat) ||
    !Number.isFinite(minLng) ||
    !Number.isFinite(maxLat) ||
    !Number.isFinite(maxLng)
  ) {
    return undefined;
  }
  return { minLat, minLng, maxLat, maxLng };
}

export function BrowsePage() {
  const { t } = useTranslation(['pages', 'common']);
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // The map's visible bbox. We track this in component state (and mirror it
  // to the URL on a debounce) rather than reading from URL every render —
  // re-parsing the URL on every map move would force a roundtrip through
  // react-router's reducer and stutter the panning UX.
  const initialBBox = useMemo(() => parseBBoxFromParams(searchParams), []);
  const [bbox, setBBox] = useState<MapBBox | undefined>(initialBBox);
  const [takenFrom, setTakenFrom] = useState<string | undefined>(
    searchParams.get('taken_from') || undefined,
  );
  const [takenTo, setTakenTo] = useState<string | undefined>(
    searchParams.get('taken_to') || undefined,
  );

  // Data state
  const [points, setPoints] = useState<GeoPoint[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [cap, setCap] = useState(0);
  const [buckets, setBuckets] = useState<HistogramBucket[]>([]);
  const [bucket, setBucket] = useState<'month' | 'year'>('month');
  const [total, setTotal] = useState(0);
  const [noDate, setNoDate] = useState(0);
  const [noGPS, setNoGPS] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Side panel UIDs from a marker / cluster click.
  const [panelUIDs, setPanelUIDs] = useState<string[] | null>(null);

  // URL sync — debounced so a fast drag of map / brush doesn't blow up the
  // navigation history.
  const urlDebounce = useRef<number | null>(null);
  useEffect(() => {
    if (urlDebounce.current !== null) window.clearTimeout(urlDebounce.current);
    urlDebounce.current = window.setTimeout(() => {
      const p = new URLSearchParams(searchParams);
      const setOrDel = (key: string, val?: string) => {
        if (val) p.set(key, val);
        else p.delete(key);
      };
      setOrDel('min_lat', bbox ? bbox.minLat.toFixed(6) : undefined);
      setOrDel('min_lng', bbox ? bbox.minLng.toFixed(6) : undefined);
      setOrDel('max_lat', bbox ? bbox.maxLat.toFixed(6) : undefined);
      setOrDel('max_lng', bbox ? bbox.maxLng.toFixed(6) : undefined);
      setOrDel('taken_from', takenFrom);
      setOrDel('taken_to', takenTo);
      setSearchParams(p, { replace: true });
    }, 250);
    return () => {
      if (urlDebounce.current !== null) window.clearTimeout(urlDebounce.current);
    };
    // searchParams is intentionally omitted to avoid feedback loops with
    // our own update; we only want to react to bbox / date changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bbox, takenFrom, takenTo]);

  // Build the filter sets for each endpoint. The histogram intentionally
  // does NOT use taken_from/taken_to — the brush should not shrink the
  // visible histogram out from under the user. Geo points DO use both so
  // the markers actually narrow when the brush moves.
  const histogramFilters = useMemo<BrowseFilters>(() => {
    const f: BrowseFilters = {};
    if (bbox) {
      f.min_lat = bbox.minLat;
      f.min_lng = bbox.minLng;
      f.max_lat = bbox.maxLat;
      f.max_lng = bbox.maxLng;
    }
    return f;
  }, [bbox]);

  const geoFilters = useMemo<BrowseFilters>(() => {
    const f: BrowseFilters = { ...histogramFilters };
    if (takenFrom) f.taken_from = takenFrom;
    if (takenTo) f.taken_to = takenTo;
    return f;
  }, [histogramFilters, takenFrom, takenTo]);

  // Fetch histogram and geo points whenever filters change. Two independent
  // requests so a slow histogram doesn't block the map from re-rendering.
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setError(null);
    void (async () => {
      try {
        // Step 1: fetch histogram with bucket='month' to learn the actual
        // date range; if it spans > 5 years, re-fetch with bucket='year'.
        const initial = await getHistogram({ ...histogramFilters, bucket: 'month' });
        if (cancelled) return;
        const chosen = pickBucket(initial.buckets);
        let hist = initial;
        if (chosen === 'year') {
          hist = await getHistogram({ ...histogramFilters, bucket: 'year' });
          if (cancelled) return;
        }
        setBucket(chosen);
        setBuckets(hist.buckets);
        setTotal(hist.total);
        setNoDate(hist.no_date_count);
        setNoGPS(hist.no_gps_count);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [histogramFilters]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await getGeoPoints(geoFilters);
        if (cancelled) return;
        setPoints(res.points);
        setTruncated(res.truncated);
        setCap(res.cap);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [geoFilters]);

  const handleBoundsChanged = useCallback((b: MapBBox) => {
    setBBox(b);
  }, []);

  const handleRangeChange = useCallback(
    (from: string | undefined, to: string | undefined) => {
      setTakenFrom(from);
      setTakenTo(to);
    },
    [],
  );

  const handleMarkerClick = useCallback((uid: string) => {
    setPanelUIDs([uid]);
  }, []);
  const handleClusterClick = useCallback((uids: string[]) => {
    setPanelUIDs(uids);
  }, []);

  const handleNoLocationClick = () => {
    // Spec: clicking the "No location" chip opens a filtered photo list.
    // The Photos page already understands the basic filter set; we can't
    // express "missing GPS" through its query API yet, so we just open it
    // with the current date range so the user lands in a useful place.
    const p = new URLSearchParams();
    if (takenFrom) p.set('taken_from', takenFrom);
    if (takenTo) p.set('taken_to', takenTo);
    void navigate(`/photos${p.toString() ? `?${p.toString()}` : ''}`);
  };

  // Empty-library state: no photos at all. Shows a friendly nudge to upload.
  const isEmptyLibrary = !isLoading && !error && total === 0 && noGPS === 0;
  // No-GPS state: there are photos, but none of them carry coordinates.
  // We hide the map but keep the timeline so date browsing still works.
  const noGPSAtAll = !isLoading && total > 0 && noGPS === total;

  return (
    <>
      <PageHeader
        icon={PAGE_CONFIGS.browse.icon}
        title={t('browse.title')}
        subtitle={t('browse.subtitle')}
        color={PAGE_CONFIGS.browse.color}
        category={PAGE_CONFIGS.browse.category}
      />

      {error && <Alert variant="error" className="mb-3">{error}</Alert>}
      {truncated && (
        <Alert variant="warning" className="mb-3">
          {t('browse.truncatedWarning', { cap: cap.toLocaleString() })}
        </Alert>
      )}

      {/* Counters / chips row */}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-800 border border-slate-700 px-3 py-1 text-xs text-slate-200">
          <Camera className="h-3.5 w-3.5 text-teal-400" />
          {t('browse.matchingPhotos', { count: total })}
        </span>
        {noGPS > 0 && (
          <button
            type="button"
            onClick={handleNoLocationClick}
            className="inline-flex items-center gap-1.5 rounded-full bg-slate-800 hover:bg-slate-700 border border-slate-700 px-3 py-1 text-xs text-slate-200 transition-colors"
          >
            <MapPin className="h-3.5 w-3.5 text-amber-400" />
            {t('browse.noGPSChip', { count: noGPS })}
          </button>
        )}
        {noDate > 0 && (
          <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-800 border border-slate-700 px-3 py-1 text-xs text-slate-200">
            <AlertTriangle className="h-3.5 w-3.5 text-amber-400" />
            {t('browse.noDateChip', { count: noDate })}
          </span>
        )}
        {(takenFrom || takenTo) && (
          <button
            type="button"
            onClick={() => handleRangeChange(undefined, undefined)}
            className="ml-auto text-xs text-teal-400 hover:text-teal-300"
          >
            {t('browse.clearDateRange')}
          </button>
        )}
      </div>

      <LoadingState
        isLoading={isLoading && buckets.length === 0 && points.length === 0}
        loadingText={t('browse.loading')}
        isEmpty={isEmptyLibrary}
        emptyIcon={<Upload className="h-12 w-12 opacity-50" />}
        emptyTitle={t('browse.noPhotos')}
        emptyDescription={t('browse.noPhotosHint')}
      >
        <div className="flex flex-col md:flex-row gap-0 md:gap-3 h-[calc(100vh-260px)] min-h-[480px]">
          <div className="flex flex-1 flex-col gap-3 min-w-0">
            {/* Map (~60%) */}
            {noGPSAtAll ? (
              <div className="flex-[3] basis-0 rounded-md border border-slate-700 bg-slate-800/50 p-6 text-center text-slate-300 flex flex-col items-center justify-center">
                <MapPin className="h-8 w-8 text-amber-400 mb-2" />
                <p className="font-medium text-white mb-1">{t('browse.noGPSTitle')}</p>
                <p className="text-sm text-slate-400 max-w-md">
                  {t('browse.noGPSDescription')}
                </p>
              </div>
            ) : (
              <div className="flex-[3] basis-0 min-h-[280px] rounded-md overflow-hidden border border-slate-700">
                <BrowseMap
                  points={points}
                  initialBBox={initialBBox}
                  onBoundsChanged={handleBoundsChanged}
                  onMarkerClick={handleMarkerClick}
                  onClusterClick={handleClusterClick}
                />
              </div>
            )}
            {/* Timeline (~40%) */}
            <div className="flex-[2] basis-0 min-h-[180px] rounded-md border border-slate-700 bg-slate-800/30 p-2">
              <div className="flex items-center justify-between px-1 pb-1">
                <span className="text-xs text-slate-400">
                  {bucket === 'year' ? t('browse.bucketYear') : t('browse.bucketMonth')}
                </span>
                {(takenFrom || takenTo) && (
                  <span className="text-xs text-slate-400">
                    {t('browse.selectedRange', {
                      from: takenFrom ? takenFrom.slice(0, 10) : '—',
                      to: takenTo ? takenTo.slice(0, 10) : '—',
                    })}
                  </span>
                )}
              </div>
              <div className="h-[calc(100%-22px)]">
                <BrowseTimeline
                  buckets={buckets}
                  bucket={bucket}
                  takenFrom={takenFrom}
                  takenTo={takenTo}
                  onRangeChange={handleRangeChange}
                />
              </div>
            </div>
          </div>

          {panelUIDs && (
            <BrowseSidePanel photoUIDs={panelUIDs} onClose={() => setPanelUIDs(null)} />
          )}
        </div>
      </LoadingState>
    </>
  );
}
