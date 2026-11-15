// An action drawn in beats on the scene's clock: the strike from its hit, a third of the way to its ready point; the follow-through to it; then ready. The beats never time a cancel: the action clock holds the cancel point, and a cancel cuts the beats wherever they are.
import { actionPoints, readyPoint } from '@/progression/actions';

import type { ActionTiming } from '../systems/actions';

/** An action's beats in the scene's milliseconds. */
export interface Beats {
  readonly start: number;
  readonly hit: number;
  readonly strikeEnd: number;
  readonly readyAt: number;
  readonly end: number;
}

export function beatsOf(t: ActionTiming, start: number, tickMs: number): Beats {
  const hit = start + actionPoints(t.ticks, t.hit, t.cancel).hit * tickMs;
  const readyAt = start + readyPoint(t.ticks, t.hit, t.ready) * tickMs;
  return {
    start,
    hit,
    strikeEnd: hit + (readyAt - hit) / 3,
    readyAt,
    end: start + t.ticks * tickMs,
  };
}
