// The mobs as the world sends them: drawn behind the newest frame, blended between frames, and kept by tick for the inputs predicted against them.
import { turn } from '@/lib/angle';
import { mobById } from '@/progression/mobs';
import type { StatusOn } from '@/progression/statuses';
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

/** A mob's pose from the time t of the frame that set it. */
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
    {
      type: string;
      hp: number;
      maxHp: number;
      poses: PoseAt[];
      statuses: readonly StatusOn[];
    }
  >();
  private readonly tracks = new Map<number, Track>();
  private readonly outOfSight = new Set<number>(); // removed as out of sight, not killed
  private newest = 0;

  constructor(private readonly tickMs: number) {}

  /** A whole map starts over: a new instance numbers its mobs and ticks afresh. */
  applySnapshot(t: number, mobs: readonly MobWire[]): void {
    this.clear();
    for (const m of mobs) {
      this.put(t, m);
    }
  }

  /** A frame: the mobs that changed, those killed, which show dead until they drain, and those out of sight, which go as they are. */
  applyDelta(
    t: number,
    mobs: readonly MobWire[],
    removed: readonly number[],
    unseen: readonly number[],
  ): void {
    for (const m of mobs) {
      this.put(t, m);
    }
    for (const id of removed) {
      const i = this.info.get(id);
      if (i) {
        i.hp = 0;
      }
      this.leave(t, id);
    }
    for (const id of unseen) {
      this.outOfSight.add(id);
      this.leave(t, id);
    }
    this.prune();
  }

  private leave(t: number, id: number): void {
    this.buffers.get(id)?.remove(t);
    const tr = this.tracks.get(id);
    if (tr) {
      tr.gone = this.tickOf(t);
    }
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

  /** Where a mob stood at tick, as the newest frame up to it sent it, that frame's tick, and its type; undefined for one not kept then. */
  placeAt(
    id: number,
    tick: number,
  ): { x: number; y: number; z: number; tick: number; type: string } | undefined {
    const tr = this.tracks.get(id);
    const s = tr?.at.findLast((a) => a.tick <= tick);
    return tr && s && { x: s.x, y: s.y, z: s.z, tick: s.tick, type: tr.type };
  }

  /** A mob's poses from the drawn one on, as the frames sent them, each from its frame's time. */
  posesOf(id: number): readonly PoseAt[] {
    return this.info.get(id)?.poses ?? [];
  }

  /** A mob's statuses as the newest frame that sent it has them, each with the tick it ends on. */
  statusesOf(id: number): readonly StatusOn[] {
    return this.info.get(id)?.statuses ?? [];
  }

  /** The time of the frame that took a mob off the map; undefined while it's on it. */
  goneAt(id: number): number | undefined {
    const gone = this.tracks.get(id)?.gone;
    return gone == null ? undefined : gone * this.tickMs;
  }

  /** The mobs drawn at renderTime, and those drawn past their removal: killed, gone, or out of sight, unseen. */
  render(renderTime: number): { shown: Map<number, MobState>; gone: number[]; unseen: number[] } {
    const shown = new Map<number, MobState>();
    const gone: number[] = [];
    const unseen: number[] = [];
    for (const [id, b] of this.buffers) {
      if (b.drained(renderTime)) {
        this.buffers.delete(id);
        this.info.delete(id);
        (this.outOfSight.delete(id) ? unseen : gone).push(id);
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
    return { shown, gone, unseen };
  }

  clear(): void {
    this.buffers.clear();
    this.info.clear();
    this.tracks.clear();
    this.outOfSight.clear();
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
    if (tr.gone !== null && tick > tr.gone) {
      tr.gone = null; // back in sight
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
    this.outOfSight.delete(m.id);
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
    this.info.set(m.id, {
      type: m.type,
      hp: m.hp,
      maxHp: m.maxHp,
      poses,
      statuses: m.statuses.map((st) => ({ id: st.id, ends: st.ends })),
    });
  }
}
