// The player's own body, stepped ahead of the world on the sim it runs, and set right from each Delta's state for its last input.
import { movement } from '@/gamedata/movement';
import type { TickTiming } from '@/lib/fixed-step';
import { RingBuffer } from '@/lib/ring-buffer';
import type { Input } from '@/sim/input';
import { faster, speedFactor, step } from '@/sim/step';
import type { Blocker, Geometry, Params, State } from '@/sim/step';

/** How far back unacknowledged ticks are kept, at any tick rate. */
export const HISTORY_MS = 2000;
/** The frames of its mobs the world keeps (mobs.History), which bound the view an input blocks at. */
const KEPT_FRAMES = 8;

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

interface TickEntry {
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

/** The movement a player predicts with: its move speed in hunting maps, and everyone's one pace in towns, as the world moves it. */
export function movementParams(kind: 'town' | 'hunting', moveSpeed: number): Params {
  return kind === 'hunting' ? faster(movement, speedFactor(moveSpeed)) : movement;
}

/** The same body to the bit: the sims run alike, so any difference is the world's own, a push or a blocker the client didn't see. */
function same(a: State, b: State): boolean {
  return (
    a.x === b.x &&
    a.y === b.y &&
    a.z === b.z &&
    a.vx === b.vx &&
    a.vy === b.vy &&
    a.vz === b.vz &&
    a.on_ground === b.on_ground &&
    a.hx === b.hx &&
    a.hz === b.hz
  );
}

export class Predictor {
  state: State;
  /** How often the world was found elsewhere and the prediction replayed from it. */
  resets = 0;
  private readonly history: RingBuffer<TickEntry>;
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
  /** The last input acknowledged and the tick it ran at, from which the world runs one input a tick. */
  private ack: { readonly seq: number; readonly tick: number } | null = null;

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
    this.ack = null;
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
    this.currentG = this.at(this.current);
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

  /** Where the current tick's input leaves the player once its steps have all run, as the world has them when it runs that tick; where they stand before any. */
  tickEnd(): State {
    const t = this.current;
    return t ? this.finish(t, this.state, this.currentG) : this.state;
  }

  /** from run on by the steps tick e has left, with its input and pace, over g. */
  private finish(e: TickEntry, from: State, g: Geometry): State {
    let s = from;
    for (let i = e.steps; i < this.stepsPerTick; i++) {
      s = step(s, e.input, g, e.p);
    }
    return s;
  }

  /** The server tick, fraction and all, that the player drawn f of the way through the last step stands at: its input's tick, the world running one input a tick from the last ack; null before an ack. */
  tickAt(f: number): number | null {
    const t = this.current;
    if (!this.ack || !t) {
      return null;
    }
    return (
      this.ack.tick + t.seq - this.ack.seq - 1 + Math.max(0, t.steps - 1 + f) / this.stepsPerTick
    );
  }

  /** Settles on the world's state after input ackSeq, which ran at tick: on any difference, resets to it and replays the later inputs. */
  reconcile(server: State, ackSeq: number, tick: number): void {
    const entry = this.history.find((e) => e.seq, ackSeq);
    if (!entry) {
      const newest = this.history.last();
      if (newest && ackSeq > newest.seq) {
        this.reset(server);
        this.ack = { seq: ackSeq, tick };
        this.resets++;
      }
      return; // older than the history: a stale Delta
    }
    this.ack = { seq: ackSeq, tick };
    // The server has run the whole tick; the client may be part way through it.
    const predicted = this.finish(entry, entry.state, this.at(entry));
    if (!same(predicted, server)) {
      entry.steps = this.stepsPerTick; // the server's state ends the tick, so a part-run one has no steps left
      this.replayFrom(server, ackSeq);
      this.resets++;
    }
    this.history.retain((e) => e.seq > ackSeq);
    this.server = { state: server, seq: ackSeq };
  }

  /** The map with the mobs blocking that the world blocks e against. */
  private at(e: Pick<TickEntry, 'seq' | 'view'>): Geometry {
    const blockers = this.blockersAt(this.seen(e));
    return blockers.length === 0 ? this.g : { ...this.g, blockers };
  }

  /** e's view held within the frames the world keeps at the tick it runs e, as its seen does: one tick an input on from the last ack. */
  private seen(e: Pick<TickEntry, 'seq' | 'view'>): number {
    if (!this.ack) {
      return e.view;
    }
    const tick = this.ack.tick + e.seq - this.ack.seq;
    if (tick <= 0) {
      return 0;
    }
    return Math.min(Math.max(e.view, tick - Math.min(tick, KEPT_FRAMES)), tick - 1);
  }

  private replayFrom(server: State, seq: number): void {
    let s = server;
    for (const e of this.history) {
      if (e.seq <= seq) {
        continue;
      }
      const g = this.at(e);
      for (let i = 0; i < e.steps; i++) {
        s = step(s, e.input, g, e.p);
      }
      e.state = s;
    }
    this.state = s;
  }
}
