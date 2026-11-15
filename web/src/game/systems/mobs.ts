// The mobs as the world sends them: drawn behind the newest frame, blended between frames, and kept by tick for the inputs predicted against them.
import { turn } from '@/lib/angle';
import { mobById } from '@/progression/mobs';
import { MobPose } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { Blocker } from '@/sim/step';

import { FrameBuffer } from './interpolation';
import type { Mix } from './interpolation';
import { HISTORY_MS } from './prediction';

const BLOCKERS_KEEP_MS = HISTORY_MS + 500; // a replayed input's view is at most this far behind the newest tick

export type MobWire = Data<'snapshot'>['mobs'][number];

/** Where a mob is and the way it faces, in metres and radians from +z toward +x. */
interface Place {
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly yaw: number;
}

/** A mob moves only as the frames say: blended between them, turning the short way, held past the last. */
const places: Mix<Place> = {
  blend: (a, b, f) => ({
    x: a.x + (b.x - a.x) * f,
    y: a.y + (b.y - a.y) * f,
    z: a.z + (b.z - a.z) * f,
    yaw: turn(a.yaw, b.yaw, f),
  }),
  ahead: (s) => s,
};

export interface MobState extends Place {
  readonly type: string;
  readonly hp: number;
  readonly maxHp: number;
  readonly pose: MobPose;
  readonly poseMs: number; // how long it has been in its pose, at the render time
}

/** A mob's pose from the render time t of the frame that set it. */
interface PoseAt {
  readonly t: number;
  readonly pose: MobPose;
}

/** A mob's places by tick, as the frames sent them, and the tick it left: what inputs were predicted against. */
interface Track {
  readonly type: string;
  readonly at: { tick: number; x: number; y: number; z: number }[];
  gone: number | null;
}

export class MobTracker {
  private readonly buffers = new Map<number, FrameBuffer<Place>>();
  private readonly info = new Map<
    number,
    { type: string; hp: number; maxHp: number; poses: PoseAt[] }
  >();
  private readonly tracks = new Map<number, Track>();
  private newest = 0;

  constructor(private readonly tickMs: number) {}

  /** A whole map starts over: a new instance numbers its mobs and ticks afresh. */
  applySnapshot(t: number, mobs: readonly MobWire[]): void {
    this.clear();
    for (const m of mobs) {
      this.put(t, m);
    }
  }

  applyDelta(t: number, mobs: readonly MobWire[], removed: readonly number[]): void {
    for (const m of mobs) {
      this.put(t, m);
    }
    for (const id of removed) {
      this.buffers.get(id)?.remove(t);
      const i = this.info.get(id);
      if (i) {
        i.hp = 0; // killed: it shows dead until it drains
      }
      const tr = this.tracks.get(id);
      if (tr) {
        tr.gone = this.tickOf(t);
      }
    }
    this.prune();
  }

  /** The mobs' circles as view's frame left them, in id order as the world keeps them, which it blocks inputs at view against. */
  blockersAt(view: number): Blocker[] {
    const blockers: Blocker[] = [];
    const ids = [...this.tracks.keys()].sort((a, b) => a - b);
    for (const id of ids) {
      const tr = this.tracks.get(id)!;
      const size = mobById(tr.type);
      const s = tr.at.findLast((a) => a.tick <= view);
      if (!size || !s || (tr.gone !== null && tr.gone <= view)) {
        continue;
      }
      blockers.push({ x: s.x, z: s.z, radius: size.radius, bottom: s.y, top: s.y + size.height });
    }
    return blockers;
  }

  /** Where a mob stood at tick, as the frames sent it, and its type; undefined for one not kept then. */
  placeAt(id: number, tick: number): { x: number; y: number; z: number; type: string } | undefined {
    const tr = this.tracks.get(id);
    const s = tr?.at.findLast((a) => a.tick <= tick);
    return tr && s && { x: s.x, y: s.y, z: s.z, type: tr.type };
  }

  render(renderTime: number): { shown: Map<number, MobState>; gone: number[] } {
    const shown = new Map<number, MobState>();
    const gone: number[] = [];
    for (const [id, b] of this.buffers) {
      if (b.drained(renderTime)) {
        this.buffers.delete(id);
        this.info.delete(id);
        gone.push(id);
        continue;
      }
      const s = b.sample(renderTime);
      const i = this.info.get(id);
      if (s && i) {
        while (i.poses.length > 1 && i.poses[1]!.t <= renderTime) {
          i.poses.shift(); // drawn past it
        }
        const p = i.poses[0];
        shown.set(id, {
          type: i.type,
          ...s,
          hp: i.hp,
          maxHp: i.maxHp,
          pose: p?.pose ?? MobPose.UNSPECIFIED,
          poseMs: p ? Math.max(0, renderTime - p.t) : 0,
        });
      }
    }
    return { shown, gone };
  }

  clear(): void {
    this.buffers.clear();
    this.info.clear();
    this.tracks.clear();
    this.newest = 0;
  }

  private tickOf(t: number): number {
    return Math.round(t / this.tickMs);
  }

  private track(tick: number, m: MobWire): void {
    this.newest = Math.max(this.newest, tick);
    let tr = this.tracks.get(m.id);
    if (!tr) {
      tr = { type: m.type, at: [], gone: null };
      this.tracks.set(m.id, tr);
    }
    const last = tr.at.at(-1);
    if (last?.tick === tick) {
      last.x = m.x;
      last.y = m.y;
      last.z = m.z;
    } else if (!last || tick > last.tick) {
      tr.at.push({ tick, x: m.x, y: m.y, z: m.z });
    }
  }

  /** Keeps BLOCKERS_KEEP_MS of ticks, and each mob's last place before them as its floor. */
  private prune(): void {
    const cutoff = this.newest - Math.ceil(BLOCKERS_KEEP_MS / this.tickMs);
    for (const [id, tr] of this.tracks) {
      if (tr.gone !== null && tr.gone <= cutoff) {
        this.tracks.delete(id);
        continue;
      }
      while (tr.at.length > 1 && tr.at[1]!.tick <= cutoff) {
        tr.at.shift();
      }
    }
  }

  private put(t: number, m: MobWire): void {
    this.track(this.tickOf(t), m);
    let b = this.buffers.get(m.id);
    if (!b) {
      b = new FrameBuffer(this.tickMs, places);
      this.buffers.set(m.id, b);
    }
    b.push(t, { x: m.x, y: m.y, z: m.z, yaw: m.yaw });
    const poses = this.info.get(m.id)?.poses ?? [];
    if (poses.at(-1)?.pose !== m.pose) {
      poses.push({ t, pose: m.pose });
    }
    this.info.set(m.id, { type: m.type, hp: m.hp, maxHp: m.maxHp, poses });
  }
}
