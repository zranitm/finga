import { clone } from '@bufbuild/protobuf';

import type { Data, MobStep, PlayerStep } from './messages';
import { MobViewSchema, PlayerViewSchema } from './messages';

type PlayerWire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];

/** Whole numbers per metre, per metre a second, per unit of a heading's part and per radian: the server's quant package. */
const PER_METRE = 100;
const PER_SPEED = 1000;
const PER_HEADING = 1000;
const PER_YAW = 1000;

/** Rounded to the nearest whole, halves toward +infinity, as the server rounds the baseline each step runs from. */
export const whole = (v: number): number => Math.floor(v + 0.5);
export const position = (metres: number): number => whole(metres * PER_METRE);

/** The views a frame's steps say, kept whole by slot and id: a step moves the last view held of its body. */
export class ViewExpander {
  private readonly players = new Map<number, PlayerWire>();
  private readonly slots = new Map<string, number>();
  private readonly mobs = new Map<number, MobWire>();
  private mySlot = 0;

  constructor(private readonly me: string) {}

  /** A map's join: the views the steps after it run from. */
  snapshot(players: readonly PlayerWire[], mobs: readonly MobWire[]): void {
    this.players.clear();
    this.slots.clear();
    this.mobs.clear();
    this.mySlot = 0;
    for (const p of players) {
      this.hold(p);
    }
    for (const m of mobs) {
      this.mobs.set(m.id, m);
    }
  }

  /** A frame's players and mobs as whole views: those sent whole, then each step applied; the player's own steps are left out, their own view comes whole. */
  delta(d: Data<'delta'>): { players: PlayerWire[]; mobs: MobWire[] } {
    const players = [...d.players];
    const mobs = [...d.mobs];
    for (const p of d.players) {
      this.hold(p);
    }
    for (const m of d.mobs) {
      this.mobs.set(m.id, m);
    }
    for (const s of d.steps) {
      const held = this.players.get(s.slot);
      if (held && !(s.slot === this.mySlot && this.mySlot !== 0)) {
        const next = steppedPlayer(held, s);
        this.players.set(s.slot, next);
        players.push(next);
      }
    }
    for (const s of d.mobSteps) {
      const held = this.mobs.get(s.id);
      if (held) {
        const next = steppedMob(held, s);
        this.mobs.set(s.id, next);
        mobs.push(next);
      }
    }
    for (const id of d.removedMobs) {
      this.mobs.delete(id);
    }
    for (const id of [...d.removedPlayers, ...(d.unseen?.players ?? [])]) {
      this.forget(id);
    }
    for (const id of d.unseen?.mobs ?? []) {
      this.mobs.delete(id);
    }
    return { players, mobs };
  }

  private hold(p: PlayerWire): void {
    this.players.set(p.slot, p);
    this.slots.set(p.id, p.slot);
    if (p.id === this.me) {
      this.mySlot = p.slot;
    }
  }

  private forget(id: string): void {
    const slot = this.slots.get(id);
    if (slot !== undefined && id !== this.me) {
      this.players.delete(slot);
      this.slots.delete(id);
    }
  }
}

function steppedPlayer(v: PlayerWire, s: PlayerStep): PlayerWire {
  const out = clone(PlayerViewSchema, v);
  out.x = (position(v.x) + s.dx) / PER_METRE;
  out.y = (position(v.y) + s.dy) / PER_METRE;
  out.z = (position(v.z) + s.dz) / PER_METRE;
  if (s.vx !== undefined) out.vx = s.vx / PER_SPEED;
  if (s.vy !== undefined) out.vy = s.vy / PER_SPEED;
  if (s.vz !== undefined) out.vz = s.vz / PER_SPEED;
  if (s.hx !== undefined) out.hx = s.hx / PER_HEADING;
  if (s.hz !== undefined) out.hz = s.hz / PER_HEADING;
  if (s.onGround !== undefined) out.onGround = s.onGround;
  if (s.hp !== undefined) out.hp = s.hp;
  if (s.sprint !== undefined) out.sprint = s.sprint;
  if (s.held !== undefined) out.held = s.held;
  if (s.aim !== undefined) out.aim = s.aim;
  return out;
}

function steppedMob(v: MobWire, s: MobStep): MobWire {
  const out = clone(MobViewSchema, v);
  out.x = (position(v.x) + s.dx) / PER_METRE;
  out.y = (position(v.y) + s.dy) / PER_METRE;
  out.z = (position(v.z) + s.dz) / PER_METRE;
  if (s.yaw !== undefined) out.yaw = Math.fround(s.yaw / PER_YAW);
  if (s.hp !== undefined) out.hp = s.hp;
  if (s.pose !== undefined) out.pose = s.pose;
  return out;
}
