import { WHOLE_TICKS } from '@/lib/fixed-step';

/** Where an action's hit lands and its cancel point falls, as ticks from its start. */
export interface ActionPoints {
  readonly hit: number;
  readonly cancel: number;
}

/** ⌊length × share⌋ for the hit and the cancel point, the cancel point held to at least the hit and at least one tick, as the server counts them. */
export function actionPoints(length: number, hit: number, cancel: number): ActionPoints {
  const hitAt = ticksOf(length, hit);
  return { hit: hitAt, cancel: Math.max(ticksOf(length, cancel), hitAt, 1) };
}

/** Where an action's look is back to ready, as ticks from its start: ⌊length × ready⌋, held as the cancel point is. The world never counts it. */
export function readyPoint(length: number, hit: number, ready: number): number {
  return actionPoints(length, hit, ready).cancel;
}

// A product within rounding of a whole number counts as it.
function ticksOf(ticks: number, share: number): number {
  const x = ticks * share;
  const whole = Math.round(x);
  return Math.abs(x - whole) <= whole * WHOLE_TICKS ? whole : Math.floor(x);
}
