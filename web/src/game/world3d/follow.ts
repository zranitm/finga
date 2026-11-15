// How the view follows the player (3d-world.md section 10): the body turns toward its heading, the camera eases after it.

/** The yaw a heading faces: radians from +z toward +x. */
export function yawOf(hx: number, hz: number): number {
  return Math.atan2(hx, hz);
}

/** from turned toward to by share of the way, the short way round. */
export function turn(from: number, to: number, share: number): number {
  const d = Math.atan2(Math.sin(to - from), Math.cos(to - from));
  return from + d * share;
}

/** The camera sits 16 m back, 50° down, looking north. */
const CAMERA_BACK = 16;
const CAMERA_PITCH = (50 * Math.PI) / 180;

/** The camera's lag: it closes 0.15 of its gap to the player in a 60 fps frame, as the 2D camera did, and as much in the same time at any frame rate. */
const CAMERA_LAG_MS = -(1000 / 60) / Math.log(1 - 0.15);

/** The share of its gap the camera closes in dtMs. */
export function easeShare(dtMs: number): number {
  return 1 - Math.exp(-dtMs / CAMERA_LAG_MS);
}

/** v along a map side of size, held so a view half wide doesn't run past the edge; a side narrower than the view centres it. */
export function heldIn(v: number, size: number, half: number): number {
  return size <= 2 * half ? size / 2 : Math.min(Math.max(v, half), size - half);
}

/** Where the camera sits for a target point: behind it to the south, above it. */
export function cameraAt(x: number, y: number, z: number): [number, number, number] {
  return [x, y + CAMERA_BACK * Math.sin(CAMERA_PITCH), z - CAMERA_BACK * Math.cos(CAMERA_PITCH)];
}
