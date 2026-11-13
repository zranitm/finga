// Mob attacks as drawn: each windup plays when the render clock reaches its tick.
import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';
import { overlaps } from '@/sim/step';
import type { Rect } from '@/sim/step';

import type { MobState } from './combat-view';

export type MobAttack = (typeof mobs)[number]['attacks'][number];

const byType = new Map<string, (typeof mobs)[number]>(mobs.map((m) => [m.id, m]));

/** How long an attack's impact shows, from its act or a leap's landing. */
export const IMPACT_MS = 250;

/** How long a Damage's tick is kept for a spit not yet thrown. */
const STRUCK_KEEP_MS = 5000;

/** One mob's attack as drawn, from the render time its windup starts. */
export interface Swing {
  readonly attack: MobAttack;
  readonly facing: 1 | -1;
  readonly from: number;
}

/** Where a mob is in its attack: each phase's progress from 0 to 1. */
export type Pose =
  | { readonly phase: 'windup'; readonly progress: number }
  | { readonly phase: 'travel'; readonly progress: number }
  | { readonly phase: 'impact'; readonly progress: number };

/** The pose of swing s at render time t, or null before it and once its impact is over. */
function poseAt(s: Swing, t: number): Pose | null {
  const act = s.from + s.attack.windup_ms;
  const lands = act + s.attack.travel_ms;
  if (t < s.from) {
    return null;
  }
  if (t < act) {
    return { phase: 'windup', progress: (t - s.from) / s.attack.windup_ms };
  }
  if (t < lands) {
    return { phase: 'travel', progress: (t - act) / s.attack.travel_ms };
  }
  if (t < lands + IMPACT_MS) {
    return { phase: 'impact', progress: (t - lands) / IMPACT_MS };
  }
  return null;
}

/** A spit drawn at a render time: its square's centre. */
export interface SpitDrawn {
  readonly id: number;
  readonly mob: number;
  readonly type: string;
  readonly x: number;
  readonly y: number;
  readonly size: number;
}

export interface AttacksDrawn {
  readonly poses: ReadonlyMap<number, { readonly swing: Swing; readonly pose: Pose }>;
  readonly spits: readonly SpitDrawn[];
}

interface Heard {
  readonly mob: number;
  readonly attack: number;
  readonly facing: 1 | -1;
  readonly from: number;
}

/** A spit in flight: its square's back edge thrown from x0 at the render time from, flying dir until ends. */
interface Spit {
  readonly id: number;
  readonly mob: number;
  readonly type: string;
  readonly x0: number;
  readonly y: number;
  readonly dir: 1 | -1;
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
  private nextSpit = 1;

  /** solids are the map's, which end a spit. */
  constructor(
    private readonly tickMs: number,
    private readonly solids: readonly Rect[],
  ) {}

  /** Forgets everything: a rejoin starts from a snapshot. */
  clear(): void {
    this.pending = [];
    this.showing.clear();
    this.spits = [];
    this.struck = [];
  }

  /** A mob's windup at tick, drawn from that tick's render time; one for a mob or attack this client doesn't know draws nothing. */
  heard(tick: number, mob: number, attack: number, facing: number): void {
    this.pending.push({ mob, attack, facing: facing < 0 ? -1 : 1, from: tick * this.tickMs });
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
      const attack = byType.get(shown.get(h.mob)?.type ?? '')?.attacks[h.attack];
      if (attack) {
        this.showing.set(h.mob, { attack, facing: h.facing, from: h.from });
      }
      return false;
    });
    const poses = new Map<number, { swing: Swing; pose: Pose }>();
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
      const pose = poseAt(swing, t);
      if (!pose || state.pose === MobPose.DOWN) {
        this.showing.delete(mob); // over, or a knockdown ended it; a spit thrown flies on
        continue;
      }
      poses.set(mob, { swing, pose });
    }
    this.struck = this.struck.filter((s) => s.at > t - STRUCK_KEEP_MS);
    this.spits = this.spits.filter((s) => t < s.ends && (shown.get(s.mob)?.hp ?? 0) > 0); // a mob's death ends its spits, as on the server
    return { poses, spits: this.spits.map((s) => this.spitAt(s, t)) };
  }

  private throw(mob: number, state: MobState, swing: Swing): void {
    const def = byType.get(state.type);
    if (!def) {
      return;
    }
    const a = swing.attack;
    const dir = swing.facing;
    const x0 = state.x + (dir * def.width) / 2;
    const y = state.y - def.height / 2;
    const from = swing.from + a.windup_ms;
    let reach = a.range - a.size; // its back edge's travel: its front goes range px past the mob's
    const lane: Rect = {
      x: dir > 0 ? x0 : x0 - a.range,
      y: y - a.size / 2,
      w: a.range,
      h: a.size,
    };
    for (const solid of this.solids) {
      if (overlaps(lane, solid)) {
        const face = dir > 0 ? solid.x - (x0 + a.size) : x0 - a.size - (solid.x + solid.w);
        reach = Math.max(0, Math.min(reach, face));
      }
    }
    let ends = from + (reach / a.speed) * 1000;
    for (const s of this.struck) {
      if (s.mob === mob && s.at >= from) {
        ends = Math.min(ends, s.at);
      }
    }
    this.spits.push({
      id: this.nextSpit++,
      mob,
      type: state.type,
      x0,
      y,
      dir,
      attack: a,
      from,
      ends,
    });
  }

  private spitAt(s: Spit, t: number): SpitDrawn {
    const flown = (Math.max(0, t - s.from) / 1000) * s.attack.speed;
    return {
      id: s.id,
      mob: s.mob,
      type: s.type,
      x: s.x0 + s.dir * (flown + s.attack.size / 2),
      y: s.y,
      size: s.attack.size,
    };
  }
}
