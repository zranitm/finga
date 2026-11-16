// Mob attacks as drawn (3d-world.md section 10): each windup runs on the player's own clock from its tick, its area fixed where the mob then stood, so it ends as the world strikes them. Its result shows once the world says it acted; a blow or death heard first breaks it off. A hop or a charge carries its mob on the same clock, and a spit flies on it.
import { ticksIn } from '@/lib/fixed-step';
import { mobById } from '@/progression/mobs';
import type { MobType } from '@/progression/mobs';
import { MobPose } from '@/protocol/messages';
import type { Geometry } from '@/sim/step';
import { height } from '@/sim/terrain';

import type { MobState } from './mobs';

export type MobAttack = MobType['attacks'][number];

/** How long an attack's impact shows, from its result. */
export const IMPACT_MS = 250;

/** How long a carried mob takes to blend between where it's drawn and where its act carries it. */
export const CARRY_BLEND_MS = 150;

/** How long a Damage's tick is kept for a swing or a spit still to come. */
const STRUCK_KEEP_MS = 5000;

/** How finely a spit's way is walked for the solid that stops it. */
const SPIT_STEP_M = 0.05;

/** How far a charge may go into a mob before it's crowded, as the world's crowdedBy: it passes one it only grazes, and meets a player first unless it would sink this much deeper into a mob than it is. */
const CROWDED_BY_M = 0.25;
const AHEAD_CAP_MS = 200; // the furthest a mob's last frames are carried on toward the player's clock

export interface Point {
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

/** An upright body: feet at (x, y, z). */
export interface Upright extends Point {
  readonly radius: number;
  readonly height: number;
}

/** What the frames say of a mob ahead of the drawn ones. */
export interface Heard {
  /** Where it stood at tick, as the newest frame up to it says, and that frame's tick; undefined for one not kept then. */
  placeAt(mob: number, tick: number): (Point & { readonly tick: number }) | undefined;
  /** Its poses from the drawn one on, each from its frame's time. */
  posesOf(mob: number): readonly { readonly t: number; readonly pose: MobPose }[];
  /** The time of the frame that took it off the map, killed; undefined while it's on it. */
  goneAt(mob: number): number | undefined;
}

/** One mob's attack as drawn, in the world's time: from its windup's start. */
export interface Swing {
  readonly mob: number;
  readonly attack: MobAttack;
  readonly yaw: number;
  readonly from: number;
  /** When its windup ends and it acts, in whole ticks as the world counts them. */
  readonly hit: number;
  /** When its act ends: a hop's landing or a charge's end; its hit for the others. */
  readonly end: number;
  /** Where the mob stood as its windup began: the area's centre, fixed then. */
  readonly at: Point;
  /** Where its hit goes: a leap's landing or a rush's end; where it stood for the others. */
  readonly to: Point;
  /** A spit's: how far its ball flies from there before a solid, a rise or the map's edge stops it. */
  readonly reach?: number;
  /** The player's time its result shows from, once the world says it acted: its Damage, or the drawn mobs reaching its end. */
  landed?: number;
}

/** A spit drawn on the player's clock: its ball's centre and size across. */
export interface SpitDrawn extends Point {
  readonly mob: number;
  readonly size: number;
}

/** Where a mob is drawn while its hop or charge carries it, and the way it faces. */
export interface Carried extends Point {
  readonly yaw: number;
}

export interface AttacksDrawn {
  readonly swings: ReadonlyMap<number, Swing>;
  readonly spits: readonly SpitDrawn[];
  readonly carried: ReadonlyMap<number, Carried>;
}

interface HeardSwing {
  readonly mob: number;
  readonly tick: number;
  readonly attack: number;
  readonly yaw: number;
  readonly from: number;
  readonly to: Point;
}

/** A spit in flight: thrown from (x, y, z) at the player's time from, flying along yaw at speed until ends. */
interface Spit {
  readonly swing: Swing;
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly from: number;
  ends: number;
}

/** A carried mob going back to where it's drawn: from `last`, starting at the render time `at`. */
interface Release {
  readonly last: Carried;
  readonly at: number;
}

const staggered = (p: MobPose): boolean => p === MobPose.FLINCH || p === MobPose.DOWN;
const travels = (a: MobAttack): boolean => a.kind === 'leap' || a.kind === 'rush';
const clamp01 = (v: number): number => Math.max(0, Math.min(1, v));

export class MobAttacks {
  private pending: HeardSwing[] = [];
  private readonly showing = new Map<number, Swing>();
  private readonly stops = new WeakMap<Swing, number>(); // how far along its lane a charge met the player
  private readonly ran = new WeakMap<Swing, number>(); // how far along its lane a charge had got by the last frame
  private readonly since = new WeakMap<Swing, number>(); // the render time it was first drawn
  private readonly thrown = new WeakSet<Swing>();
  private readonly lastCarried = new Map<number, Carried>();
  private readonly releasing = new Map<number, Release>();
  private spits: Spit[] = [];
  private struck: { mob: number; at: number }[] = [];

