// The player's own body, stepped ahead of the world on the sim it runs, and set right from each Delta's state for its last input.
import { movement } from '@/gamedata/movement';
import type { TickTiming } from '@/lib/fixed-step';
import { RingBuffer } from '@/lib/ring-buffer';
import type { Input } from '@/sim/input';
import { faster, speedFactor, step } from '@/sim/step';
import type { Blocker, Geometry, Params, State } from '@/sim/step';

/** How far apart, in metres, the predicted and the world's body may be before the prediction resets: 4 px of the 2D game. */
export const RESET_DISTANCE_M = 0.125;
/** How far back unacknowledged ticks are kept, at any tick rate. */
export const HISTORY_MS = 2000;

/** A body as the server sends it, in a PlayerView. */
export interface ServerState {
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly vx: number;
  readonly vy: number;
  readonly vz: number;
  readonly onGround: boolean;
  readonly hx: number;
  readonly hz: number;
}

export function wireState(s: ServerState): State {
  return {
    x: s.x,
    y: s.y,
    z: s.z,
    vx: s.vx,
    vy: s.vy,
    vz: s.vz,
    on_ground: s.onGround,
    hx: s.hx,
    hz: s.hz,
  };
}

/** A pace from the first input past after, as the world switches a buff's move speed at its hit and back at its end; params is read as it applies, so it follows a sheet that came since. */
export interface Pace {
  readonly after: number;
  readonly params: () => Params;
}

export interface TickEntry {
  readonly seq: number;
  readonly input: Input;
  /** The tick of the mobs drawn as it was predicted, which the world blocks it against and a replay uses again. */
  readonly view: number;
  /** The paces applied at its start, in order. */
  readonly paces: Pace[];
  /** The pace its steps run at, which a replay runs them at again. */
  p: Params;
  steps: number;
  state: State;
}

export type Reconciled = 'match' | 'reset' | 'stale' | 'ahead';

/** The movement a player predicts with: its move speed in hunting maps, and everyone's one pace in towns, as the world moves it. */
export function movementParams(kind: 'town' | 'hunting', moveSpeed: number): Params {
  return kind === 'hunting' ? faster(movement, speedFactor(moveSpeed)) : movement;
}

/** Off by more than the tolerance. */
function differs(predicted: State, server: State): boolean {
  return (
    Math.hypot(predicted.x - server.x, predicted.y - server.y, predicted.z - server.z) >
    RESET_DISTANCE_M
  );
}

export class Predictor {
  state: State;
  readonly history: RingBuffer<TickEntry>;
  private readonly stepsPerTick: number;
  private current: TickEntry | null = null;
  private currentG: Geometry;
  /** Paces waiting for the first input past their seq, and for each applied one, the params it set and those it replaced. */
  private paces: Pace[] = [];
  private readonly applied = new WeakMap<
    Pace,
    { readonly set: Params; readonly replaced: Params }
  >();
  /** The last server state the history was settled on, and the input it acknowledged. */
  private server: { readonly state: State; readonly seq: number } | null = null;

  /** timing is the world's: its ticks, and the movement steps each holds; blockersAt is the mobs at a view, which block the player. */
  constructor(
    private readonly g: Geometry,
    timing: Pick<TickTiming, 'tickMs' | 'stepsPerTick'>,
    private p: Params,
    private readonly blockersAt: (view: number) => readonly Blocker[],
  ) {
    this.currentG = g;
    this.stepsPerTick = timing.stepsPerTick;
    this.history = new RingBuffer<TickEntry>(Math.ceil(HISTORY_MS / timing.tickMs));
    this.state = { x: 0, y: 0, z: 0, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 0, hz: 1 };
  }

  /** A new move speed applies from the next step; what was predicted before it, the usual reconciliation corrects. */
  setParams(p: Params): void {
    this.p = p;
    if (this.current) {
      this.current.p = p;
    }
  }

  /** Every later input replays from server, and no pace waits: a new map or a rejoin starts afresh. */
  reset(server: State): void {
    this.state = server;
    this.history.clear();
    this.current = null;
    this.paces = [];
    this.server = { state: server, seq: -1 };
  }

  /** Sets the pace from the first input past after, reading params then; dropPace takes it back. */
  pace(after: number, params: () => Params): Pace {
    const m = { after, params };
    this.paces.push(m);
    return m;
  }

  /** Takes back a refused buff's pace: one still waiting goes, and one applied gives its ticks and those since the pace it replaced, replayed from the last server state. */
  dropPace(m: Pace): void {
    if (this.paces.includes(m)) {
      this.paces = this.paces.filter((k) => k !== m);
      return;
    }
    const a = this.applied.get(m);
    if (!a) {
      return;
    }
    let since = false;
    for (const e of this.history) {
      const i = e.paces.indexOf(m);
      if (i >= 0) {
        e.paces.splice(i, 1);
        since = true;
      }
      if (since && e.p === a.set) {
        e.p = a.replaced;
      }
    }
    if (this.p === a.set) {
      this.p = a.replaced;
    }
    if (this.server) {
      this.replayFrom(this.server.state, this.server.seq);
    }
  }

  beginTick(seq: number, input: Input, view: number): void {
    const paced = this.paces.filter((m) => m.after < seq);
    if (paced.length > 0) {
      this.paces = this.paces.filter((m) => m.after >= seq);
      for (const m of paced) {
        const set = m.params();
        this.applied.set(m, { set, replaced: this.p });
        this.p = set;
      }
    }
    this.current = {
      seq,
      input,
      view,
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
  reconcile(server: State, ackSeq: number): Reconciled {
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
    const blockers = this.blockersAt(view);
    return blockers.length === 0 ? this.g : { ...this.g, blockers };
  }

  private replayFrom(server: State, seq: number): void {
    let s = server;
    for (const e of this.history) {
      if (e.seq <= seq) {
        continue;
      }
      const g = this.at(e.view);
      for (let i = 0; i < e.steps; i++) {
        s = step(s, e.input, g, e.p);
      }
      e.state = s;
    }
    this.state = s;
  }
}
