import type { Received } from '@/net/socket';

/** The messages a map's scene applies, held while the next map's scene is not built yet. */
export type Held =
  Received<'snapshot'> | Received<'delta'> | Received<'died'> | Received<'respawned'>;

/** What an Arrival needs of the socket. */
export interface Feed {
  on<T extends Held['t'] | 'welcome' | 'mapChange'>(
    t: T,
    handler: (msg: Received<T>) => void,
  ): () => void;
}

export interface Spot {
  readonly x: number;
  readonly y: number;
}

/** A map being entered: from the moment the client learns of it until its scene is built, its messages held in order, as a scene not yet built would drop the Snapshot that places the player. */
export class Arrival {
  private held: Held[] = [];
  private offs: (() => void)[];
  private map: string;
  private rate: number;
  private spot: Spot | undefined;

  constructor(feed: Feed, mapId: string, tickRateHz: number, at?: Spot) {
    this.map = mapId;
    this.rate = tickRateHz;
    this.spot = at;
    this.offs = [
      feed.on('snapshot', (m) => this.hold(m)),
      feed.on('delta', (m) => this.hold(m)),
      feed.on('died', (m) => this.hold(m)),
      feed.on('respawned', (m) => this.hold(m)),
      feed.on('mapChange', (m) => {
        this.held = []; // another map after all: its own Snapshot follows
        this.map = m.d.mapId;
        this.spot = { x: m.d.x, y: m.d.y };
      }),
      feed.on('welcome', (m) => {
        this.held = []; // a reconnect: its own Snapshot follows
        this.map = m.d.mapId;
        this.rate = m.d.tickRateHz;
        this.spot = undefined;
      }),
    ];
  }

  /** The map to build: the latest a MapChange or a Welcome named. */
  get mapId(): string {
    return this.map;
  }

  get tickRateHz(): number {
    return this.rate;
  }

  /** Where to show the player until the Snapshot: a MapChange's spot; none after a Welcome, which says nothing of it. */
  get at(): Spot | undefined {
    return this.spot;
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