  /** g is the map's, whose solids and rises stop a spit; heard is what the frames say of the mobs ahead of the drawn ones. */
  constructor(
    private readonly tickMs: number,
    private readonly g: Geometry,
    private readonly heard: Heard,
  ) {}

  /** Forgets everything: a rejoin starts from a snapshot. */
  clear(): void {
    this.pending = [];
    this.showing.clear();
    this.lastCarried.clear();
    this.releasing.clear();
    this.spits = [];
    this.struck = [];
  }

  /** A mob's windup at tick turned to yaw, its hit going to `to`, drawn from that tick on the player's clock; one for a mob or attack this client doesn't know draws nothing. */
  heardWindup(tick: number, mob: number, attack: number, yaw: number, to: Point): void {
    this.pending.push({ mob, tick, attack, yaw, from: tick * this.tickMs, to });
  }

  /** A Damage from mob at tick: the world says its swing acted, and its spit in flight, or still to come, ends there. */
  struckBy(tick: number, mob: number): void {
    const at = tick * this.tickMs;
    this.struck.push({ mob, at });
    for (const s of this.spits) {
      if (s.swing.mob === mob && s.from <= at && at < s.ends) {
        s.ends = at;
      }
    }
  }

  /**
   * What the attacks draw at render time t, the player's clock reading mine, over the mobs drawn at t: each windup from when mine reaches it, so it ends for the player as the world strikes them.
   * me is the player's body as drawn, which a charge or a spit stops at, as the world's does.
   */
  frame(t: number, mine: number, shown: ReadonlyMap<number, MobState>, me?: Upright): AttacksDrawn {
    this.pending = this.pending.filter((h) => {
      const state = shown.get(h.mob);
      if (h.from > mine || (!state && h.from > t)) {
        return true; // to come, or its mob not drawn yet
      }
      const def = mobById(state?.type);
      const attack = def?.attacks[h.attack];
      if (state && def && attack) {
        this.release(h.mob, t);
        const swing = this.swingOf(h, attack, def.height, state);
        this.since.set(swing, t);
        this.showing.set(h.mob, swing);
      }
      return false;
    });
    const carried = new Map<number, Carried>();
    let view: Map<number, MobState> | undefined; // the mobs a charge sweeps, on the player's clock
    for (const [mob, swing] of this.showing) {
      const state = shown.get(mob);
      if (!state || this.broken(swing)) {
        this.showing.delete(mob); // gone, or a blow or its death broke it off
        this.spits = this.spits.filter((s) => s.swing !== swing);
        this.release(mob, t);
        continue;
      }
      if (swing.landed === undefined && (t >= swing.end || this.actedBy(swing))) {
        swing.landed = Math.max(mine, swing.end);
      }
      if (swing.attack.kind === 'shot' && mine >= swing.hit && !this.thrown.has(swing)) {
        this.thrown.add(swing);
        this.throw(swing, state);
      }
      view ??= swing.attack.kind === 'rush' ? this.ahead(t, mine, shown) : undefined;
      const c = travels(swing.attack)
        ? this.carry(swing, state, t, mine, view ?? shown, me)
        : undefined;
      if (c) {
        carried.set(mob, c);
        this.lastCarried.set(mob, c);
      } else {
        this.lastCarried.delete(mob);
      }
      if (t >= swing.end + CARRY_BLEND_MS && mine >= (swing.landed ?? Infinity) + IMPACT_MS) {
        this.showing.delete(mob); // over: its result shown, and its mob back where it's drawn
        this.lastCarried.delete(mob);
      }
    }
    for (const [mob, r] of this.releasing) {
      const state = shown.get(mob);
      const k = (t - r.at) / CARRY_BLEND_MS;
      if (!state || k >= 1 || carried.has(mob)) {
        this.releasing.delete(mob);
        continue;
      }
      carried.set(mob, blend(r.last, state, k, state.yaw));
    }
    this.struck = this.struck.filter((s) => s.at > t - STRUCK_KEEP_MS);
    this.spits = this.spits.filter((s) => {
      const gone = this.heard.goneAt(s.swing.mob);
      if (gone !== undefined) {
        s.ends = Math.min(s.ends, gone); // a mob's death ends its spits, as on the server
      }
      if (me && mine < s.ends && meets(spitAt(s, mine), s.swing.attack.size / 2, me)) {
        s.ends = mine; // on the player, as the world's lands on them
      }
      return mine < s.ends;
    });
    return { swings: this.showing, spits: this.spits.map((s) => spitAt(s, mine)), carried };
  }

