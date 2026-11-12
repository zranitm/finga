// The client never computes damage, hit chance or EXP; it shows what the server sent.
import { classes } from '@/gamedata/classes';
import { ticksIn } from '@/lib/fixed-step';
import type { Data, StatSheet } from '@/protocol/messages';

import { RemoteBuffer } from './interpolation';
import { wireState } from './prediction';

export type Ref =
  { readonly kind: 'player'; readonly id: string } | { readonly kind: 'mob'; readonly id: number };

/** "p:<character id>" or "m:<mob id>". */
export function parseRef(ref: string): Ref | undefined {
  if (ref.startsWith('p:') && ref.length > 2) {
    return { kind: 'player', id: ref.slice(2) };
  }
  if (ref.startsWith('m:')) {
    const id = Number(ref.slice(2));
    if (Number.isInteger(id) && id >= 0) {
      return { kind: 'mob', id };
    }
  }
  return undefined;
}

export type MobWire = Data<'snapshot'>['mobs'][number];

export interface MobState {
  readonly type: string;
  readonly x: number;
  readonly y: number;
  readonly facing: 1 | -1;
  readonly hp: number;
  readonly maxHp: number;
}

export class MobTracker {
  private readonly buffers = new Map<number, RemoteBuffer>();
  private readonly info = new Map<number, { type: string; hp: number; maxHp: number }>();

  constructor(private readonly tickMs: number) {}

  applySnapshot(t: number, mobs: readonly MobWire[]): void {
    const seen = new Set<number>();
    for (const m of mobs) {
      seen.add(m.id);
      this.put(t, m);
    }
    for (const id of [...this.buffers.keys()]) {
      if (!seen.has(id)) {
        this.buffers.delete(id);
        this.info.delete(id);
      }
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
    }
  }

  private put(t: number, m: MobWire): void {
    let b = this.buffers.get(m.id);
    if (!b) {
      b = new RemoteBuffer(this.tickMs);
      this.buffers.set(m.id, b);
    }
    b.push(
      t,
      wireState({
        x: m.x,
        y: m.y,
        vx: 0,
        vy: 0,
        facing: m.facing,
        onGround: true,
        climbing: false,
      }),
    );
    this.info.set(m.id, { type: m.type, hp: m.hp, maxHp: m.maxHp });
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
        shown.set(id, {
          type: i.type,
          x: s.x,
          y: s.y,
          facing: s.facing,
          hp: i.hp,
          maxHp: i.maxHp,
        });
      }
    }
    return { shown, gone };
  }

  get size(): number {
    return this.buffers.size;
  }

  clear(): void {
    this.buffers.clear();
    this.info.clear();
  }
}

export type NumberKind = 'hit' | 'crit' | 'miss' | 'taken' | 'healHp' | 'healMp';

export interface DamageNumber {
  readonly id: number;
  readonly x: number;
  readonly y: number;
  readonly text: string;
  readonly kind: NumberKind;
  readonly bornAt: number;
}

export interface DrawnNumber extends DamageNumber {
  readonly dy: number;
  readonly alpha: number;
}

export const NUMBER_LIFE_MS = 800;
const RISE_PX = 40;

export function numberFor(
  d: { readonly amount: number; readonly crit?: boolean; readonly miss?: boolean },
  targetIsMe: boolean,
): { text: string; kind: NumberKind } {
  if (d.miss) {
    return { text: 'MISS', kind: 'miss' };
  }
  if (targetIsMe) {
    return { text: `-${d.amount}`, kind: 'taken' };
  }
  return d.crit ? { text: `${d.amount}!`, kind: 'crit' } : { text: String(d.amount), kind: 'hit' };
}

export class DamageNumbers {
  private items: DamageNumber[] = [];
  private nextId = 1;

  spawn(x: number, y: number, text: string, kind: NumberKind, now: number): DamageNumber {
    const n: DamageNumber = { id: this.nextId++, x, y, text, kind, bornAt: now };
    this.items.push(n);
    return n;
  }

  update(now: number): DrawnNumber[] {
    this.items = this.items.filter((n) => now - n.bornAt < NUMBER_LIFE_MS);
    return this.items.map((n) => {
      const f = (now - n.bornAt) / NUMBER_LIFE_MS;
      return { ...n, dy: 0 - RISE_PX * f, alpha: f < 0.5 ? 1 : 1 - (f - 0.5) * 2 }; // 0 - 0 is +0
    });
  }

  get size(): number {
    return this.items.length;
  }

  clear(): void {
    this.items = [];
  }
}

function classCooldownMs(cls: string): number {
  return (classes.find((c) => c.id === cls) ?? classes[0]).attack.cooldown_ms;
}

/** The sheet's count, which attack speed shortens; the class's cooldown in whole ticks before a sheet arrives. */
export function attackCooldownTicks(
  sheet: StatSheet | undefined,
  cls: string,
  tickMs: number,
): number {
  const ticks = sheet?.attackCooldownTicks ?? 0;
  return ticks > 0 ? ticks : ticksIn(classCooldownMs(cls), tickMs);
}

/** Only stops a held key spamming the server, which enforces the cooldown itself; it counts the server's whole ticks, so an attack the gate lets through never lands in it. */
export class AttackGate {
  private readyAt = Number.NEGATIVE_INFINITY;

  constructor(
    private cooldownTicks: number,
    private readonly tickMs: number,
    private readonly send: () => boolean,
  ) {}

  /** Attack speed changed: the next attack after this waits the new count. */
  setCooldownTicks(ticks: number): void {
    this.cooldownTicks = ticks;
  }

  tryAttack(now: number): boolean {
    if (now < this.readyAt) {
      return false;
    }
    this.readyAt = now + this.cooldownTicks * this.tickMs;
    return this.send();
  }

  /** How long until the next action's turn, 0 when it's now. */
  readyIn(now: number): number {
    return Math.max(0, this.readyAt - now);
  }

  /** A skill takes the next attack's turn, counted from the last turn's end as the server counts it; the returned undo gives it back if nothing took a turn since. */
  take(now: number): () => void {
    const before = this.readyAt;
    const after = Math.max(before, now) + this.cooldownTicks * this.tickMs;
    this.readyAt = after;
    return () => {
      if (this.readyAt === after) {
        this.readyAt = before;
      }
    };
  }

  reset(): void {
    this.readyAt = Number.NEGATIVE_INFINITY;
  }
}

/** Follows the server's invulnerable flag, not a local timer. */
export class InvulnFlash {
  private on = false;
  private since = 0;

  set(on: boolean, now: number): void {
    if (on && !this.on) {
      this.since = now;
    }
    this.on = on;
  }

  get active(): boolean {
    return this.on;
  }

  alpha(now: number): number {
    if (!this.on) {
      return 1;
    }
    return Math.floor((now - this.since) / 100) % 2 === 0 ? 0.4 : 1;
  }
}
