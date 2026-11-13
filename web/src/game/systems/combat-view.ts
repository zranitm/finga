// The client never computes damage, hit chance or EXP; it shows what the server sent.
import { mobs as mobData } from '@/gamedata/mobs';
import type { Data } from '@/protocol/messages';
import type { Rect } from '@/sim/step';

import { RemoteBuffer } from './interpolation';
import { HISTORY_MS, wireState } from './prediction';

const BOXES_KEEP_MS = HISTORY_MS + 500; // a replayed input's view is at most this far behind the newest tick

const sizes = new Map<string, { w: number; h: number }>(
  mobData.map((m) => [m.id, { w: m.width, h: m.height }]),
);

/** A mob's positions by tick, as the frames sent them, and the tick it left: what inputs were predicted against. */
interface Track {
  readonly type: string;
  readonly at: { tick: number; x: number; y: number }[];
  gone: number | null;
}

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
  private readonly tracks = new Map<number, Track>();
  private newest = 0;

  constructor(private readonly tickMs: number) {}

  applySnapshot(t: number, mobs: readonly MobWire[]): void {
    this.tracks.clear(); // a new map or a rejoin: its predictions start over too
    this.newest = 0;
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
      const tr = this.tracks.get(id);
      if (tr) {
        tr.gone = this.tickOf(t);
      }
    }
    this.prune();
  }

  /** The mobs' boxes as view's frame left them, which the world blocks inputs at view against. */
  boxesAt(view: number): Rect[] {
    const boxes: Rect[] = [];
    for (const tr of this.tracks.values()) {
      const size = sizes.get(tr.type);
      const s = tr.at.findLast((a) => a.tick <= view);
      if (!size || !s || (tr.gone !== null && tr.gone <= view)) {
        continue;
      }
      boxes.push({ x: s.x - size.w / 2, y: s.y - size.h, w: size.w, h: size.h });
    }
    return boxes;
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
    } else if (!last || tick > last.tick) {
      tr.at.push({ tick, x: m.x, y: m.y });
    }
  }

  /** Keeps BOXES_KEEP_MS of ticks, and each mob's last position before them as its floor. */
  private prune(): void {
    const cutoff = this.newest - Math.ceil(BOXES_KEEP_MS / this.tickMs);
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
        casting: 0,
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
    this.tracks.clear();
    this.newest = 0;
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
  readonly scale: number;
}

export const NUMBER_LIFE_MS = 800;
const RISE_PX = 40;
const POP_MS = 120; // a number pops from POP_FROM times its size to its size, easing back
const POP_FROM = 1.4;
const STACK_MS = 300; // a number this soon after another on its target starts a line above it
const LINE_PX = 16;

const rise = (n: DamageNumber, now: number) => 0 - (RISE_PX * (now - n.bornAt)) / NUMBER_LIFE_MS; // 0 - 0 is +0

/** Back-out easing: past 1 a little, then settling on it. */
function backOut(t: number): number {
  const c = 1.70158;
  return 1 + (c + 1) * (t - 1) ** 3 + c * (t - 1) ** 2;
}

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
  private readonly latest = new Map<string, DamageNumber>(); // each target's newest number

  /** A number at (x, y), or for a target (on) with one under 300 ms old, a line above that one as it's drawn now. */
  spawn(
    x: number,
    y: number,
    text: string,
    kind: NumberKind,
    now: number,
    on?: string,
  ): DamageNumber {
    const last = on === undefined ? undefined : this.latest.get(on);
    const top = last && now - last.bornAt < STACK_MS ? last.y + rise(last, now) - LINE_PX : y;
    const n: DamageNumber = { id: this.nextId++, x, y: Math.min(y, top), text, kind, bornAt: now };
    this.items.push(n);
    if (on !== undefined) {
      this.latest.set(on, n);
    }
    return n;
  }

  update(now: number): DrawnNumber[] {
    this.items = this.items.filter((n) => now - n.bornAt < NUMBER_LIFE_MS);
    for (const [on, n] of this.latest) {
      if (now - n.bornAt >= STACK_MS) {
        this.latest.delete(on);
      }
    }
    return this.items.map((n) => {
      const age = now - n.bornAt;
      const f = age / NUMBER_LIFE_MS;
      const pop = age < POP_MS ? (POP_FROM - 1) * (1 - backOut(age / POP_MS)) : 0;
      return { ...n, dy: rise(n, now), alpha: f < 0.5 ? 1 : 1 - (f - 0.5) * 2, scale: 1 + pop };
    });
  }

  get size(): number {
    return this.items.length;
  }

  clear(): void {
    this.items = [];
    this.latest.clear();
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
