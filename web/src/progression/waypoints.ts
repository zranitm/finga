// A region's waypoints by id, from the data every map's regions carry, so a name never waits on which map is showing.
import { maps } from '@/gamedata/maps';

export interface Waypoint {
  readonly id: string;
  readonly name: string;
}

const NAMES = new Map(
  maps.flatMap((m): readonly Waypoint[] => m.waypoints).map((w) => [w.id, w.name] as const),
);

/** The waypoint's name, or its id for one this client's data lacks. */
export function waypointName(id: string): string {
  return NAMES.get(id) ?? id;
}
