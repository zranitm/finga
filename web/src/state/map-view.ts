// What the map views draw that only the world scene knows (map-views.md sections 4 and 5): the ground, a map's image made as it loads or a region's baked tiles, and where the player, the party, the lock and the field boss are drawn, set about ten times a second.
import { defineStore } from './store';

/** A place on the map: x east and z north, in metres. */
export interface Spot {
  readonly x: number;
  readonly z: number;
}

/** A region's waypoint, as the world draws its stone and the map window marks it: the stone's spot, the ground's height there, and whether it's in the region's main town, open to a teleport unfound (large-regions.md section 8). */
export interface MapWaypoint {
  readonly id: string;
  readonly name: string;
  readonly x: number;
  readonly z: number;
  readonly y: number;
  readonly main: boolean;
}

/** A map's ground as the map views draw it: a map's one image, '' where none could be drawn, or the folder of a region's baked tiles, `<level>/<x>_<z>.png` under it (large-regions.md section 3). */
export type MapGround =
  | { readonly kind: 'image'; readonly url: string }
  | { readonly kind: 'tiles'; readonly folder: string };

export const NO_GROUND: MapGround = { kind: 'image', url: '' };

/** Where the bodies the map views mark are drawn now. */
export interface MapMarks {
  readonly me: (Spot & { readonly yaw: number }) | null; // yaw from north toward east
  readonly party: readonly (Spot & { readonly id: string; readonly name: string })[];
  readonly lock: Spot | null;
  readonly boss: Spot | null;
}

export interface MapViewState extends MapMarks {
  readonly mapId: string | null;
  readonly channel: number; // the region's channel the player's latest Snapshot named; 0 off regions
  readonly area: string | null; // the region's area the player stands in, as their frames last said; null off regions
  readonly size: readonly [number, number]; // metres east and north
  readonly ground: MapGround;
  readonly waypoints: readonly MapWaypoint[]; // a region's; none elsewhere
  /** A map loaded: its ground and its waypoints, and no marks till the scene places them. */
  showMap(
    mapId: string,
    size: readonly [number, number],
    ground: MapGround,
    waypoints: readonly MapWaypoint[],
  ): void;
  mark(marks: MapMarks): void;
  /** The player's Snapshot named their channel: one per join and map change. */
  inChannel(channel: number): void;
  /** The player's frames said which of the region's areas they stand in, '' for none. */
  inArea(area: string): void;
  clear(): void;
}

const NONE: MapMarks = { me: null, party: [], lock: null, boss: null };

export const mapView = defineStore<MapViewState>((set) => ({
  mapId: null,
  channel: 0,
  area: null,
  size: [0, 0],
  ground: NO_GROUND,
  waypoints: [],
  ...NONE,
  showMap: (mapId, size, ground, waypoints) => {
    set({ mapId, size, ground, waypoints, area: null, ...NONE });
  },
  mark: (marks) => {
    set(marks);
  },
  inChannel: (channel) => {
    set({ channel });
  },
  inArea: (area) => {
    set({ area: area || null });
  },
  clear: () => {
    set({
      mapId: null,
      channel: 0,
      area: null,
      size: [0, 0],
      ground: NO_GROUND,
      waypoints: [],
      ...NONE,
    });
  },
}));
