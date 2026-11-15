// The fight as drawn (combat-effects.md section 4): each hit's spark and number where its target is drawn, a shot's once it visibly lands, and the player's own hits held a moment.
import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';
import type { Skill, Stage } from '@/progression/skills';
import type { Data } from '@/protocol/messages';

import { arrival, shotRange } from '../effects/flight';
import type { Shot } from '../effects/flight';
import type { Fx } from '../effects/fx';
import { damageType, hitEffects } from '../effects/ids';
import type { Damage } from '../effects/ids';
import type { Body } from '../effects/types';
import { numberFor, parseRef } from '../systems/combat-view';
import type { Ref } from '../systems/combat-view';
import { HitStops } from '../systems/hit-stop';

import type { Beats } from './beats';
import type { Figure } from './body';
import type { NumberLayer } from './numbers';

const MOB_NUMBER_M = 0.25; // a number over a mob's head
const PLAYER_NUMBER_M = 0.75; // over a player's name

/** What the fight reads of the world: who's drawn where. */
export interface CombatDeps {
  /** The local player's character id. */
  readonly me: string;
  readonly now: () => number;
  /** Where r is drawn now; undefined when it isn't on this map. */
  body(r: Ref): Body | undefined;
  /** The figure drawn for r, which a hit-stop holds. */
  figure(r: Ref): Figure | null;
  /** A mob's type as drawn. */
  mobType(id: number): string | undefined;
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

  constructor(
    private readonly deps: CombatDeps,
    private readonly fx: Fx,
    private readonly numbers: NumberLayer,
  ) {}

  /** A player's basic attack from feet `at`: their damage type from now on, and a ranged one's shot. */
  attack(caster: string, cls: string, beats: Beats, at: Body): void {
    const a = attackOf(cls);
    this.acted(caster, beats, a.damage, a.range, at);
  }

  /** A player's skill at its stage from feet `at`: its damage type from now on, and a shot's flight. */
  skill(caster: string, skill: Skill, stage: Stage, beats: Beats, at: Body): void {
    this.acted(caster, beats, damageType(stage.damage), shotRange(skill, stage), at);
  }

  /** A hit, drawn only (HP comes with the frames): a shot's waits till its shot is drawn there; then its spark and number, and the player's own holds a moment. */
  damage(d: Data<'damage'>): void {
    const target = parseRef(d.target);
    if (!target) {
      return;
    }
    const from = parseRef(d.source);
    const now = this.deps.now();
    const body = this.deps.body(target);
    const shot = from?.kind === 'player' ? this.shots.get(from.id) : undefined;
    const at = shot && body ? arrival(shot, body.x, body.z) : now;
    const hit = { d, target, damage: this.damageFrom(d.source), at, last: body };
    if (at > now) {
      this.landings.push({ ...hit, ...(shot && { beats: shot.beats }) });
      return;
    }
    this.land(hit, now);
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

  /** Another player gone from view: their latest damage type and shot go, so a long grind keeps none of them. */
  forget(id: string): void {
    this.lastDamage.delete(`p:${id}`);
    this.shots.delete(id);
  }

  /** Everyone drawn has gone, as on a rejoin: what was still to land is dropped. */
  clear(): void {
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
      return damageType(mobs.find((m) => m.id === type)?.damage ?? null);
    }
    return this.lastDamage.get(source) ?? 'physical';
  }
}

/** A player's body drawn with feet at (x, y, z). */
export function playerBody(x: number, y: number, z: number): Body {
  return { x, y, z, radius: movement.radius, height: movement.height };
}
