// Leaflet bundles its default marker icon as a relative URL that breaks
// under Vite's static asset pipeline. Pulling the three image files in as
// modules and re-pointing Icon.Default at them makes the default markers
// render — without this, every marker is invisible.
//
// Imported for its side effect from BrowseMap; do not import elsewhere.

import L from 'leaflet';
import iconUrl from 'leaflet/dist/images/marker-icon.png';
import iconRetinaUrl from 'leaflet/dist/images/marker-icon-2x.png';
import shadowUrl from 'leaflet/dist/images/marker-shadow.png';

/* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access */
delete (L.Icon.Default.prototype as any)._getIconUrl;
/* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access */
L.Icon.Default.mergeOptions({ iconUrl, iconRetinaUrl, shadowUrl });
