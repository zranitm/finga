export type Pose = 'stand' | 'walk' | 'jump' | 'climb';

export interface PoseState {
  readonly vx: number;
  readonly vy: number;
  readonly on_ground: boolean;
  readonly climbing: boolean;
}

export function poseFor(s: PoseState): Pose {
  if (s.climbing) {
    return 'climb';
  }
  if (!s.on_ground) {
    return 'jump';
  }
  return s.vx !== 0 ? 'walk' : 'stand';
}

/** Climbing animates only while moving along the rope. */
export function climbMoving(s: PoseState): boolean {
  return s.climbing && s.vy !== 0;
}
