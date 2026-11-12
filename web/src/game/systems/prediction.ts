import type { TickTiming } from '@/lib/fixed-step';
import { RingBuffer } from '@/lib/ring-buffer';
import type { Input } from '@/sim/input';
import type { MapKind } from '@/sim/map';
import {
  NO_PLATFORM,
  params as defaultParams,
  faster,
  speedFactor,
  standing,
  step,
} from '@/sim/step';
import type { Geometry, Params, State } from '@/sim/step';

export const RESET_DISTANCE_PX = 4;
/** How far back unacknowledged ticks are kept, at any tick rate. */
export const HISTORY_MS = 2000;

/** sim/step's State without the client-only fields. */
export interface WireState {
  readonly x: number;
  readonly y: number;
  readonly vx: number;
  readonly vy: number;
  readonly on_ground: boolean;
  readonly climbing: boolean;
  readonly facing: 1 | -1;
  readonly casting: number;
}

/** A body as the server sends it, in a PlayerView. */
export interface ServerState {
  readonly x: number;
  readonly y: number;
  readonly vx: number;
  readonly vy: number;
  readonly onGround: boolean;
  readonly climbing: boolean;
  readonly facing: number; // -1 left, 1 right
  readonly casting: number; // sub-steps of casting left, which only the player's own client predicts
}

export function wireState(s: ServerState): WireState {
  return {
    x: s.x,
    y: s.y,
    vx: s.vx,
    vy: s.vy,
    on_ground: s.onGround,
    climbing: s.climbing,
    facing: s.facing < 0 ? -1 : 1,
    casting: s.casting,
  };
}

/** Casting set from the first input past after, as the server sets it: a skill's length in sub-steps as it starts, or 0 as an attack cuts it short. */
export interface Mark {
  readonly after: number;
  readonly casting: number;
}

export interface TickEntry {
  readonly seq: number;
  readonly input: Input;
  /** The marks applied at its start, in order, which a replay applies again. */
  readonly marks: Mark[];
  steps: number;
  state: State;
}

export type Reconciled = 'match' | 'reset' | 'stale' | 'ahead';

/** The movement a player predicts with: its move speed in hunting maps, and everyone's one pace in towns, as the world moves it. */
export function movementParams(kind: MapKind, moveSpeed: number): Params {
  return kind === 'hunting' ? faster(defaultParams, speedFactor(moveSpeed)) : defaultParams;
}

/** The platform index the feet stand on, as sim/step would record it. */
export function platformUnder(x: number, y: number, g: Geometry, p: Params): number {
  for (let i = 0; i < g.platforms.length; i++) {
    const plat = g.platforms[i]!;
    if (y === plat.y && x + p.width / 2 > plat.x && x - p.width / 2 < plat.x + plat.w) {
      return i;
    }
  }
  return NO_PLATFORM;
}

/** Derives the client-only fields; assumes no drop-through in progress. */
export function fromWire(w: WireState, g: Geometry, p: Params): State {
  return {
    x: w.x,
    y: w.y,
    vx: w.vx,
    vy: w.vy,
    on_ground: w.on_ground,
    climbing: w.climbing,
    facing: w.facing,
    ground: w.on_ground ? platformUnder(w.x, w.y, g, p) : NO_PLATFORM,
    drop_platform: NO_PLATFORM,
    drop_timer: 0,
    casting: w.casting,
  };
}

export function differs(predicted: State, server: WireState): boolean {
  return Math.hypot(predicted.x - server.x, predicted.y - server.y) > RESET_DISTANCE_PX;
}

export class Predictor {
  state: State = standing(0, 0);
  readonly history: RingBuffer<TickEntry>;
  private readonly stepsPerTick: number;
  private current: TickEntry | null = null;
  /** Marks waiting for the first input past their seq. */
  private marks: Mark[] = [];
  /** The last server state the history was settled on, and the input it acknowledged. */
  private server: { readonly state: WireState; readonly seq: number } | null = null;

  /** timing is the world's: its ticks, and the movement steps each holds. */
  constructor(
    private readonly g: Geometry,
    timing: Pick<TickTiming, 'tickMs' | 'stepsPerTick'>,
    private p: Params,
  ) {
    this.stepsPerTick = timing.stepsPerTick;
    this.history = new RingBuffer<TickEntry>(Math.ceil(HISTORY_MS / timing.tickMs));
  }

  /** A new move speed applies from the next step; what was predicted before it, the usual reconciliation corrects. */
  setParams(p: Params): void {
    this.p = p;
  }

  /** Every later input replays from server, and no mark waits: a new map or a rejoin starts with no action. */
  reset(server: WireState): void {
    this.state = fromWire(server, this.g, this.p);
    this.history.clear();
    this.current = null;
    this.marks = [];
    this.server = { state: server, seq: -1 };
  }

  /** Sets the casting from the first input past after; drop takes it back. */
  mark(after: number, casting: number): Mark {
    const m = { after, casting };
    this.marks.push(m);
    return m;
  }

  /** Takes back a refused skill's mark: one still waiting goes, and one applied leaves its tick and the ticks since replay from the last server state. */
  drop(m: Mark): void {
    if (this.marks.includes(m)) {
      this.marks = this.marks.filter((k) => k !== m);
      return;
    }
    for (const e of this.history) {
      const i = e.marks.indexOf(m);
      if (i >= 0) {
        e.marks.splice(i, 1);
      }
    }
    if (this.server) {
      this.replayFrom(this.server.state, this.server.seq);
    }
  }

  beginTick(seq: number, input: Input): void {
    const due = this.marks.filter((m) => m.after < seq);
    if (due.length > 0) {
      this.marks = this.marks.filter((m) => m.after >= seq);
      this.state = withMarks(this.state, due);
    }
    this.current = { seq, input, marks: due, steps: 0, state: this.state };
    this.history.push(this.current);
  }

  subStep(): void {
    const t = this.current;
    if (!t || t.steps >= this.stepsPerTick) {
      return;
    }
    this.state = step(this.state, t.input, this.g, this.p);
    t.steps++;
    t.state = this.state;
  }

  /** On a disagreement, resets to the server's state and replays the later inputs. */
  reconcile(server: WireState, ackSeq: number): Reconciled {
    const entry = this.history.find((e) => e.seq, ackSeq);
    if (!entry) {
      const newest = this.history.last();
      if (newest && ackSeq > newest.seq) {
        this.reset(server);
        return 'ahead';
      }
      return 'stale';
    }
    // The server has run the whole tick; the client may be part way through it.
    let predicted = entry.state;
    for (let i = entry.steps; i < this.stepsPerTick; i++) {
      predicted = step(predicted, entry.input, this.g, this.p);
    }
    let result: Reconciled = 'match';
    if (differs(predicted, server)) {
      this.replayFrom(server, ackSeq);
      result = 'reset';
    }
    this.history.retain((e) => e.seq > ackSeq);
    this.server = { state: server, seq: ackSeq };
    return result;
  }

  private replayFrom(server: WireState, seq: number): void {
    let s = fromWire(server, this.g, this.p);
    for (const e of this.history) {
      if (e.seq <= seq) {
        continue;
      }
      s = withMarks(s, e.marks);
      for (let i = 0; i < e.steps; i++) {
        s = step(s, e.input, this.g, this.p);
      }
      e.state = s;
    }
    this.state = s;
  }
}

/** s with the casting the last of marks sets, or as it was with none. */
function withMarks(s: State, marks: readonly Mark[]): State {
  const last = marks.at(-1);
  return last ? { ...s, casting: last.casting } : s;
}
