// The fight as drawn (combat-effects.md sections 4 to 6): each action's look, each hit's spark and number where its target is drawn, a shot's once it visibly lands, the player's own hits held a moment, and their cancels' afterimage.
import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';
import { ticksIn } from '@/lib/fixed-step';
import { mobById } from '@/progression/mobs';
import type { Skill, Stage } from '@/progression/skills';
import type { Data, MobPose } from '@/protocol/messages';

import { warmAfterimage } from '../effects/afterimage';
import { arrival, shotRange } from '../effects/flight';
import type { Shot } from '../effects/flight';
import type { Fx } from '../effects/fx';
import { attackEffect, damageType, hitEffects, mobEffect, skillEffect } from '../effects/ids';
import type { Damage, EffectId } from '../effects/ids';
import type { Body, PlayOptions, Point, Stop } from '../effects/types';
import { numberFor, parseRef } from '../systems/combat-view';
import type { Ref } from '../systems/combat-view';
import { HitStops } from '../systems/hit-stop';
import { IMPACT_MS, MobAttacks } from '../systems/mob-attacks';
import type { Carried, SpitDrawn, Swing } from '../systems/mob-attacks';
import type { MobState } from '../systems/mobs';

import type { Beats } from './beats';
import type { Figure } from './body';
import type { NumberLayer } from './numbers';

const MOB_NUMBER_M = 0.25; // a number over a mob's head
const RESULT_WAIT_MS = 2000; // how long a mob's look waits past its act for the world to say it acted
const PLAYER_NUMBER_M = 0.75; // over a player's name

/** What the fight reads of the world: who's drawn where. */
export interface CombatDeps {
  /** The local player's character id. */
  readonly me: string;
  readonly now: () => number;
  readonly tickMs: number;
  /** Where r is drawn now; undefined when it isn't on this map. */
  body(r: Ref): Body | undefined;
  /** The figure drawn for r, which a hit-stop holds. */
  figure(r: Ref): Figure | null;
  /** A mob's type as drawn. */
  mobType(id: number): string | undefined;
  /** Where a mob stood at a tick, as the frames sent it, and its type. */
  mobAt(
    id: number,
    tick: number,
  ): { x: number; y: number; z: number; tick: number; type: string } | undefined;
  /** A mob's poses from the drawn one on, as the frames sent them. */
  mobPoses(id: number): readonly { readonly t: number; readonly pose: MobPose }[];
  /** When the frames took a mob off the map; undefined while it's on it. */
  mobGone(id: number): number | undefined;
  /** Whom the player's own heal at stage reaches, as the world picks: "p:<id>" refs. */
  healedBy(stage: Stage): string[];
}

/** An action's look as it plays: its targets, refs its Damage events add to till its ready point when it's open to them. */
interface Showing {
  readonly stop: Stop;
  readonly targets: string[];
  readonly open: boolean;
  readonly until: number;
}

/** A hit to show at `at`: its damage type as its source then acted, the beats of the shot it came by, and its target as last drawn. */
interface Landing {
  readonly d: Data<'damage'>;
  readonly target: Ref;
  readonly damage: Damage;
  readonly beats?: Beats;
  readonly at: number;
  last: Body | undefined;
}

/** A player's basic attack in the data: its damage, and a shot's range. */
function attackOf(cls: string): { readonly damage: Damage; readonly range?: number } {
  const a = (classes.find((c) => c.id === cls) ?? classes[0]).attack;
  return { damage: damageType(a.damage), ...(a.kind === 'shot' && { range: a.range }) };
}

export class Combat {
  private readonly lastDamage = new Map<string, Damage>(); // by source ref, from its latest action
  private readonly shots = new Map<string, Shot>(); // by caster, while their latest action is a drawn shot
  private landings: Landing[] = [];
  private readonly hitStops = new HitStops();
  private myBeats: Beats | null = null;
  private readonly showing = new Map<string, Showing>(); // by caster
  private readonly warmed = new WeakSet<Figure>();
  private readonly mobAttacks: MobAttacks;
  private readonly mobLooks = new Map<
    number,
    { readonly swing: Swing; readonly stop: Stop; acted: boolean }
  >();
  private spits: readonly SpitDrawn[] = [];

  constructor(
    private readonly deps: CombatDeps,
    private readonly fx: Fx,
    private readonly numbers: NumberLayer,
  ) {
    this.mobAttacks = new MobAttacks(deps.tickMs, fx.map.geometry, {
      placeAt: (id, tick) => deps.mobAt(id, tick),
      posesOf: (id) => deps.mobPoses(id),
      goneAt: (id) => deps.mobGone(id),
    });
  }

