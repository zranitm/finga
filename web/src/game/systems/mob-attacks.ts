// Mob attacks as drawn (3d-world.md section 7): each windup shows when the render clock reaches its tick, its area fixed where the mob then stood; a blow that breaks it off ends it, and a spit flies till it lands.
import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';
import type { Geometry } from '@/sim/step';
import { height } from '@/sim/terrain';

import type { MobState } from './mobs';

export type MobAttack = (typeof mobs)[number]['attacks'][number];

const byType = new Map<string, (typeof mobs)[number]>(mobs.map((m) => [m.id, m]));

/** How long an attack's impact shows, from its travel's end. */
export const IMPACT_MS = 250;

/** How long a Damage's tick is kept for a spit not yet thrown. */
const STRUCK_KEEP_MS = 5000;

/** How finely a spit's way is walked for the solid that stops it. */
const SPIT_STEP_M = 0.05;

export interface Point {
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

/** One mob's attack as drawn, from the render time its windup starts. */
export interface Swing {
  readonly mob: number;
  readonly attack: MobAttack;
  readonly yaw: number;
  readonly from: number;
  /** Where the mob stood as its windup began: the area's centre, fixed then. */
  readonly at: Point;
  /** Where its hit goes: a leap's landing or a rush's end; where it stood for the others. */
  readonly to: Point;
}

/** A spit drawn at a render time: its ball's centre and size across. */
export interface SpitDrawn extends Point {
  readonly mob: number;
  readonly size: number;
}

export interface AttacksDrawn {
  readonly swings: ReadonlyMap<number, Swing>;
  readonly spits: readonly SpitDrawn[];
}

interface Heard {
  readonly mob: number;
  readonly attack: number;
  readonly yaw: number;
  readonly from: number;
  readonly to: Point;
}

/** A spit in flight: thrown from (x, y, z) at the render time from, flying along yaw at speed until ends. */
interface Spit {
  readonly mob: number;
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly yaw: number;
  readonly attack: MobAttack;
  readonly from: number;
  ends: number;
}

const staggered = (p: MobPose): boolean => p === MobPose.FLINCH || p === MobPose.DOWN;

export class MobAttacks {
  private pending: Heard[] = [];
  private readonly showing = new Map<number, Swing>();
  private readonly thrown = new WeakSet<Swing>();
  private spits: Spit[] = [];
  private struck: { mob: number; at: number }[] = [];

  /** g is the map's, whose solids and rises stop a spit. */
  constructor(
    private readonly tickMs: number,
    private readonly g: Geometry,
  ) {}

  /** Forgets everything: a rejoin starts from a snapshot. */
  clear(): void {
    this.pending = [];
    this.showing.clear();
    this.spits = [];
    this.struck = [];
  }

  /** A mob's windup at tick turned to yaw, its hit going to `to`, drawn from that tick's render time; one for a mob or attack this client doesn't know draws nothing. */
  heard(tick: number, mob: number, attack: number, yaw: number, to: Point): void {
    this.pending.push({ mob, attack, yaw, from: tick * this.tickMs, to });
  }

  /** A Damage from mob at tick: its spit in flight, or still to come, ends there. */
  struckBy(tick: number, mob: number): void {
    const at = tick * this.tickMs;
    this.struck.push({ mob, at });
    for (const s of this.spits) {
      if (s.mob === mob && s.from <= at && at < s.ends) {
        s.ends = at;
      }
    }
  }

