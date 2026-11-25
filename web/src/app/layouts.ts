// Each map's layout for the map window's World tab, fetched once a session (map-views.md section 12): its size, ground and a region's waypoints.
import type { MapLayout } from '@/features/map/MapWindow';
import { mapGround } from '@/game/map-image';
import { loadMap } from '@/game/world3d/map';

const kept = new Map<string, Promise<MapLayout>>();

/** Map id's size and ground, from its JSON fetched the first time it's asked for; one that fails is fetched again when next asked. */
export function layoutOf(id: string): Promise<MapLayout> {
  let layout = kept.get(id);
  if (!layout) {
    layout = loadMap(id).then((m) => ({
      size: [m.geometry.size_x, m.geometry.size_z] as const,
      ground: mapGround(m),
      waypoints: m.waypoints,
    }));
    kept.set(id, layout);
    layout.catch(() => kept.delete(id));
  }
  return layout;
}
