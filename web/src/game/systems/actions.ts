// The player's own action as the world times it, in the same whole ticks. A press goes when the world would let it start, so one sent early only goes later on the server, never faster.
import { actions } from '@/gamedata/actions';
import { classes } from '@/gamedata/classes';
import { actionPoints, attackLengthTicks, skillLengthTicks } from '@/progression/actions';
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

/** The basic attack: the sheet's length, which attack speed shortens, or the class's in whole ticks before a sheet arrives. Every basic attack can be cut short by a skill. */
export function attackTiming(
  sheet: StatSheet | undefined,
  cls: string,
  tickMs: number,
): ActionTiming {
  const { attack } = classes.find((c) => c.id === cls) ?? classes[0];
  const sent = sheet?.attackLengthTicks ?? 0;
  const ticks = sent > 0 ? sent : attackLengthTicks(attack.length_ms, 0, tickMs);
  return { kind: 'attack', ticks, hit: attack.hit, cancel: attack.cancel, cancelable: true };
}

export function skillTiming(stage: Stage, tickMs: number): ActionTiming {
  return {
    kind: 'skill',
    ticks: skillLengthTicks(stage.length_ms, tickMs),
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
  /** A press held for its moment, and its kind; a newer press takes its place, but an attack never a skill's. */
  private held: { timer: ReturnType<typeof setTimeout>; kind: ActionKind } | null = null;
  /** A held attack key's next swing, at the end it waits for. */
  private repeat: { timer: ReturnType<typeof setTimeout>; at: number } | null = null;

  constructor(
    private readonly tickMs: number,
    private readonly now: () => number,
  ) {}

  /** No action runs: a held attack key repeats from here. */
  idle(): boolean {
    return !this.running || this.now() >= this.running.end;
  }

  /** Whether a press waits for its moment. */
  waiting(): boolean {
    return this.held !== null;
  }

  /** Whether a fresh press of kind goes now. One that can't is held, and pressed again at its moment: the cancel point if it's of the other kind and comes from early_press before it, else the end. While a skill press waits, an attack press does nothing, so the skill goes first. */
  press(kind: ActionKind, again: () => void): boolean {
    if (kind === 'attack' && this.held?.kind === 'skill') {
      return false;
    }
    this.stop();
    const wait = this.startAt(kind) - this.now();
    if (wait <= 0) {
      return true;
    }
    this.held = {
      kind,
      timer: setTimeout(() => {
        this.held = null;
        again();
      }, Math.ceil(wait)),
    };
    return false;
  }

  /** A held attack key's next swing: go runs as the running action ends, once for that end, not a frame later. */
  repeatAtEnd(go: () => void): void {
    const end = this.running?.end;
    if (end === undefined || this.repeat?.at === end) {
      return;
    }
    this.stopRepeat();
    this.repeat = {
      at: end,
      timer: setTimeout(
        () => {
          this.repeat = null;
          go();
        },
        Math.max(0, Math.ceil(end - this.now())),
      ),
    };
  }

  private startAt(kind: ActionKind): number {
    const now = this.now();
    const a = this.running;
    if (!a || now >= a.end) {
      return now;
    }
    if (a.kind !== kind && a.cancelable && now >= a.cancelAt - actions.early_press_ms) {
      return Math.max(now, a.cancelAt);
    }
    return a.end;
  }

  /** Starts an action now, cutting short the one running; the returned undo gives it back if none started since. */
  begin(t: ActionTiming): () => void {
    const now = this.now();
    const before = this.running;
    const started: Running = {
      kind: t.kind,
      cancelable: t.cancelable,
      cancelAt: now + actionPoints(t.ticks, t.hit, t.cancel).cancel * this.tickMs,
      end: now + t.ticks * this.tickMs,
    };
    this.running = started;
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
    this.stop();
    this.stopRepeat();
  }
}
