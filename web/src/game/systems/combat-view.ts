// What a hit shows (combat-effects.md section 4): who it came from and struck, its number and how numbers stack on the screen, and the player's blink while invulnerable. The client never computes damage, hit chance or EXP; it shows what the server sent.

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

export type NumberKind = 'hit' | 'crit' | 'miss' | 'taken' | 'healHp' | 'dot' | 'shield';

/** A number on the screen: where it was asked to start and where it starts, a line or more above when it stacks, in CSS pixels. */
export interface DamageNumber {
  readonly id: number;
  readonly x: number;
  readonly y: number;
  readonly text: string;
  readonly kind: NumberKind;
  readonly bornAt: number;
  /** How far above where it was asked to start it starts. */
  readonly lift: number;
}

export interface DrawnNumber extends DamageNumber {
  readonly dy: number;
  readonly alpha: number;
  readonly scale: number;
}

const NUMBER_LIFE_MS = 800;
const RISE_PX = 40;
const POP_MS = 120; // a number pops from POP_FROM times its size to its size, from the frame that first draws it
const POP_FROM = 1.4;
const STACK_MS = 300; // a number this soon after others in its spot starts a line above the highest of them
const STACK_PX = 24; // a spot: starts within this across
const LINE_PX = 16;
const STACK_LINES = 2; // and this many lines up or down

const rise = (n: DamageNumber, now: number) => 0 - (RISE_PX * (now - n.bornAt)) / NUMBER_LIFE_MS; // 0 - 0 is +0

/** How much bigger than its size a number is, ms after it was first drawn: an even ease from POP_FROM to 1, so the pop shows on every frame of it. */
const pop = (ms: number) =>
  ms < POP_MS ? ((POP_FROM - 1) * (1 + Math.cos((Math.PI * ms) / POP_MS))) / 2 : 0;

/** A hit's numbers: what came off HP, then what a shield took, if any; a blow a shield took whole shows only the shield's. */
export function numbersFor(
  d: {
    readonly amount: number;
    readonly crit?: boolean;
    readonly miss?: boolean;
    readonly dodge?: boolean;
    readonly shielded?: number;
  },
  targetIsMe: boolean,
): { text: string; kind: NumberKind }[] {
  if (d.dodge) {
    return [{ text: 'DODGE', kind: 'miss' }]; // where MISS would show
  }
  if (d.miss) {
    return [{ text: 'MISS', kind: 'miss' }];
  }
  const shielded = d.shielded ?? 0;
  const hp = d.amount - shielded;
  const out: { text: string; kind: NumberKind }[] = [];
  if (hp > 0 || shielded === 0) {
    if (targetIsMe) {
      out.push({ text: `-${hp}`, kind: 'taken' });
    } else {
      out.push(d.crit ? { text: `${hp}!`, kind: 'crit' } : { text: String(hp), kind: 'hit' });
    }
  }
  if (shielded > 0) {
    out.push({ text: targetIsMe ? `-${shielded}` : String(shielded), kind: 'shield' });
  }
  return out;
}

/** A number as the queue keeps it: the y it was asked to start at, before stacking, and when it was first drawn. */
interface Queued {
  readonly n: DamageNumber;
  readonly asked: number;
  drawnAt?: number;
}

export class DamageNumbers {
  private items: Queued[] = [];
  private nextId = 1;

  /** A number asked to start at screen point (x, y), or a line above the highest of those under 300 ms old asked for the same spot, as they're drawn now: piled foes' numbers stack too. */
  spawn(x: number, y: number, text: string, kind: NumberKind, now: number): DamageNumber {
    let top = y;
    for (const { n: o, asked } of this.items) {
      const spot = Math.abs(o.x - x) <= STACK_PX && Math.abs(asked - y) <= STACK_LINES * LINE_PX;
      if (spot && now - o.bornAt < STACK_MS) {
        top = Math.min(top, o.y + rise(o, now) - LINE_PX);
      }
    }
    const n: DamageNumber = {
      id: this.nextId++,
      x,
      y: top,
      text,
      kind,
      bornAt: now,
      lift: y - top,
    };
    this.items.push({ n, asked: y });
    return n;
  }

  update(now: number): DrawnNumber[] {
    this.items = this.items.filter(({ n }) => now - n.bornAt < NUMBER_LIFE_MS);
    return this.items.map((q) => {
      const f = (now - q.n.bornAt) / NUMBER_LIFE_MS;
      q.drawnAt ??= now;
      return {
        ...q.n,
        dy: rise(q.n, now),
        alpha: f < 0.5 ? 1 : 1 - (f - 0.5) * 2,
        scale: 1 + pop(now - q.drawnAt),
      };
    });
  }

  clear(): void {
    this.items = [];
  }
}

const BLINK_MS = 100;

/** The player's blink while invulnerable after a hit: it follows the server's flag, not a local timer. */
export class InvulnFlash {
  private on = false;
  private since = 0;

  set(on: boolean, now: number): void {
    if (on && !this.on) {
      this.since = now;
    }
    this.on = on;
  }

  /** Whether the player is drawn at now: hidden for the first 100 ms of every 200 while invulnerable. */
  shown(now: number): boolean {
    return !this.on || Math.floor((now - this.since) / BLINK_MS) % 2 === 1;
  }
}
