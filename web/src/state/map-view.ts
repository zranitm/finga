// What the map views draw that only the world scene knows (map-views.md sections 4 and 5): the ground's image, made as a map loads, and where the player, the party, the lock and the field boss are drawn, set about ten times a second.
import { defineStore } from './store';

/** A place on the map: x east and z north, in metres. */
export interface Spot {
  readonly x: number;
  readonly z: number;
}

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
  readonly size: readonly [number, number]; // metres east and north
  readonly image: string; // the ground's image URL; '' where none could be drawn
  /** A map loaded: its ground's image, and no marks till the scene places them. */
  showMap(mapId: string, size: readonly [number, number], image: string): void;
  mark(marks: MapMarks): void;
  /** The player's Snapshot named their channel: one per join and map change. */
  inChannel(channel: number): void;
  clear(): void;
}

const NONE: MapMarks = { me: null, party: [], lock: null, boss: null };

export const mapView = defineStore<MapViewState>((set) => ({
  mapId: null,
  channel: 0,
  size: [0, 0],
  image: '',
  ...NONE,
  showMap: (mapId, size, image) => {
    set({ mapId, size, image, ...NONE });
  },
  mark: (marks) => {
    set(marks);
  },
  inChannel: (channel) => {
    set({ channel });
  },
  clear: () => {
    set({ mapId: null, channel: 0, size: [0, 0], image: '', ...NONE });
  },
}));