  /** A mob's windup at tick, heard as it began: drawn when the player's clock gets there. */
  mobAttacked(d: Data<'mobAttacked'>, tick: number): void {
    this.mobAttacks.heardWindup(tick, d.mob, d.attack, d.yaw, { x: d.toX, y: d.toY, z: d.toZ });
  }

  /**
   * The mobs' attacks over the mobs drawn at renderTime, which the world's clock reads as now: each windup's look and clip on the player's own clock, mine, so it ends as the world strikes them; its result once the world says it acted; a broken-off one's look ended, and the spits in flight.
   * Returns where the mobs a hop or a charge carries are drawn instead.
   */
  mobsDrawn(
    renderTime: number,
    mine: number,
    now: number,
    shown: ReadonlyMap<number, MobState>,
  ): ReadonlyMap<number, Carried> {
    const me = this.deps.body({ kind: 'player', id: this.deps.me });
    const drawn = this.mobAttacks.frame(renderTime, mine, shown, me);
    this.spits = drawn.spits;
    for (const [id, look] of this.mobLooks) {
      const flying = this.spits.some((s) => s.mob === id);
      if (drawn.swings.get(id) !== look.swing && !flying) {
        look.stop();
        this.mobLooks.delete(id);
      }
    }
    for (const [id, swing] of drawn.swings) {
      let look = this.mobLooks.get(id);
      if (look?.swing !== swing) {
        look?.stop();
        look = { swing, stop: this.mobLook(id, swing, now - (mine - swing.from)), acted: false };
        this.mobLooks.set(id, look);
      }
      const figure = this.deps.figure({ kind: 'mob', id });
      if (!look.acted && figure) {
        look.acted = true; // on its beats from the windup's start, however late its figure came
        figure.act('attack', mobBeats(swing, now - (mine - swing.from), this.deps.tickMs));
      }
    }
    return drawn.carried;
  }

  /** A player's basic attack from feet `at` along yaw: its look in place of their last, their damage type from now on, and a ranged one's shot, which flies on whatever they do next. */
  attack(caster: string, cls: string, beats: Beats, at: Body, yaw: number): void {
    const a = attackOf(cls);
    this.acted(caster, beats, a.damage, a.range, at);
    const o = this.options(caster, at, yaw, { beats });
    if (a.range === undefined) {
      this.look(caster, attackEffect(cls), o);
    } else {
      this.cutShort(caster);
      this.fx.play(attackEffect(cls), o);
    }
  }

  /** A player's skill at its stage from feet `at` along yaw: its look toward those it reaches, their damage type from now on, and a shot's flight. */
  skill(caster: string, skill: Skill, stage: Stage, beats: Beats, at: Body, yaw: number): void {
    this.acted(caster, beats, damageType(stage.damage), shotRange(skill, stage), at);
    const tier = Math.max(0, skill.stages.indexOf(stage));
    this.look(
      caster,
      skillEffect(skill.id),
      this.options(caster, at, yaw, { beats, tier }),
      this.reachedBy(caster, skill, stage),
    );
  }

  /** The player's own cancel leaves the pose it cut off behind, white and fading: the only cancel hint, and only after one. */
  afterimage(at: Point, yaw: number, figure: Figure | null): void {
    if (figure) {
      this.fx.play('cancel.afterimage', { ...at, yaw, copy: figure });
    }
  }

  /** Makes a figure's afterimage ahead, once, so the first cancel doesn't hitch. */
  ready(figure: Figure): void {
    if (!this.warmed.has(figure)) {
      this.warmed.add(figure);
      warmAfterimage(this.fx, figure);
    }
  }

  /** A hit, drawn only (HP comes with the frames): a shot's waits till its shot is drawn there; then its spark and number, and the player's own holds a moment. */
  damage(d: Data<'damage'>, tick: number): void {
    const from = parseRef(d.source);
    if (from?.kind === 'mob') {
      this.mobAttacks.struckBy(tick, from.id); // its spit ends on the one it hit
    }
    const target = parseRef(d.target);
    if (!target) {
      return;
    }
    const now = this.deps.now();
    const body = this.deps.body(target) ?? this.undrawn(target, tick);
    const show = from?.kind === 'player' ? this.showing.get(from.id) : undefined;
    if (body && show?.open && !d.miss && now < show.until && !show.targets.includes(d.target)) {
      show.targets.push(d.target); // a foe the action reached, for Arc, Volley and Fire Arrow to go to
    }
    const shot = from?.kind === 'player' ? this.shots.get(from.id) : undefined;
    const at = shot && body ? arrival(shot, body.x, body.z) : now;
    const hit = { d, target, damage: this.damageFrom(d.source), at, last: body };
    if (at > now) {
      this.landings.push({ ...hit, ...(shot && { beats: shot.beats }) });
      return;
    }
    this.land(hit, now);
  }

