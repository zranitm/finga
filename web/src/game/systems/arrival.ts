import type { Received } from '@/net/socket';

/** The messages a map's scene applies, held while the next map's scene is not built yet. */
export type Held =
  | Received<'snapshot'>
  | Received<'delta'>
  | Received<'died'>
  | Received<'respawned'>
  | Received<'mapChange'>;

type HeldType = Held['t'];

/** What an Arrival needs of the socket. */
export interface Feed {
  on<T extends HeldType | 'welcome'>(t: T, handler: (msg: Received<T>) => void): () => void;
}

/** A map being entered: from its MapChange until its scene is built, its messages held in order, as the restart would drop the Snapshot that places the player. */
export class Arrival {
  private held: Held[] = [];
  private offs: (() => void)[];
  private map: string;
  private rate: number;
  private reconnected = false;

  constructor(feed: Feed, mapId: string, tickRateHz: number) {
    this.map = mapId;
    this.rate = tickRateHz;
    this.offs = [
      feed.on('snapshot', (m) => this.hold(m)),
      feed.on('delta', (m) => this.hold(m)),
      feed.on('died', (m) => this.hold(m)),
      feed.on('respawned', (m) => this.hold(m)),
      feed.on('mapChange', (m) => this.hold(m)),
      feed.on('welcome', (m) => {
        this.held = []; // a reconnect: its own Snapshot follows
        this.map = m.d.mapId;
        this.rate = m.d.tickRateHz;
        this.reconnected = true;
      }),
    ];
  }

  /** The map to build: the MapChange's, or a reconnect's since. */
  get mapId(): string {
    return this.map;
  }

  get tickRateHz(): number {
    return this.rate;
  }

  /** Whether a reconnect since put the player somewhere the MapChange did not say. */
  get moved(): boolean {
    return this.reconnected;
  }

  /** Stops holding, and hands over what was held, oldest first; later calls hand over nothing. */
  take(): readonly Held[] {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
    const held = this.held;
    this.held = [];
    return held;
  }

  /** A Snapshot starts the new map afresh; anything before the first one is the old map's. */
  private hold(m: Held): void {
    if (m.t === 'snapshot') {
      this.held = [m];
    } else if (this.held.length > 0) {
      this.held.push(m);
    }
  }
}
