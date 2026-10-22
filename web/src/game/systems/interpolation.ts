// Interpolation for other players (page 7, Frame loop): their Snapshot and
// Delta states are buffered on the server's tick clock and rendered 100 ms
// in the past, blended between the two nearest states. Plain data, tested
// without Phaser; nothing here goes through a store.
import type { WireState } from './prediction';

export const TICK_MS = 50;
/** How far behind the newest server state remote players are drawn. */
export const RENDER_DELAY_MS = 100;
/** States older than this behind the newest are dropped. */
const KEEP_MS = 1000;

export interface Timed {
  /** Milliseconds on the server's tick clock: tick * TICK_MS. */
  readonly t: number;
  readonly state: WireState;
}

/** The linear blend of two states at a fraction between them; flags come from the nearer one. */
export function blend(a: WireState, b: WireState, f: number): WireState {
  const near = f < 0.5 ? a : b;
  return {
    x: a.x + (b.x - a.x) * f,
    y: a.y + (b.y - a.y) * f,
    vx: a.vx + (b.vx - a.vx) * f,
    vy: a.vy + (b.vy - a.vy) * f,
    facing: near.facing,
    on_ground: near.on_ground,
    climbing: near.climbing,
  };
}

/**
 * The state to draw at renderTime: the first state before the buffer
 * starts, the blend between the two nearest states inside it, and past the
 * last state an extrapolation along its velocity of at most one tick.
 */
export function sample(buffer: readonly Timed[], renderTime: number): WireState | undefined {
  const first = buffer[0];
  if (!first) {
    return undefined;
  }
  if (renderTime <= first.t) {
    return first.state;
  }
  const last = buffer[buffer.length - 1]!;
  if (renderTime >= last.t) {
    const dt = Math.min(renderTime - last.t, TICK_MS) / 1000;
    const s = last.state;
    return { ...s, x: s.x + s.vx * dt, y: s.y + s.vy * dt };
  }
  for (let i = 0; i < buffer.length - 1; i++) {
    const a = buffer[i]!;
    const b = buffer[i + 1]!;
    if (renderTime >= a.t && renderTime < b.t) {
      return blend(a.state, b.state, (renderTime - a.t) / (b.t - a.t));
    }
  }
  return last.state;
}

/** One remote player's recent states, and the tick it was removed on if any. */
export class RemoteBuffer {
  private items: Timed[] = [];
  private removedAt: number | null = null;

  push(t: number, state: WireState): void {
    // Delivered in order; a repeat for the same tick replaces.
    const last = this.items[this.items.length - 1];
    if (last && t <= last.t) {
      if (t === last.t) {
        this.items[this.items.length - 1] = { t, state };
      }
      return;
    }
    this.items.push({ t, state });
    // Keep one state at or before the cutoff as the floor, drop the rest.
    const cutoff = t - KEEP_MS;
    while (this.items.length > 1 && this.items[1]!.t <= cutoff) {
      this.items.shift();
    }
  }

  /** The server said the player left on this tick; it stays drawn until renderTime passes it. */
  remove(t: number): void {
    this.removedAt = t;
  }

  get removed(): boolean {
    return this.removedAt !== null;
  }

  /** True once the render clock has passed the removal tick: nothing left to show. */
  drained(renderTime: number): boolean {
    return this.removedAt !== null && renderTime >= this.removedAt;
  }

  sample(renderTime: number): WireState | undefined {
    return sample(this.items, renderTime);
  }

  get size(): number {
    return this.items.length;
  }
}

export interface RemoteWire extends WireState {
  readonly id: string;
  readonly name: string;
  readonly class: string;
}

/** Everyone but the local player, keyed by character id. */
export class RemotePlayers {
  private readonly buffers = new Map<string, RemoteBuffer>();
  private readonly info = new Map<string, { name: string; cls: string }>();

  constructor(private readonly localId: string) {}

  /** A Snapshot is the whole map: players not in it are gone at once. */
  applySnapshot(t: number, players: readonly RemoteWire[]): void {
    const seen = new Set<string>();
    for (const p of players) {
      if (p.id === this.localId) {
        continue;
      }
      seen.add(p.id);
      this.put(t, p);
    }
    for (const id of [...this.buffers.keys()]) {
      if (!seen.has(id)) {
        this.buffers.delete(id);
        this.info.delete(id);
      }
    }
  }

  applyDelta(t: number, players: readonly RemoteWire[], removed: readonly string[]): void {
    for (const p of players) {
      if (p.id !== this.localId) {
        this.put(t, p);
      }
    }
    for (const id of removed) {
      this.buffers.get(id)?.remove(t);
    }
  }

  private put(t: number, p: RemoteWire): void {
    let b = this.buffers.get(p.id);
    if (!b) {
      b = new RemoteBuffer();
      this.buffers.set(p.id, b);
      this.info.set(p.id, { name: p.name, cls: p.class });
    }
    b.push(t, p);
  }

  /** The states to draw now; a player whose removal has drained is dropped and reported. */
  render(renderTime: number): { shown: Map<string, WireState>; gone: string[] } {
    const shown = new Map<string, WireState>();
    const gone: string[] = [];
    for (const [id, b] of this.buffers) {
      if (b.drained(renderTime)) {
        this.buffers.delete(id);
        this.info.delete(id);
        gone.push(id);
        continue;
      }
      const s = b.sample(renderTime);
      if (s) {
        shown.set(id, s);
      }
    }
    return { shown, gone };
  }

  nameOf(id: string): string {
    return this.info.get(id)?.name ?? '';
  }

  classOf(id: string): string {
    return this.info.get(id)?.cls ?? 'warrior';
  }

  get size(): number {
    return this.buffers.size;
  }

  clear(): void {
    this.buffers.clear();
    this.info.clear();
  }
}

/**
 * The render clock: the newest server tick seen and when it arrived, so
 * the render time advances smoothly between messages and sits
 * RENDER_DELAY_MS behind the server.
 */
export class RenderClock {
  private latestTick = 0;
  private latestAt = 0;
  private started = false;

  /** A Snapshot or Delta for this tick arrived at local time now. */
  update(tick: number, now: number): void {
    if (!this.started || tick > this.latestTick) {
      this.latestTick = tick;
      this.latestAt = now;
      this.started = true;
    }
  }

  /** The server-clock time to draw at local time now, or null before the first message. */
  renderTime(now: number): number | null {
    if (!this.started) {
      return null;
    }
    return this.latestTick * TICK_MS - RENDER_DELAY_MS + (now - this.latestAt);
  }

  reset(): void {
    this.started = false;
    this.latestTick = 0;
    this.latestAt = 0;
  }
}
