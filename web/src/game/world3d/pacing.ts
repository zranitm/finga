// How often each figure's bones are posed (3d-world.md section 10): every frame near the camera's target, less often far off, never off the screen.
import { Frustum } from '@babylonjs/core/Maths/math.frustum';
import { Plane } from '@babylonjs/core/Maths/math.plane';
import type { Matrix } from '@babylonjs/core/Maths/math.vector';

/** Beyond each distance from the camera's target, in metres, farthest first, a figure is posed every so many frames. */
export const POSE_EVERY: readonly (readonly [beyond: number, every: number])[] = [
  [60, 4],
  [30, 2],
];

export class Pacing {
  private frame = 0;
  private beats = 0;
  private x = 0;
  private z = 0;
  private readonly planes = Array.from({ length: 6 }, () => new Plane(0, 0, 0, 0));
  private seen = false; // whether a frame has been drawn, whose screen it can test against

  /** A new figure's beat: each one's frames fall on its own, so the work spreads across them. */
  beat(): number {
    return this.beats++;
  }

  /** Starts a frame: the camera's target now, and the screen as last drawn, its view and projection; none before the first frame. */
  next(x: number, z: number, screen: Matrix | null): void {
    this.frame++;
    this.x = x;
    this.z = z;
    this.seen = screen !== null;
    if (screen) {
      Frustum.GetPlanesToRef(screen, this.planes);
    }
  }

  /** Whether a figure whose bounds are a sphere reach round (x, y, z), on beat, is posed this frame. */
  poses(x: number, y: number, z: number, reach: number, beat: number): boolean {
    if (this.seen) {
      for (const p of this.planes) {
        if (p.normal.x * x + p.normal.y * y + p.normal.z * z + p.d < -reach) {
          return false; // off the screen: it's posed again as it comes back
        }
      }
    }
    const away = Math.hypot(x - this.x, z - this.z);
    let every = 1;
    for (const [beyond, n] of POSE_EVERY) {
      if (away > beyond) {
        every = n;
        break;
      }
    }
    return (this.frame + beat) % every === 0;
  }
}