  private swingOf(h: HeardSwing, attack: MobAttack, tall: number, state: MobState): Swing {
    const p = this.heard.placeAt(h.mob, h.tick);
    const at = p ? { x: p.x, y: p.y, z: p.z } : { x: state.x, y: state.y, z: state.z };
    const hit = h.from + this.ms(attack.windup_ms);
    const end = travels(attack) ? hit + this.ms(attack.travel_ms) : hit;
    const swing: Swing = { mob: h.mob, attack, yaw: h.yaw, from: h.from, hit, end, at, to: h.to };
    if (attack.kind !== 'shot') {
      return swing;
    }
    return { ...swing, reach: this.reach(at.x, at.y + tall / 2, at.z, h.yaw, attack) };
  }

  /** A duration in whole ticks, as the world counts it. */
  private ms(d: number): number {
    return ticksIn(d, this.tickMs) * this.tickMs;
  }

  /** Whether the frames say the world broke a swing off before its result: a flinch or knockdown in its windup, a knockdown in its act, or its death by its end. */
  private broken(s: Swing): boolean {
    if (s.landed !== undefined) {
      return false;
    }
    const gone = this.heard.goneAt(s.mob);
    if (gone !== undefined && gone >= s.from && gone <= s.end) {
      return true;
    }
    return this.heard
      .posesOf(s.mob)
      .some(
        (p) =>
          p.t >= s.from &&
          ((p.t < s.hit && staggered(p.pose)) || (p.t < s.end && p.pose === MobPose.DOWN)),
      );
  }

  /** Whether a Damage from the swing's mob said it acted. */
  private actedBy(s: Swing): boolean {
    return this.struck.some((k) => k.mob === s.mob && k.at >= s.hit);
  }

  /**
   * Where a hop or a charge has carried its mob at the player's time mine, blended from where it's drawn as it starts and back as its act ends, once the drawn mobs reach that end; undefined once it's back.
   * A charge stops where the world's does: at the player its run meets once it runs, level with its body, or at a mob that stepped into its lane after its windup, unless the player comes first. Only along the way it went since the last frame, so nothing it passed pulls it back.
   * shown is the mobs it sweeps, where the world has them at mine (ahead).
   */
  private carry(
    s: Swing,
    state: MobState,
    t: number,
    mine: number,
    shown: ReadonlyMap<number, MobState>,
    me?: Upright,
  ): Carried | undefined {
    const dx = s.to.x - s.at.x;
    const dz = s.to.z - s.at.z;
    const length = Math.hypot(dx, dz);
    const went = length * clamp01((mine - s.hit) / Math.max(1, s.end - s.hit));
    if (s.attack.kind === 'rush' && length > 0) {
      const ran = this.ran.get(s) ?? 0;
      this.ran.set(s, went);
      let stop = this.stops.get(s);
      if (stop === undefined && ran < length && mine >= s.hit) {
        const [ux, uz, r] = [dx / length, dz / length, radiusOf(state)];
        const ahead = went > ran ? mobAhead(s, state, shown, ux, uz, ran, went - ran) : undefined;
        const reach = ahead ? ran + Math.min(ahead.deep, went - ran) : went;
        const y = s.at.y + (s.to.y - s.at.y) * (ran / length);
        stop =
          me && levelWith(state, y, me)
            ? meetsBetween(s.at, ux, uz, r, me, Math.min(ran, went), reach)
            : undefined;
        stop ??= ahead && ran + ahead.cut;
        if (stop !== undefined) {
          this.stops.set(s, stop);
        }
      }
    }
    return this.carriedPlace(s, state, t, mine);
  }

