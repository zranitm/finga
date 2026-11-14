// The player's own action as the world times it, in the same whole ticks. A press goes when the world would let it start, so one sent early only goes later on the server, never faster.
import { classes } from '@/gamedata/classes';
import { ticksIn } from '@/lib/fixed-step';
import { actionPoints } from '@/progression/actions';
import type { Stage } from '@/progression/skills';
import type { StatSheet } from '@/protocol/messages';

export type ActionKind = 'attack' | 'skill';

/** An action in whole ticks, its hit and cancel point as shares of them, and whether the other kind may cut it short. */
export interface ActionTiming {
  readonly kind: ActionKind;
  readonly ticks: number;
  readonly hit: number;
  readonly cancel: number;
  readonly cancelable: boolean;
}

/** The basic attack: the sheet's length, or the class's in whole ticks before a sheet arrives. Every basic attack can be cut short by a skill. */
export function attackTiming(
  sheet: StatSheet | undefined,
  cls: string,
  tickMs: number,
): ActionTiming {
  const sent = sheet?.attackLengthTicks ?? 0;
  return attackIn(cls, sent > 0 ? sent : ticksIn(attackOf(cls).length_ms, tickMs));
}

/** A basic attack of the class lasting ticks, as another player's AttackUsed tells its length. */
export function attackIn(cls: string, ticks: number): ActionTiming {
  const { hit, cancel } = attackOf(cls);
  return { kind: 'attack', ticks, hit, cancel, cancelable: true };
}

/** The class's basic attack, or the first class's for one this client doesn't know. */
export function attackOf(cls: string): (typeof classes)[number]['attack'] {
  return (classes.find((c) => c.id === cls) ?? classes[0]).attack;
}

export function skillTiming(stage: Stage, tickMs: number): ActionTiming {
  return {
    kind: 'skill',
    ticks: ticksIn(stage.length_ms, tickMs),
    hit: stage.hit,
    cancel: stage.cancel,
    cancelable: stage.cancelable,
  };
}

interface Running {
  readonly kind: ActionKind;
  readonly cancelable: boolean;
  readonly cancelAt: number;
  readonly end: number;
}

export class ActionClock {
  private running: Running | null = null;
  /** The latest attack until the world's count of it comes: its own AttackUsed, which follows the Delta with its energy. */
  private uncounted: Running | null = null;
  /** A press held for its moment or for that count, and its kind; a newer press takes its place, but an attack never a skill's. */
  private held: {
    timer: ReturnType<typeof setTimeout>;
    kind: ActionKind;
    again: () => void;
    count: boolean;
  } | null = null;
  /** A held attack key's next swing, at the moment it waits for. */
  private repeat: { timer: ReturnType<typeof setTimeout>; at: number } | null = null;

  /** onCancel runs as an action starts that cuts the other kind short, before the cut-off pose is drawn over. */
  constructor(
    private readonly tickMs: number,
    private readonly now: () => number,
    private readonly onCancel: () => void,
  ) {}

  /** Whether an action of kind may start now: no action runs, or it may cut the running one off. */
  canStart(kind: ActionKind): boolean {
    return this.startAt(kind) <= this.now();
  }

  /** Whether a press waits for its moment. */
  waiting(): boolean {
    return this.held !== null;
  }

  /** Whether a fresh press of kind goes now. One that can't is held, and pressed again at its moment (startAt). While a skill press waits, an attack press does nothing, so the skill goes first. */
  press(kind: ActionKind, again: () => void): boolean {
    if (kind === 'attack' && this.held?.kind === 'skill') {
      return false;
    }
    this.stop();
    const wait = this.startAt(kind) - this.now();
    if (wait <= 0) {
      return true;
    }
    this.hold(kind, wait, again, false);
    return false;
  }

  /** Whether a skill press short of energy waits for the world's count of the latest attack: held, it goes again as the count comes or the attack ends, whichever is first. */
  awaitCount(again: () => void): boolean {
    const a = this.uncounted;
    const now = this.now();
    if (!a || now >= a.end) {
      return false;
    }
    this.stop();
    this.hold('skill', a.end - now, again, true);
    return true;
  }

  /** The world counted the latest attack: a press waiting for the count goes again now. */
  counted(): void {
    this.uncounted = null;
    const h = this.held;
    if (h?.count) {
      this.stop();
      h.again();
    }
  }

  private hold(kind: ActionKind, wait: number, again: () => void, count: boolean): void {
    this.held = {
      kind,
      again,
      count,
      timer: setTimeout(() => {
        this.held = null;
        again();
      }, Math.ceil(wait)),
    };
  }

  /** A held attack key's next swing: go runs as an attack may next start, at a cancelable skill's cancel point or else as the running action ends, once for that moment, not a frame later. */
  repeatWhenFree(go: () => void): void {
    if (!this.running) {
      return;
    }
    const at = this.startAt('attack');
    if (this.repeat?.at === at) {
      return;
    }
    this.stopRepeat();
    this.repeat = {
      at,
      timer: setTimeout(
        () => {
          this.repeat = null;
          go();
        },
        Math.max(0, Math.ceil(at - this.now())),
      ),
    };
  }

  /** When an action of kind may start: now with none running; the running one's cancel point, or now once past it, if kind may cut it off, whenever it was pressed; else its end. */
  private startAt(kind: ActionKind): number {
    const now = this.now();
    const a = this.running;
    if (!a || now >= a.end) {
      return now;
    }
    if (a.kind !== kind && a.cancelable) {
      return Math.max(now, a.cancelAt);
    }
    return a.end;
  }

  /** Starts an action now, cutting short the one running; the returned undo gives it back if none started since. */
  begin(t: ActionTiming): () => void {
    const now = this.now();
    const before = this.running;
    if (before && before.kind !== t.kind && now < before.end) {
      this.onCancel();
    }
    const started: Running = {
      kind: t.kind,
      cancelable: t.cancelable,
      cancelAt: now + actionPoints(t.ticks, t.hit, t.cancel).cancel * this.tickMs,
      end: now + t.ticks * this.tickMs,
    };
    this.running = started;
    if (t.kind === 'attack') {
      this.uncounted = started;
    }
    return () => {
      if (this.running === started) {
        this.running = before;
      }
    };
  }

  /** Forgets a held press. */
  private stop(): void {
    if (this.held !== null) {
      clearTimeout(this.held.timer);
    }
    this.held = null;
  }

  private stopRepeat(): void {
    if (this.repeat !== null) {
      clearTimeout(this.repeat.timer);
    }
    this.repeat = null;
  }

  /** No action, no held press and no repeat: at a death, a rejoin or a new map. */
  reset(): void {
    this.running = null;
    this.uncounted = null;
    this.stop();
    this.stopRepeat();
  }
}