  /** A mob hit before it's drawn, where the frames put it at tick: one killed on the tick it spawned is never drawn. */
  private undrawn(target: Ref, tick: number): Body | undefined {
    const at = target.kind === 'mob' ? this.deps.mobAt(target.id, tick) : undefined;
    const m = at && mobById(at.type);
    return at && m && { x: at.x, y: at.y, z: at.z, radius: m.radius, height: m.height };
  }

  /** A potion's HP over the player who drank it. */
  restored(hp: number): void {
    const me = this.deps.body({ kind: 'player', id: this.deps.me });
    if (me && hp > 0) {
      this.numberOver(me, 'player', `+${hp}`, 'healHp');
    }
  }

  levelUp(): void {
    const me = this.deps.body({ kind: 'player', id: this.deps.me });
    if (me) {
      this.numberOver(me, 'player', 'LEVEL UP!', 'crit');
    }
  }

  /** Each frame: the shots that have reached their targets show their hits, and every effect steps. */
  frame(now: number): void {
    this.landings = this.landings.filter((l) => {
      l.last = this.deps.body(l.target) ?? l.last;
      if (now < l.at) {
        return true;
      }
      this.land(l, now);
      return false;
    });
    this.fx.update(now);
  }

  /** Another player gone from view: their look stops, and their latest damage type and shot go, so a long grind keeps none of them. */
  forget(id: string): void {
    this.cutShort(id);
    this.lastDamage.delete(`p:${id}`);
    this.shots.delete(id);
  }

  /** Everyone drawn has gone, as on a rejoin: their looks stop, and what was still to land is dropped. */
  clear(): void {
    for (const id of [...this.showing.keys()]) {
      this.cutShort(id);
    }
    for (const look of this.mobLooks.values()) {
      look.stop();
    }
    this.mobLooks.clear();
    this.mobAttacks.clear();
    this.spits = [];
    this.lastDamage.clear();
    this.shots.clear();
    this.landings = [];
    this.numbers.clear();
  }

  dispose(): void {
    this.clear();
    this.myBeats = null;
    this.fx.dispose();
  }

  /** A mob's attack's look from its windup's start in the world's clock: its area as fixed then, filling till its hit and held till its result, which its impact shows from. */
  private mobLook(id: number, swing: Swing, start: number): Stop {
    const a = swing.attack;
    const { hit, strikeEnd } = mobBeats(swing, start, this.deps.tickMs);
    const ref = { kind: 'mob', id } as const;
    const type = this.deps.mobType(id);
    const def = mobById(type);
    const flies = swing.reach ?? (a.kind === 'shot' ? a.range : 0);
    const flight = a.kind === 'shot' ? (flies / a.speed) * 1000 : 0;
    const readyAt = strikeEnd + IMPACT_MS;
    return this.fx.play(mobEffect(a.name), {
      ...swing.at,
      yaw: swing.yaw,
      size: { radius: def?.radius ?? 0.5, height: def?.height ?? 1 },
      reach: a.kind === 'front' ? a.reach : a.kind === 'shot' ? flies : a.spread,
      to: swing.to,
      width: a.kind === 'shot' ? a.size : 2 * (def?.radius ?? 0.5),
      beats: { start, hit, strikeEnd, readyAt, end: readyAt + flight + RESULT_WAIT_MS },
      landed: () => (swing.landed === undefined ? undefined : start + (swing.landed - swing.from)),
      follow: () => this.deps.body(ref),
      shots: () => this.spits.filter((s) => s.mob === id),
      ...(def?.look.tint != null && { color: def.look.tint }),
    });
  }

  /** A look's options: at the caster's feet along yaw, following them as they move, toward the bodies of those it reached. */
  private options(caster: string, at: Body, yaw: number, more: Partial<PlayOptions>): PlayOptions {
    return { ...at, yaw, ...more, follow: () => this.deps.body({ kind: 'player', id: caster }) };
  }

  /** Plays a player's action's look in place of their last one still showing, toward `reached`, or else the foes its hits reach. */
  private look(caster: string, id: EffectId, o: PlayOptions, reached?: readonly string[]): void {
    this.cutShort(caster);
    const targets = [...(reached ?? [])];
    const stop = this.fx.play(id, { ...o, targets: () => this.bodies(targets) });
    this.showing.set(caster, {
      stop,
      targets,
      open: reached === undefined,
      until: o.beats?.readyAt ?? -Infinity,
    });
  }

