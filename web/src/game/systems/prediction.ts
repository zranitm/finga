// Client prediction and reconciliation over plain data (page 7, Frame
// loop): the local player runs sim/step ahead of the server, one Input per
// tick, and each Delta's state for ack_seq is checked against what was
// predicted for that tick. Tested without Phaser.
import { RingBuffer } from '@/lib/ring-buffer';
import type { Input } from '@/sim/input';
import { NO_PLATFORM, params as defaultParams, standing, step } from '@/sim/step';
import type { Geometry, Params, State } from '@/sim/step';

export const STEPS_PER_TICK = 3;
/** A predicted position further than this from the server's is a real disagreement. */
export const RESET_DISTANCE_PX = 4;
/** 2 s of ticks are kept by sequence number. */
export const HISTORY_TICKS = 40;

/** The server's view of a player's movement: sim/step's State without the client-only fields. */
export interface WireState {
  readonly x: number;
  readonly y: number;
  readonly vx: number;
  readonly vy: number;
  readonly on_ground: boolean;
  readonly climbing: boolean;
  readonly facing: 1 | -1;
}

/** One tick: the Input sent for it, how many of its sub-steps have run, and the state after them. */
export interface TickEntry {
  readonly seq: number;
  readonly input: Input;
  steps: number;
  state: State;
}

export type Reconciled = 'match' | 'reset' | 'stale' | 'ahead';

/** The platform index the feet stand on, as sim/step would have recorded it, or NO_PLATFORM on a solid. */
export function platformUnder(x: number, y: number, g: Geometry, p: Params): number {
  for (let i = 0; i < g.platforms.length; i++) {
    const plat = g.platforms[i]!;
    if (y === plat.y && x + p.width / 2 > plat.x && x - p.width / 2 < plat.x + plat.w) {
      return i;
    }
  }
  return NO_PLATFORM;
}

/** A full sim state from the wire: the client-only fields are derived, a drop-through is not in progress. */
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

/** True when the predicted position is further than RESET_DISTANCE_PX from the server's. */
export function differs(predicted: State, server: WireState): boolean {
  return Math.hypot(predicted.x - server.x, predicted.y - server.y) > RESET_DISTANCE_PX;
}

export class Predictor {
  state: State = standing(0, 0);
  readonly history = new RingBuffer<TickEntry>(HISTORY_TICKS);
  private current: TickEntry | null = null;

  constructor(
    private readonly g: Geometry,
    private readonly p: Params = defaultParams,
  ) {}

  /** Takes the server's state as the truth and forgets the history (the first Snapshot, a map change). */
  reset(server: WireState): void {
    this.state = fromWire(server, this.g, this.p);
    this.history.clear();
    this.current = null;
  }

  /** Starts a tick: the Input sent for it, with its sequence number, drives this tick's sub-steps. */
  beginTick(seq: number, input: Input): void {
    this.current = { seq, input, steps: 0, state: this.state };
    this.history.push(this.current);
  }

  /** One 1/60 s sub-step with the current tick's Input; nothing before the first tick. */
  subStep(): void {
    const t = this.current;
    if (!t || t.steps >= STEPS_PER_TICK) {
      return;
    }
    this.state = step(this.state, t.input, this.g, this.p);
    t.steps++;
    t.state = this.state;
  }

  /**
   * Checks the server's state for ack_seq against the prediction for that
   * tick. A match drops the acknowledged entries; a disagreement resets to
   * the server's state and replays the later inputs. An ack older than the
   * history compares nothing; one newer than every entry (a reconnect) takes
   * the server's word.
   */
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
    for (let i = entry.steps; i < STEPS_PER_TICK; i++) {
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

  /** A Correction: the server's state after the input with this sequence number, unconditionally. */
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
