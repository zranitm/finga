// An action drawn in beats on the scene's clock: the strike from its hit, a third of the way to the cancel point; the follow-through to it; then ready, when a cancel can start. A skill slows its user to its end, so after its cancel point it holds a slight recovery lean until then, or until an attack frees the player. The follow-through is the only tell of the cancel point, which players find out by feel.
import { actionPoints } from '@/progression/actions';

import type { ActionTiming } from '../systems/actions';

/** An action's beats in the scene's milliseconds. */
export interface Beats {
  readonly start: number;
  readonly hit: number;
  readonly strikeEnd: number;
  readonly cancelAt: number;
  readonly end: number;
  /** A skill slows its user, walking and climbing, from its start to its end. */
  readonly slows: boolean;
}

export function beatsOf(t: ActionTiming, start: number, tickMs: number): Beats {
  const points = actionPoints(t.ticks, t.hit, t.cancel);
  const hit = start + points.hit * tickMs;
  const cancelAt = start + points.cancel * tickMs;
  return {
    start,
    hit,
    strikeEnd: hit + (cancelAt - hit) / 3,
    cancelAt,
    end: start + t.ticks * tickMs,
    slows: t.kind === 'skill',
  };
}

const RECOVERY = 0.25; // a share of the strike's lean, held while a skill's slow lasts

/** How much of each part of the pose is drawn, 1 the most and 0 none: a lean below 0 leans back in a wind-up. slowed walks at the casting pace. */
export interface Pose {
  readonly hitFrame: boolean;
  readonly lean: number;
  readonly lunge: number;
  readonly stretch: number;
  readonly slowed: boolean;
}

/** The pose at now: none before the start; none once ready at the cancel point, or for a skill the recovery lean until its end. */
export function poseAt(b: Beats, now: number): Pose | null {
  const slowed = b.slows;
  if (now < b.start || now >= (slowed ? b.end : b.cancelAt)) {
    return null;
  }
  if (now >= b.cancelAt) {
    return { hitFrame: false, lean: RECOVERY, lunge: 0, stretch: 0, slowed };
  }
  if (now < b.hit) {
    return {
      hitFrame: false,
      lean: -(now - b.start) / (b.hit - b.start),
      lunge: 0,
      stretch: 0,
      slowed,
    };
  }
  if (now < b.strikeEnd) {
    return {
      hitFrame: true,
      lean: 1,
      lunge: 1,
      stretch: 1 - (now - b.hit) / (b.strikeEnd - b.hit),
      slowed,
    };
  }
  const left = (1 - (now - b.strikeEnd) / (b.cancelAt - b.strikeEnd)) ** 2; // eases out, a skill to its recovery lean
  const rest = slowed ? RECOVERY : 0;
  return { hitFrame: false, lean: rest + (1 - rest) * left, lunge: left, stretch: 0, slowed };
}
