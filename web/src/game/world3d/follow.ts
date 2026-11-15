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

/** The camera sits 16 m back, 50° down, looking north, and eases this share of the way to the player each frame, as today. */
const CAMERA_BACK = 16;
const CAMERA_PITCH = (50 * Math.PI) / 180;
export const CAMERA_EASE = 0.15;

/** Where the camera sits for a target point: behind it to the south, above it. */
export function cameraAt(x: number, y: number, z: number): [number, number, number] {
  return [x, y + CAMERA_BACK * Math.sin(CAMERA_PITCH), z - CAMERA_BACK * Math.cos(CAMERA_PITCH)];
}
