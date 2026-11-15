// The weights a body's clips play at: actions over locomotion, on the upper body alone while it moves, every switch faded.

/** A cut, a start, an end, and the mask's switch at the start or end of a move all blend over this. */
export const BLEND_MS = 80;

/** Weights fading linearly toward their targets, a full swing in BLEND_MS. */
export class Fades {
  private readonly weights = new Map<string, number>();
  private readonly targets = new Map<string, number>();

  /** Fades key toward target; one not seen yet starts from 0. */
  aim(key: string, target: number): void {
    this.targets.set(key, target);
    if (!this.weights.has(key)) {
      this.weights.set(key, 0);
    }
  }

  /** Fades every other key toward 0, and key toward 1. */
  only(key: string): void {
    for (const k of this.weights.keys()) {
      this.targets.set(k, 0);
    }
    this.aim(key, 1);
  }

  step(dtMs: number): void {
    const d = dtMs / BLEND_MS;
    for (const [key, w] of this.weights) {
      const t = this.targets.get(key) ?? 0;
      this.weights.set(key, w < t ? Math.min(t, w + d) : Math.max(t, w - d));
    }
  }

  get(key: string): number {
    return this.weights.get(key) ?? 0;
  }

  entries(): [string, number][] {
    return [...this.weights];
  }
}

export interface Layered {
  readonly upper: number;
  readonly lower: number;
}

/** Each clip's weight on the upper and the lower body: actions take their share of both while standing (moving 0) and of the upper alone while moving (1); locomotion fills the rest, each layer's weights scaled to sum at most 1. */
export function layered(
  actions: readonly [string, number][],
  locomotion: readonly [string, number][],
  moving: number,
): Map<string, Layered> {
  const total = (ws: readonly [string, number][]) => ws.reduce((sum, [, w]) => sum + w, 0);
  const a = total(actions);
  const scaleA = a > 1 ? 1 / a : 1;
  const acting = Math.min(a, 1);
  const l = total(locomotion);
  const scaleL = l > 0 ? 1 / l : 0;
  const out = new Map<string, Layered>();
  for (const [clip, w] of actions) {
    out.set(clip, { upper: w * scaleA, lower: w * scaleA * (1 - moving) });
  }
  for (const [clip, w] of locomotion) {
    const share = w * scaleL;
    out.set(clip, { upper: share * (1 - acting), lower: share * (1 - acting * (1 - moving)) });
  }
  return out;
}