  /** A player's new action takes away the look of the one it cuts short. */
  private cutShort(caster: string): void {
    this.showing.get(caster)?.stop();
    this.showing.delete(caster);
  }

  /** Who a skill reaches as it starts: a heal those it finds, a buff none (its aura follows its caster); a damaging one's foes come with its hits. */
  private reachedBy(caster: string, skill: Skill, stage: Stage): string[] | undefined {
    if (skill.effect === 'buff') {
      return [];
    }
    if (skill.effect === 'heal') {
      return caster === this.deps.me ? this.deps.healedBy(stage) : [`p:${caster}`]; // another's party isn't known here
    }
    return undefined;
  }

  /** The bodies of refs drawn here now. */
  private bodies(refs: readonly string[]): Body[] {
    return refs.flatMap((r) => {
      const ref = parseRef(r);
      const b = ref && this.deps.body(ref);
      return b ? [b] : [];
    });
  }

  private acted(
    caster: string,
    beats: Beats,
    damage: Damage,
    range: number | undefined,
    at: Body,
  ): void {
    this.lastDamage.set(`p:${caster}`, damage);
    if (range === undefined) {
      this.shots.delete(caster);
    } else {
      this.shots.set(caster, { x: at.x, z: at.z, range, beats });
    }
    if (caster === this.deps.me) {
      this.myBeats = beats;
    }
  }

  /** A hit shown: the player's own holds their pose and the mob it struck; each sparks and pops a number at its target, drawn or last seen. */
  private land(l: Landing, now: number): void {
    const { d, target } = l;
    if (d.source === `p:${this.deps.me}` && !d.miss) {
      this.hitStop(l.beats ?? this.myBeats, target, now);
    }
    const body = this.deps.body(target) ?? l.last;
    if (!body) {
      return;
    }
    const from = parseRef(d.source);
    const source = from && this.deps.body(from);
    const yaw = source ? Math.atan2(body.x - source.x, body.z - source.z) : 0;
    const middle = { x: body.x, y: body.y + body.height / 2, z: body.z };
    const size = { radius: body.radius, height: body.height };
    for (const id of hitEffects(l.damage, d)) {
      this.fx.play(id, { ...middle, yaw, size });
    }
    const n = numberFor(d, target.kind === 'player' && target.id === this.deps.me);
    this.numberOver(body, target.kind, n.text, n.kind);
  }

  /** Holds the player's pose a moment, and the mob they struck: only for the action under way, so a shot landing after the next began holds nothing. */
  private hitStop(beats: Beats | null, target: Ref, now: number): void {
    if (!beats || beats !== this.myBeats) {
      return;
    }
    const held = this.hitStops.hit(now, beats.readyAt);
    if (!held) {
      return;
    }
    this.deps.figure({ kind: 'player', id: this.deps.me })?.hold(held);
    if (target.kind === 'mob') {
      this.deps.figure(target)?.hold({ from: now, until: held.until, readyAt: held.until });
    }
  }

  private numberOver(
    body: Body,
    kind: Ref['kind'],
    text: string,
    style: Parameters<NumberLayer['spawn']>[4],
  ): void {
    const above = kind === 'mob' ? MOB_NUMBER_M : PLAYER_NUMBER_M;
    this.numbers.spawn(body.x, body.y + body.height + above, body.z, text, style, this.deps.now());
  }

  /** The damage type of a hit from source: a mob type's own, else the player's latest action's, else physical. */
  private damageFrom(source: string): Damage {
    const r = parseRef(source);
    if (r?.kind === 'mob') {
      const type = this.deps.mobType(r.id);
      return damageType(mobById(type)?.damage ?? null);
    }
    return this.lastDamage.get(source) ?? 'physical';
  }
}

/** A player's body drawn with feet at (x, y, z). */
export function playerBody(x: number, y: number, z: number): Body {
  return { x, y, z, radius: movement.radius, height: movement.height };
}

/** A mob attack's beats from start: its windup to the hit, its travel, then its recovery. */
/** A mob's attack's beats from start in the world's clock, in whole ticks as the world counts them. */
function mobBeats(s: Swing, start: number, tickMs: number): Beats {
  const hit = start + (s.hit - s.from);
  const strikeEnd = start + (s.end - s.from);
  const ready = strikeEnd + ticksIn(s.attack.recover_ms, tickMs) * tickMs;
  return { start, hit, strikeEnd, readyAt: ready, end: ready };
}