  /** Where a hop or a charge carries its mob at render time t and the player's time mine, as far as the stop it has so far; undefined once it's back. */
  private carriedPlace(s: Swing, state: MobState, t: number, mine: number): Carried | undefined {
    const dx = s.to.x - s.at.x;
    const dz = s.to.z - s.at.z;
    const length = Math.hypot(dx, dz);
    const went = Math.min(
      length * clamp01((mine - s.hit) / Math.max(1, s.end - s.hit)),
      this.stops.get(s) ?? Infinity,
    );
    const f = length > 0 ? went / length : 0;
    const there = { x: s.at.x + dx * f, y: s.at.y + (s.to.y - s.at.y) * f, z: s.at.z + dz * f };
    const into = clamp01((t - (this.since.get(s) ?? t)) / CARRY_BLEND_MS);
    const w = Math.min(into, 1 - clamp01((t - s.end) / CARRY_BLEND_MS));
    return w > 0 ? blend(state, there, w, w >= 0.5 ? s.yaw : state.yaw) : undefined;
  }

  /**
   * The drawn mobs where the world has them at the player's clock mine, which the mobs drawn a moment behind lag: a carried one where its own hop or charge carries it this frame, at render time t, cut at the stop it had by the last frame, so two charges never wait on each other; any other at its newest frame up to mine, moved on by its velocity between its last two frames for at most AHEAD_CAP_MS.
   */
  private ahead(
    t: number,
    mine: number,
    shown: ReadonlyMap<number, MobState>,
  ): Map<number, MobState> {
    const view = new Map<number, MobState>();
    for (const [id, s] of shown) {
      const swing = this.showing.get(id);
      const c = swing && travels(swing.attack) ? this.carriedPlace(swing, s, t, mine) : undefined;
      const p = c ? undefined : this.heard.placeAt(id, Math.floor(mine / this.tickMs));
      const q = p && this.heard.placeAt(id, p.tick - 1);
      if (c || !p) {
        view.set(id, c ? { ...s, x: c.x, y: c.y, z: c.z } : s);
        continue;
      }
      const k =
        q && q.tick < p.tick
          ? Math.min(mine - p.tick * this.tickMs, AHEAD_CAP_MS) / ((p.tick - q.tick) * this.tickMs)
          : 0;
      view.set(id, {
        ...s,
        x: p.x + (p.x - (q?.x ?? p.x)) * k,
        y: p.y,
        z: p.z + (p.z - (q?.z ?? p.z)) * k,
      });
    }
    return view;
  }

  /** A carried mob goes back to where it's drawn, from where it was last carried. */
  private release(mob: number, t: number): void {
    const last = this.lastCarried.get(mob);
    this.lastCarried.delete(mob);
    if (last) {
      this.releasing.set(mob, { last, at: t });
    }
  }

  /** Throws a spit from where the mob stood at mid-height along its yaw at its hit, as the server does: it flies its reach, or till a Damage says it landed. */
  private throw(swing: Swing, state: MobState): void {
    const def = mobById(state.type);
    if (!def) {
      return;
    }
    const a = swing.attack;
    const from = swing.hit;
    let ends = from + ((swing.reach ?? a.range) / a.speed) * 1000;
    for (const k of this.struck) {
      if (k.mob === swing.mob && k.at >= from) {
        ends = Math.min(ends, k.at);
      }
    }
    this.spits.push({
      swing,
      x: swing.at.x,
      y: swing.at.y + def.height / 2,
      z: swing.at.z,
      from,
      ends,
    });
  }