  /** What the attacks draw at render time t over the mobs drawn there. */
  frame(t: number, shown: ReadonlyMap<number, MobState>): AttacksDrawn {
    this.pending = this.pending.filter((h) => {
      if (h.from > t) {
        return true;
      }
      const state = shown.get(h.mob);
      const attack = byType.get(state?.type ?? '')?.attacks[h.attack];
      if (state && attack) {
        const at = { x: state.x, y: state.y, z: state.z };
        this.showing.set(h.mob, { mob: h.mob, attack, yaw: h.yaw, from: h.from, at, to: h.to });
      }
      return false;
    });
    for (const [mob, swing] of this.showing) {
      const state = shown.get(mob);
      const thrown = swing.from + swing.attack.windup_ms;
      const struck = state && staggered(state.pose) ? t - state.poseMs : Infinity; // when a blow staggered it, if one has
      if (!state || state.hp <= 0 || struck < thrown) {
        this.showing.delete(mob); // gone, dead, or a blow broke off its windup
        continue;
      }
      if (swing.attack.kind === 'shot' && t >= thrown && !this.thrown.has(swing)) {
        this.thrown.add(swing); // even on a frame past its impact, so a slow frame keeps the spit
        this.throw(mob, state, swing);
      }
      const over = thrown + swing.attack.travel_ms + IMPACT_MS;
      if (t >= over || state.pose === MobPose.DOWN) {
        this.showing.delete(mob); // over, or a knockdown ended it; a spit thrown flies on
      }
    }
    this.struck = this.struck.filter((s) => s.at > t - STRUCK_KEEP_MS);
    this.spits = this.spits.filter((s) => t < s.ends && (shown.get(s.mob)?.hp ?? 0) > 0); // a mob's death ends its spits, as on the server
    return { swings: this.showing, spits: this.spits.map((s) => spitAt(s, t)) };
  }

  /** Throws a spit from the mob's centre at mid-height along its yaw, as the server does: it flies its range, short of the first solid or rise, or till a Damage says it landed. */
  private throw(mob: number, state: MobState, swing: Swing): void {
    const def = byType.get(state.type);
    if (!def) {
      return;
    }
    const a = swing.attack;
    const from = swing.from + a.windup_ms;
    const s = {
      mob,
      x: state.x,
      y: state.y + def.height / 2,
      z: state.z,
      yaw: swing.yaw,
      attack: a,
      from,
    };
    let ends = from + (this.reach(s) / a.speed) * 1000;
    for (const k of this.struck) {
      if (k.mob === mob && k.at >= from) {
        ends = Math.min(ends, k.at);
      }
    }
    this.spits.push({ ...s, ends });
  }

  /** How far a spit flies before a solid, a rise or the map's edge stops its ball. */
  private reach(s: Omit<Spit, 'ends'>): number {
    const r = s.attack.size / 2;
    const [dx, dz] = [Math.sin(s.yaw), Math.cos(s.yaw)];
    for (let d = SPIT_STEP_M; d <= s.attack.range; d += SPIT_STEP_M) {
      if (blocked(this.g, s.x + dx * d, s.z + dz * d, r, s.y - r)) {
        return d - SPIT_STEP_M;
      }
    }
    return s.attack.range;
  }
}

function spitAt(s: Spit, t: number): SpitDrawn {
  const flown = (Math.max(0, t - s.from) / 1000) * s.attack.speed;
  return {
    mob: s.mob,
    x: s.x + Math.sin(s.yaw) * flown,
    y: s.y,
    z: s.z + Math.cos(s.yaw) * flown,
    size: s.attack.size,
  };
}

/** Whether a ball of radius r with its bottom at `bottom`, centred at (x, z), meets a solid or ground over its bottom, or the map's edge. */
function blocked(g: Geometry, x: number, z: number, r: number, bottom: number): boolean {
  if (x < r || z < r || x > g.size_x - r || z > g.size_z - r || height(g.terrain, x, z) > bottom) {
    return true;
  }
  for (const c of g.cylinders) {
    if (c.top > bottom && Math.hypot(x - c.x, z - c.z) < c.radius + r) {
      return true;
    }
  }
  for (const b of g.boxes) {
    if (b.top <= bottom) {
      continue;
    }
    const [px, pz] = [x - b.x, z - b.z];
    const u = Math.abs(px * b.ux + pz * b.uz) - b.half_w;
    const v = Math.abs(pz * b.ux - px * b.uz) - b.half_d;
    if (Math.hypot(Math.max(u, 0), Math.max(v, 0)) < r) {
      return true;
    }
  }
  return false;
}
