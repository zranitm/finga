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
import type { Geometry, Params, Rect, State } from '@/sim/step';

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

/** A pace from the first input past after, as the world switches a buff's move speed at its hit and back at its end. */
export interface Pace {
  readonly after: number;
  readonly params: Params;
}

export interface TickEntry {
  readonly seq: number;
  readonly input: Input;
  /** The tick of the mobs drawn as it was predicted, which the world blocks it against and a replay uses again. */
  readonly view: number;
  /** The marks applied at its start, in order, which a replay applies again. */
  readonly marks: Mark[];
  /** The paces applied at its start, in order. */
  readonly paces: Pace[];
  /** The pace its steps run at, which a replay runs them at again. */
  p: Params;
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

/** Off by more than the tolerance, or casting a different amount, so the pace the client walks at would drift. */
export function differs(predicted: State, server: WireState): boolean {
  return (
    Math.hypot(predicted.x - server.x, predicted.y - server.y) > RESET_DISTANCE_PX ||
    predicted.casting !== server.casting
  );
}

export class Predictor {
  state: State = standing(0, 0);
  readonly history: RingBuffer<TickEntry>;
  private readonly stepsPerTick: number;
  private current: TickEntry | null = null;
  private currentG: Geometry;
  /** Marks waiting for the first input past their seq. */
  private marks: Mark[] = [];
  /** Paces waiting for the first input past their seq, and the pace each applied one replaced. */
  private paces: Pace[] = [];
  private readonly replaced = new WeakMap<Pace, Params>();
  /** The last server state the history was settled on, and the input it acknowledged. */
  private server: { readonly state: WireState; readonly seq: number } | null = null;

  /** timing is the world's: its ticks, and the movement steps each holds; boxesAt is the mobs at a view, which block the player. */
  constructor(
    private readonly g: Geometry,
    timing: Pick<TickTiming, 'tickMs' | 'stepsPerTick'>,
    private p: Params,
    private readonly boxesAt: (view: number) => readonly Rect[],
  ) {
    this.currentG = g;
    this.stepsPerTick = timing.stepsPerTick;
    this.history = new RingBuffer<TickEntry>(Math.ceil(HISTORY_MS / timing.tickMs));
  }

  /** A new move speed applies from the next step; what was predicted before it, the usual reconciliation corrects. */
  setParams(p: Params): void {
    this.p = p;
    if (this.current) {
      this.current.p = p;
    }
  }

  /** Every later input replays from server, and no mark waits: a new map or a rejoin starts with no action. */
  reset(server: WireState): void {
    this.state = fromWire(server, this.g, this.p);
    this.history.clear();
    this.current = null;
    this.marks = [];
    this.paces = [];
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

  /** Sets the pace from the first input past after; dropPace takes it back. */
  pace(after: number, p: Params): Pace {
    const m = { after, params: p };
    this.paces.push(m);
    return m;
  }

  /** Takes back a refused buff's pace: one still waiting goes, and one applied gives its ticks and those since the pace it replaced, replayed from the last server state. */
  dropPace(m: Pace): void {
    if (this.paces.includes(m)) {
      this.paces = this.paces.filter((k) => k !== m);
      return;
    }
    const prior = this.replaced.get(m);
    if (!prior) {
      return;
    }
    let since = false;
    for (const e of this.history) {
      const i = e.paces.indexOf(m);
      if (i >= 0) {
        e.paces.splice(i, 1);
        since = true;
      }
      if (since && e.p === m.params) {
        e.p = prior;
      }
    }
    if (this.p === m.params) {
      this.p = prior;
    }
    if (this.server) {
      this.replayFrom(this.server.state, this.server.seq);
    }
  }

  beginTick(seq: number, input: Input, view: number): void {
    const due = this.marks.filter((m) => m.after < seq);
    if (due.length > 0) {
      this.marks = this.marks.filter((m) => m.after >= seq);
      this.state = withMarks(this.state, due);
    }
    const paced = this.paces.filter((m) => m.after < seq);
    if (paced.length > 0) {
      this.paces = this.paces.filter((m) => m.after >= seq);
      for (const m of paced) {
        this.replaced.set(m, this.p);
        this.p = m.params;
      }
    }
    this.current = {
      seq,
      input,
      view,
      marks: due,
      paces: paced,
      p: this.p,
      steps: 0,
      state: this.state,
    };
    this.currentG = this.at(view);
    this.history.push(this.current);
  }

  subStep(): void {
    const t = this.current;
    if (!t || t.steps >= this.stepsPerTick) {
      return;
    }
    this.state = step(this.state, t.input, this.currentG, t.p);
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
    const g = this.at(entry.view);
    for (let i = entry.steps; i < this.stepsPerTick; i++) {
      predicted = step(predicted, entry.input, g, entry.p);
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

  /** The map with the mobs at view blocking. */
  private at(view: number): Geometry {
    const blockers = this.boxesAt(view);
    return blockers.length === 0 ? this.g : { ...this.g, blockers };
  }

  private replayFrom(server: WireState, seq: number): void {
    let s = fromWire(server, this.g, this.p);
    for (const e of this.history) {
      if (e.seq <= seq) {
        continue;
      }
      s = withMarks(s, e.marks);
      const g = this.at(e.view);
      for (let i = 0; i < e.steps; i++) {
        s = step(s, e.input, g, e.p);
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
