// A drawn shot's flight (combat-effects.md section 4): from its action's hit at one pace, so its hits show where and when it visibly lands.
import type { Beats } from '../world3d/beats';

/** A shot's pace: 2D's 1.4 px/ms at 32 px a metre, so a full-range Power Shot lands in about 330 ms. */
const SHOT_M_PER_MS = 0.04375;

/** A shot drawn from (x, z) at its action's hit, flying up to range metres. */
export interface Shot {
  readonly x: number;
  readonly z: number;
  readonly range: number;
  readonly beats: Beats;
}

/** How long a shot takes over metres. */
export function flightMs(metres: number): number {
  return metres / SHOT_M_PER_MS;
}

/** How far a shot flying `range` from its hit has flown at now; null before its hit and once past its range. */
export function flown(b: Beats, now: number, range: number): number | null {
  const d = (now - b.hit) * SHOT_M_PER_MS;
  return d < 0 || d > range ? null : d;
}

/** When shot s reaches a target standing at (x, z), as Fire Arrow bursts on its foe: across the ground, no further than its range. */
export function arrival(s: Shot, x: number, z: number): number {
  return s.beats.hit + flightMs(Math.min(s.range, Math.hypot(x - s.x, z - s.z)));
}

/** Skills whose look strikes at once though they reach along a lane: Spark's bolt and Arc's lightning. */
const STRIKES_AT_ONCE: ReadonlySet<string> = new Set(['spark']);

/** How far a skill's stage flies as a drawn shot, as every one reaching along a lane does but those that strike at once; else undefined. */
export function shotRange(
  skill: { readonly id: string },
  stage: { readonly shape: { readonly kind: string; readonly range?: number } | null },
): number | undefined {
  const s = stage.shape;
  return s?.kind === 'shot' && !STRIKES_AT_ONCE.has(skill.id) ? (s.range ?? 0) : undefined;
}