  /** How far a spit thrown from (x, y, z) along yaw flies before a solid, a rise or the map's edge stops its ball. */
  private reach(x: number, y: number, z: number, yaw: number, a: MobAttack): number {
    const r = a.size / 2;
    const [dx, dz] = [Math.sin(yaw), Math.cos(yaw)];
    for (let d = SPIT_STEP_M; d <= a.range; d += SPIT_STEP_M) {
      if (blocked(this.g, x + dx * d, z + dz * d, r, y - r)) {
        return d - SPIT_STEP_M;
      }
    }
    return a.range;
  }
}

function radiusOf(state: MobState): number {
  return mobById(state.type)?.radius ?? 0.5;
}

/** Whether me overlaps the body of a mob of state's type standing at y, as the world's charge meets only a player between its feet and its top. */
function levelWith(state: MobState, y: number, me: Upright): boolean {
  return me.y <= y + (mobById(state.type)?.height ?? 0) && me.y + me.height >= y;
}

/**
 * The first drawn mob a charge's run cuts going on dist from ran along (ux, uz), as the world's mobAhead: how far on it touches it, and how far on it would be crowded deeper into any mob ahead than it is; undefined if none cuts it.
 * One beside the lane by less than the crowding margin is grazed and passed; one behind, or clear above or below, is no matter.
 */
function mobAhead(
  s: Swing,
  state: MobState,
  shown: ReadonlyMap<number, MobState>,
  ux: number,
  uz: number,
  ran: number,
  dist: number,
): { cut: number; deep: number } | undefined {
  const me = mobById(state.type);
  if (!me) {
    return undefined;
  }
  const length = Math.hypot(s.to.x - s.at.x, s.to.z - s.at.z);
  const [x, z] = [s.at.x + ux * ran, s.at.z + uz * ran];
  const y = s.at.y + (s.to.y - s.at.y) * (ran / length);
  let [cut, deep, found] = [dist, Infinity, false];
  for (const [mob, o] of shown) {
    const def = mobById(o.type);
    if (mob === s.mob || !def || o.y >= y + me.height || y >= o.y + def.height) {
      continue;
    }
    const [ox, oz] = [o.x - x, o.z - z];
    const along = ox * ux + oz * uz;
    const r = me.radius + def.radius;
    const apart = Math.hypot(ox, oz);
    const across = apart * apart - along * along;
    const graze = r - CROWDED_BY_M;
    if (along <= 0 || across >= graze * graze) {
      continue;
    }
    const at = Math.max(0, along - Math.sqrt(r * r - across));
    if (at < cut) {
      [cut, found] = [at, true];
    }
    const near = Math.min(apart, r) - CROWDED_BY_M;
    if (near > 0 && near * near > across) {
      deep = Math.min(deep, along - Math.sqrt(near * near - across));
    }
  }
  return found ? { cut, deep } : undefined;
}

/** From a toward b by k, facing yaw. */
function blend(a: Point, b: Point, k: number, yaw: number): Carried {
  const f = clamp01(k);
  return { x: a.x + (b.x - a.x) * f, y: a.y + (b.y - a.y) * f, z: a.z + (b.z - a.z) * f, yaw };
}

/** How far along (ux, uz) from `from` a body of radius r going from lo to hi first touches `me`, as the world's charge meets a player in a step; lo if it touches already, undefined if it never does. */
function meetsBetween(
  from: Point,
  ux: number,
  uz: number,
  r: number,
  me: Upright,
  lo: number,
  hi: number,
): number | undefined {
  const px = me.x - from.x;
  const pz = me.z - from.z;
  const along = px * ux + pz * uz;
  const across = px * px + pz * pz - along * along;
  const reach = r + me.radius;
  const nearest = Math.max(lo, Math.min(along, hi));
  if (across + (along - nearest) ** 2 > reach * reach) {
    return undefined;
  }
  return Math.max(lo, along - Math.sqrt(reach * reach - across));
}

/** Whether a ball of radius r centred at p touches an upright body, as the world's spit lands. */
function meets(p: Point, r: number, me: Upright): boolean {
  return (
    Math.hypot(p.x - me.x, p.z - me.z) <= r + me.radius &&
    p.y + r >= me.y &&
    p.y - r <= me.y + me.height
  );
}

function spitAt(s: Spit, mine: number): SpitDrawn {
  const a = s.swing.attack;
  const flown = (Math.max(0, mine - s.from) / 1000) * a.speed;
  return {
    mob: s.swing.mob,
    x: s.x + Math.sin(s.swing.yaw) * flown,
    y: s.y,
    z: s.z + Math.cos(s.swing.yaw) * flown,
    size: a.size,
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
