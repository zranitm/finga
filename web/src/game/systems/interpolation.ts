// Remote players are drawn behind the newest server state, blended between buffered states.
import { classes } from '@/gamedata/classes';

import { wireState } from './prediction';
import type { ServerState, WireState } from './prediction';

const KEEP_MS = 1000;
const MIN_DELAY_MS = 100; // at 30 Hz and up, two ticks are shorter than ordinary network jitter

export interface Timed {
  /** Server tick clock: tick * the tick's length in ms. */
  readonly t: number;
  readonly state: WireState;
}

/** Flags come from the nearer state. */
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
    casting: near.casting,
  };
}

/** Past the last state, extrapolates along its velocity for at most one tick of tickMs. */
export function sample(
  buffer: readonly Timed[],
  renderTime: number,
  tickMs: number,
): WireState | undefined {
  const first = buffer[0];
  if (!first) {
    return undefined;
  }
  if (renderTime <= first.t) {
    return first.state;
  }
  const last = buffer[buffer.length - 1]!;
  if (renderTime >= last.t) {
    const dt = Math.min(renderTime - last.t, tickMs) / 1000;
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

export class RemoteBuffer {
  private items: Timed[] = [];
  private removedAt: number | null = null;

  constructor(private readonly tickMs: number) {}

  push(t: number, state: WireState): void {
    const last = this.items[this.items.length - 1];
    if (last && t <= last.t) {
      if (t === last.t) {
        this.items[this.items.length - 1] = { t, state };
      }
      return;
    }
    this.items.push({ t, state });
    // Keep one state at or before the cutoff as the floor.
    const cutoff = t - KEEP_MS;
    while (this.items.length > 1 && this.items[1]!.t <= cutoff) {
      this.items.shift();
    }
  }

  /** The player stays drawn until renderTime passes t. */
  remove(t: number): void {
    this.removedAt = t;
  }

  get removed(): boolean {
    return this.removedAt !== null;
  }

  drained(renderTime: number): boolean {
    return this.removedAt !== null && renderTime >= this.removedAt;
  }

  sample(renderTime: number): WireState | undefined {
    return sample(this.items, renderTime, this.tickMs);
  }

  get size(): number {
    return this.items.length;
  }
}

export interface RemoteWire extends ServerState {
  readonly id: string;
  readonly name: string;
  readonly class: string;
}

export class RemotePlayers {
  private readonly buffers = new Map<string, RemoteBuffer>();
  private readonly info = new Map<string, { name: string; cls: string }>();

  constructor(
    private readonly localId: string,
    private readonly tickMs: number,
  ) {}

  /** Players missing from a Snapshot are gone at once. */
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
      b = new RemoteBuffer(this.tickMs);
      this.buffers.set(p.id, b);
    }
    // A promotion changes the class mid-stay.
    const info = this.info.get(p.id);
    if (info?.cls !== p.class || info.name !== p.name) {
      this.info.set(p.id, { name: p.name, cls: p.class });
    }
    b.push(t, wireState(p));
  }

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
    return this.info.get(id)?.cls ?? classes[0].id;
  }

  get size(): number {
    return this.buffers.size;
  }

  clear(): void {
    this.buffers.clear();
    this.info.clear();
  }
}

/** Advances smoothly between messages, behind the newest tick seen by two ticks or 100 ms, whichever is longer. */
export class RenderClock {
  private firstTick = 0;
  private latestTick = 0;
  private latestAt = 0;
  private started = false;
  private readonly delayMs: number;

  constructor(private readonly tickMs: number) {
    this.delayMs = Math.max(MIN_DELAY_MS, 2 * tickMs);
  }

  update(tick: number, now: number): void {
    if (!this.started) {
      this.firstTick = tick;
    }
    if (!this.started || tick > this.latestTick) {
      this.latestTick = tick;
      this.latestAt = now;
      this.started = true;
    }
  }

  renderTime(now: number): number | null {
    if (!this.started) {
      return null;
    }
    return this.latestTick * this.tickMs - this.delayMs + (now - this.latestAt);
  }

  /** The tick drawn at now, held between the first and the newest received; 0 before any. */
  viewAt(now: number): number {
    const t = this.renderTime(now);
    return t === null
      ? 0
      : Math.max(this.firstTick, Math.min(this.latestTick, Math.floor(t / this.tickMs)));
  }

  reset(): void {
    this.started = false;
    this.firstTick = 0;
    this.latestTick = 0;
    this.latestAt = 0;
  }
}
