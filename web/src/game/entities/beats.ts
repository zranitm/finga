// An action drawn in beats on the scene's clock: the strike from its hit, a third of the way to the cancel point; the follow-through to it; then ready, when a cancel can start. The follow-through easing into ready is the only tell of the cancel point, which players find out by feel.
import { actionPoints } from '@/progression/actions';

import type { ActionTiming } from '../systems/actions';

/** An action's beats in the scene's milliseconds. */
export interface Beats {
  readonly start: number;
  readonly hit: number;
  readonly strikeEnd: number;
  readonly cancelAt: number;
}

export function beatsOf(t: ActionTiming, start: number, tickMs: number): Beats {
  const points = actionPoints(t.ticks, t.hit, t.cancel);
  const hit = start + points.hit * tickMs;
  const cancelAt = start + points.cancel * tickMs;
  return { start, hit, strikeEnd: hit + (cancelAt - hit) / 3, cancelAt };
}

/** How much of each part of the pose is drawn, 1 the most and 0 none: a lean below 0 leans back in a wind-up. */
export interface Pose {
  readonly hitFrame: boolean;
  readonly lean: number;
  readonly lunge: number;
  readonly stretch: number;
}

/** The pose at now: none before the start or once ready at the cancel point. */
export function poseAt(b: Beats, now: number): Pose | null {
  if (now < b.start || now >= b.cancelAt) {
    return null;
  }
  if (now < b.hit) {
    return { hitFrame: false, lean: -(now - b.start) / (b.hit - b.start), lunge: 0, stretch: 0 };
  }
  if (now < b.strikeEnd) {
    return {
      hitFrame: true,
      lean: 1,
      lunge: 1,
      stretch: 1 - (now - b.hit) / (b.strikeEnd - b.hit),
    };
  }
  const left = (1 - (now - b.strikeEnd) / (b.cancelAt - b.strikeEnd)) ** 2; // eases out
  return { hitFrame: false, lean: left, lunge: left, stretch: 0 };
}
