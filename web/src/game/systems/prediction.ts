import type { TickTiming } from '@/lib/fixed-step';
import { RingBuffer } from '@/lib/ring-buffer';
import type { Input } from '@/sim/input';
import { NO_PLATFORM, params as defaultParams, standing, step } from '@/sim/step';
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
}

/** A body as the server sends it: a PlayerView, or a Correction with the current facing. */
export interface ServerState {
  readonly x: number;
  readonly y: number;
  readonly vx: number;
  readonly vy: number;
  readonly onGround: boolean;
  readonly climbing: boolean;
  readonly facing: number; // -1 left, 1 right
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
  };
}

export interface TickEntry {
  readonly seq: number;
  readonly input: Input;
  steps: number;
  state: State;
}

export type Reconciled = 'match' | 'reset' | 'stale' | 'ahead';

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
export function fromWire(w: WireState, g: Geometry, p: Params = defaultParams): State {
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

  /** timing is the world's: its ticks, and the movement steps each holds. */
  constructor(
    private readonly g: Geometry,
    timing: Pick<TickTiming, 'tickMs' | 'stepsPerTick'>,
    private readonly p: Params = defaultParams,
  ) {
    this.stepsPerTick = timing.stepsPerTick;
    this.history = new RingBuffer<TickEntry>(Math.ceil(HISTORY_MS / timing.tickMs));
  }

  reset(server: WireState): void {
    this.state = fromWire(server, this.g, this.p);
    this.history.clear();
    this.current = null;
  }

  beginTick(seq: number, input: Input): void {
    this.current = { seq, input, steps: 0, state: this.state };
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
    return result;
  }

  /** Applies unconditionally. */
  correct(server: WireState, seq: number): void {
    this.replayFrom(server, seq);
    this.history.retain((e) => e.seq > seq);
  }

  private replayFrom(server: WireState, seq: number): void {
    let s = fromWire(server, this.g, this.p);
    for (const e of this.history) {
      if (e.seq <= seq) {
        continue;
      }
      for (let i = 0; i < e.steps; i++) {
        s = step(s, e.input, this.g, this.p);
      }
      e.state = s;
    }
    this.state = s;
  }
}
