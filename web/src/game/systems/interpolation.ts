// Remote players and mobs are drawn behind the newest server state, blended between buffered states.
import { classes } from '@/gamedata/classes';
import type { StatusOn } from '@/progression/statuses';
import type { Blocker, State } from '@/sim/step';

import { wireState } from './prediction';
import type { ServerState } from './prediction';
import { relationOf } from './teams';

const KEEP_MS = 1000;
const MIN_DELAY_MS = 100; // at 30 Hz and up, two ticks are shorter than ordinary network jitter

export interface Timed<S = State> {
  /** Server tick clock: tick * the tick's length in ms. */
  readonly t: number;
  readonly state: S;
}

/** How a buffer blends two of its states, and carries its last one ahead by up to a tick. */
export interface Mix<S> {
  blend(a: S, b: S, f: number): S;
  ahead(s: S, ms: number): S;
}

/** The ground flag, the heading and the sprint's count come from the nearer state. */
export function blend(a: State, b: State, f: number): State {
  const near = f < 0.5 ? a : b;
  return {
    ...(near.sprint && { sprint: near.sprint }),
    x: a.x + (b.x - a.x) * f,
    y: a.y + (b.y - a.y) * f,
    z: a.z + (b.z - a.z) * f,
    vx: a.vx + (b.vx - a.vx) * f,
    vy: a.vy + (b.vy - a.vy) * f,
    vz: a.vz + (b.vz - a.vz) * f,
    on_ground: near.on_ground,
    hx: near.hx,
    hz: near.hz,
  };
}

/** A body past its last state goes on along its velocity. */
export const bodies: Mix<State> = {
  blend,
  ahead: (s, ms) => ({
    ...s,
    x: s.x + (s.vx * ms) / 1000,
    y: s.y + (s.vy * ms) / 1000,
    z: s.z + (s.vz * ms) / 1000,
  }),
};

/** Past the last state, carries it ahead for at most one tick of tickMs. */
export function sampleWith<S>(
  buffer: readonly Timed<S>[],
  renderTime: number,
  tickMs: number,
  mix: Mix<S>,
): S | undefined {
  const first = buffer[0];
  if (!first) {
    return undefined;
  }
  if (renderTime <= first.t) {
    return first.state;
  }
  const last = buffer[buffer.length - 1]!;
  if (renderTime >= last.t) {
    return mix.ahead(last.state, Math.min(renderTime - last.t, tickMs));
  }
  for (let i = 0; i < buffer.length - 1; i++) {
    const a = buffer[i]!;
    const b = buffer[i + 1]!;
    if (renderTime >= a.t && renderTime < b.t) {
      return mix.blend(a.state, b.state, (renderTime - a.t) / (b.t - a.t));
    }
  }
  return last.state;
}

/** Past the last state, extrapolates along its velocity for at most one tick of tickMs. */
export function sample(
  buffer: readonly Timed[],
  renderTime: number,
  tickMs: number,
): State | undefined {
  return sampleWith(buffer, renderTime, tickMs, bodies);
}

/** One body's states by server time, drawn at a render time behind the newest, and kept until a removal is drawn past. */
export class FrameBuffer<S> {
  private items: Timed<S>[] = [];
  private removedAt: number | null = null;

  constructor(
    private readonly tickMs: number,
    private readonly mix: Mix<S>,
  ) {}

  push(t: number, state: S): void {
    if (this.removedAt !== null && t > this.removedAt) {
      this.removedAt = null; // back on the map, or in sight, before its removal was drawn past
    }
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

  drained(renderTime: number): boolean {
    return this.removedAt !== null && renderTime >= this.removedAt;
  }

  sample(renderTime: number): S | undefined {
    return sampleWith(this.items, renderTime, this.tickMs, this.mix);
  }

  /** The newest state at or before t as its frame sent it, not blended; none once the body's removal is at or before t. */
  heldAt(t: number): S | undefined {
    if (this.removedAt !== null && this.removedAt <= t) {
      return undefined;
    }
    return this.items.findLast((i) => i.t <= t)?.state;
  }
}

/** A remote player's buffer. */
export class RemoteBuffer extends FrameBuffer<State> {
  constructor(tickMs: number) {
    super(tickMs, bodies);
  }
}

/** A player's look as their view carries it: each slot's piece id, empty for the class's own or no wings, and whether the head is bare. */
export interface LookWire {
  readonly outfit: string;
  readonly headwear: string;
  readonly face: string;
  readonly weapon: string;
  readonly wings: string;
  readonly hideHeadwear: boolean;
}

export interface RemoteWire extends ServerState {
  readonly id: string;
  readonly name: string;
  readonly class: string;
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  /** Its side in a match, 1 or 2; 0 outside one, where no player is anyone's enemy. */
  readonly team: number;
  /** A bot fighting in a match, which no one plays. */
  readonly bot: boolean;
  readonly statuses: readonly StatusOn[];
  readonly dodging: boolean;
  readonly shield: number;
  /** What it wears over its class's look; none, as proto3 sends an empty one, is the class's own. */
  readonly look?: LookWire | undefined;
  /** Down in a match, as of the frame. */
  readonly dead?: boolean | undefined;
}

/** What a remote player shows besides where it stands, as of a frame. */
export interface RemoteLook {
  readonly dodging: boolean;
  /** Down in a match: no body to block. */
  readonly dead: boolean;
  /** What's left of their shields. */
  readonly shield: number;
}

/** Who a remote player is and how they stand, as of the newest frame. */
export interface RemoteInfo {
  readonly name: string;
  readonly cls: string;
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  readonly team: number;
  readonly bot: boolean;
  readonly look: LookWire | undefined;
}

/** A look is on or off: it holds from its frame to the next, and isn't blended. */
const steps: Mix<RemoteLook> = { blend: (a) => a, ahead: (s) => s };

export class RemotePlayers {
  private readonly buffers = new Map<string, RemoteBuffer>();
  private readonly looks = new Map<string, FrameBuffer<RemoteLook>>();
  private readonly info = new Map<string, RemoteInfo>();
  private readonly statuses = new Map<string, readonly StatusOn[]>();

