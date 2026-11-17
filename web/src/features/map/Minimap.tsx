// The minimap (map-views.md section 5): 40 m round the player, north up, held inside the map at its edges; it ignores the pointer.
import { mapView } from '@/state/map-view';
import { settings } from '@/state/settings';

import { MapMarks } from './MapMarks';

export const MINIMAP_M = 40; // metres across
const MINIMAP_PX = 180;

/** The span of `view` metres along one axis of a map `size` long, centred on `at` but held inside the map; a map shorter than the view is centred. */
function along(at: number, size: number, view: number): number {
  if (size <= view) {
    return (size - view) / 2;
  }
  return Math.min(Math.max(at - view / 2, 0), size - view);
}

/** The minimap's SVG view box round the player at (x, z) on a map of size: its west edge, its north edge as y down from the map's north, and its sides. */
export function viewBoxOf(x: number, z: number, size: readonly [number, number]): string {
  const [sx, sz] = size;
  return `${along(x, sx, MINIMAP_M)} ${along(sz - z, sz, MINIMAP_M)} ${MINIMAP_M} ${MINIMAP_M}`;
}

export function Minimap() {
  const shown = settings.use((s) => s.showMinimap);
  const size = mapView.use((s) => s.size);
  const me = mapView.use((s) => s.me);
  if (!shown || !me) {
    return null;
  }
  return (
    <svg
      className="minimap"
      role="img"
      aria-label="Minimap"
      width={MINIMAP_PX}
      height={MINIMAP_PX}
      viewBox={viewBoxOf(me.x, me.z, size)}
    >
      <MapMarks unit={MINIMAP_M / MINIMAP_PX} />
    </svg>
  );
}
