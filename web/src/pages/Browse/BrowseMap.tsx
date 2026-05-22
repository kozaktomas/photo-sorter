import { useEffect, useMemo, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { MapContainer, TileLayer, Marker, useMap, useMapEvents } from 'react-leaflet';
import MarkerClusterGroup from 'react-leaflet-cluster';
import L, { type LatLngBoundsExpression } from 'leaflet';
import 'leaflet.markercluster';
import './leafletSetup';
import type { GeoPoint } from '../../types';

export interface MapBBox {
  minLat: number;
  minLng: number;
  maxLat: number;
  maxLng: number;
}

interface BrowseMapProps {
  points: GeoPoint[];
  initialBBox?: MapBBox;
  onBoundsChanged: (bbox: MapBBox) => void;
  onMarkerClick: (uid: string) => void;
  onClusterClick: (uids: string[]) => void;
}

// computeFitBounds returns the bounding box that frames the supplied points
// with a small visual padding. Falls back to a world view when there are no
// points, which lets the user pan to wherever their photos actually live.
function computeFitBounds(points: GeoPoint[]): LatLngBoundsExpression {
  if (points.length === 0) {
    return [
      [-60, -180],
      [75, 180],
    ];
  }
  let minLat = points[0].lat;
  let maxLat = points[0].lat;
  let minLng = points[0].lng;
  let maxLng = points[0].lng;
  for (const p of points) {
    if (p.lat < minLat) minLat = p.lat;
    if (p.lat > maxLat) maxLat = p.lat;
    if (p.lng < minLng) minLng = p.lng;
    if (p.lng > maxLng) maxLng = p.lng;
  }
  // Single-point libraries need a real bounding box for fitBounds to behave;
  // a 0.05 degree halo (~5km) keeps it readable without zooming to street level.
  if (minLat === maxLat && minLng === maxLng) {
    minLat -= 0.05;
    maxLat += 0.05;
    minLng -= 0.05;
    maxLng += 0.05;
  }
  return [
    [minLat, minLng],
    [maxLat, maxLng],
  ];
}

// BoundsListener fires onBoundsChanged on every map move/zoom. Debouncing
// happens in the parent so this stays a thin react-leaflet adapter.
function BoundsListener({ onBoundsChanged }: { onBoundsChanged: (b: MapBBox) => void }) {
  const map = useMapEvents({
    moveend: () => emit(),
    zoomend: () => emit(),
  });

  function emit() {
    const b = map.getBounds();
    onBoundsChanged({
      minLat: b.getSouth(),
      minLng: b.getWest(),
      maxLat: b.getNorth(),
      maxLng: b.getEast(),
    });
  }

  return null;
}

// InitialFit centers the map on the supplied bbox the first time it sees a
// non-empty value. After that it stays out of the way so panning by the user
// or by URL params is the only thing that moves the view.
function InitialFit({ bbox, fallbackPoints }: { bbox?: MapBBox; fallbackPoints: GeoPoint[] }) {
  const map = useMap();
  const didFitRef = useRef(false);

  useEffect(() => {
    if (didFitRef.current) return;
    if (bbox) {
      map.fitBounds(
        [
          [bbox.minLat, bbox.minLng],
          [bbox.maxLat, bbox.maxLng],
        ],
        { animate: false },
      );
      didFitRef.current = true;
      return;
    }
    if (fallbackPoints.length > 0) {
      map.fitBounds(computeFitBounds(fallbackPoints), { padding: [40, 40], animate: false });
      didFitRef.current = true;
    }
  }, [bbox, fallbackPoints, map]);

  return null;
}

// makePointIcon returns the small dot used for individual markers. Sized to
// stay legible at typical zoom levels while not eclipsing the underlying
// terrain when several markers are next to each other.
const pointIcon = L.divIcon({
  html: '<div style="width:14px;height:14px;border-radius:50%;background:#14b8a6;border:2px solid #0f172a;box-shadow:0 1px 4px rgba(0,0,0,0.4);"></div>',
  className: 'browse-point-icon',
  iconSize: [14, 14],
  iconAnchor: [7, 7],
});

// makeClusterIcon scales the cluster pill so a 5-photo cluster reads small
// and a 1000-photo cluster reads large, without ever exceeding ~56px.
function makeClusterIcon(cluster: L.MarkerCluster): L.DivIcon {
  const count = cluster.getChildCount();
  let size = 32;
  if (count > 10) size = 38;
  if (count > 100) size = 46;
  if (count > 1000) size = 54;
  return L.divIcon({
    html: `<div class="browse-cluster" style="width:${size}px;height:${size}px;">${count}</div>`,
    className: 'browse-cluster-wrapper',
    iconSize: [size, size],
  });
}

// posKey turns a lat/lng pair into a string usable as a Map key. Six
// decimal digits is ~10cm precision — well past the GPS noise floor.
function posKey(lat: number, lng: number): string {
  return `${lat.toFixed(6)},${lng.toFixed(6)}`;
}

export function BrowseMap({
  points,
  initialBBox,
  onBoundsChanged,
  onMarkerClick,
  onClusterClick,
}: BrowseMapProps) {
  const { t } = useTranslation('pages');

  // posIndex lets the cluster onClick handler resolve marker -> uid in O(1)
  // instead of scanning all points per child. Multiple photos can share the
  // exact same coordinates (burst shots from one spot), so the value is an
  // array, not a single uid.
  const posIndex = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const p of points) {
      const k = posKey(p.lat, p.lng);
      const existing = m.get(k);
      if (existing) existing.push(p.uid);
      else m.set(k, [p.uid]);
    }
    return m;
  }, [points]);

  // The map only renders a fresh marker tree when the photo set actually
  // changes; otherwise every histogram tick would force a full cluster
  // rebuild and visibly jitter the markers.
  const markers = useMemo(
    () =>
      points.map(p => (
        <Marker
          key={p.uid}
          position={[p.lat, p.lng]}
          icon={pointIcon}
          eventHandlers={{
            click: () => onMarkerClick(p.uid),
          }}
        />
      )),
    [points, onMarkerClick],
  );

  return (
    <MapContainer
      className="browse-map h-full w-full rounded-md"
      center={[20, 0]}
      zoom={2}
      worldCopyJump
      attributionControl
    >
      <TileLayer
        attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
        maxZoom={19}
      />
      <InitialFit bbox={initialBBox} fallbackPoints={points} />
      <BoundsListener onBoundsChanged={onBoundsChanged} />
      <MarkerClusterGroup
        chunkedLoading
        showCoverageOnHover={false}
        spiderfyOnMaxZoom
        zoomToBoundsOnClick={false}
        iconCreateFunction={makeClusterIcon}
        onClick={(e: L.LeafletMouseEvent) => {
          const cluster = (e as unknown as { layer: L.MarkerCluster }).layer;
          if (!cluster || typeof cluster.getAllChildMarkers !== 'function') return;
          const children = cluster.getAllChildMarkers();
          const seen = new Set<string>();
          const uids: string[] = [];
          // children may include several markers at the exact same lat/lng;
          // dedupe via a per-position seen Set so the side panel doesn't
          // list each shared position multiple times.
          for (const child of children) {
            const ll = child.getLatLng();
            const k = posKey(ll.lat, ll.lng);
            if (seen.has(k)) continue;
            seen.add(k);
            const list = posIndex.get(k);
            if (list) uids.push(...list);
          }
          onClusterClick(uids);
        }}
      >
        {markers}
      </MarkerClusterGroup>
      {points.length === 0 && (
        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-[400] pointer-events-none bg-slate-900/80 px-3 py-1.5 rounded text-xs text-slate-300">
          {t('browse.noGPSTitle')}
        </div>
      )}
    </MapContainer>
  );
}