  constructor(
    private readonly localId: string,
    private readonly tickMs: number,
  ) {}

  /** A whole map starts over: players missing from it are gone at once, and a new instance's ticks begin afresh. */
  applySnapshot(t: number, players: readonly RemoteWire[]): void {
    this.clear();
    for (const p of players) {
      if (p.id !== this.localId) {
        this.put(t, p);
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
      this.looks.get(id)?.remove(t);
    }
  }

  private put(t: number, p: RemoteWire): void {
    let b = this.buffers.get(p.id);
    if (!b) {
      b = new RemoteBuffer(this.tickMs);
      this.buffers.set(p.id, b);
    }
    this.info.set(p.id, {
      name: p.name,
      cls: p.class, // a promotion changes it mid-stay
      level: p.level,
      hp: p.hp,
      maxHp: p.maxHp,
      team: p.team,
      bot: p.bot,
      look: p.look, // a wardrobe change mid-stay
    });
    this.statuses.set(p.id, p.statuses);
    b.push(t, wireState(p));
    let l = this.looks.get(p.id);
    if (!l) {
      l = new FrameBuffer(this.tickMs, steps);
      this.looks.set(p.id, l);
    }
    l.push(t, { dodging: p.dodging, shield: p.shield, dead: p.dead ?? false });
  }

  /** Each player drawn at renderTime, and how they look then; gone are the ones whose removal it has passed. */
  render(renderTime: number): {
    shown: Map<string, State>;
    looks: Map<string, RemoteLook>;
    gone: string[];
  } {
    const shown = new Map<string, State>();
    const looks = new Map<string, RemoteLook>();
    const gone: string[] = [];
    for (const [id, b] of this.buffers) {
      if (b.drained(renderTime)) {
        this.buffers.delete(id);
        this.looks.delete(id);
        this.info.delete(id);
        this.statuses.delete(id);
        gone.push(id);
        continue;
      }
      const s = b.sample(renderTime);
      if (s) {
        shown.set(id, s);
      }
      const l = this.looks.get(id)?.sample(renderTime);
      if (l) {
        looks.set(id, l);
      }
    }
    return { shown, looks, gone };
  }

  /**
   * The living enemies' circles as view's frame left them, in id order as the world keeps them, which it blocks inputs at view against (pvp.md section 19):
   * from each player's tick states, never the smoothed draw. Teammates and everyone outside a match, where every team is 0, are no one's body.
   */
  foesAt(view: number, myTeam: number, radius: number, height: number): Blocker[] {
    const t = view * this.tickMs;
    const foes: { id: string; blocker: Blocker }[] = [];
    for (const [id, b] of this.buffers) {
      if (relationOf(myTeam, this.info.get(id)?.team ?? 0) !== 'foe') {
        continue;
      }
      const s = b.heldAt(t);
      if (!s || this.looks.get(id)?.heldAt(t)?.dead) {
        continue;
      }
      foes.push({ id, blocker: { x: s.x, z: s.z, radius, bottom: s.y, top: s.y + height } });
    }
    return foes.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)).map((f) => f.blocker);
  }

  /** Who a player is as of the newest frame, until they're gone. */
  infoOf(id: string): RemoteInfo | undefined {
    return this.info.get(id);
  }

  nameOf(id: string): string {
    return this.info.get(id)?.name ?? '';
  }

  classOf(id: string): string {
    return this.info.get(id)?.cls ?? classes[0].id;
  }

  /** What a player wears, as of the newest frame. */
  lookOf(id: string): LookWire | undefined {
    return this.info.get(id)?.look;
  }

  /** A player's statuses as of the newest frame, as the world judges them. */
  statusesOf(id: string): readonly StatusOn[] {
    return this.statuses.get(id) ?? [];
  }

  clear(): void {
    this.buffers.clear();
    this.looks.clear();
    this.info.clear();
    this.statuses.clear();
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
