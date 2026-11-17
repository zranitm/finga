// How the view follows the player (3d-world.md section 10): the body turns toward its heading, the camera eases after it.

/** The yaw a heading faces: radians from +z toward +x. */
export function yawOf(hx: number, hz: number): number {
  return Math.atan2(hx, hz);
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
function heldIn(v: number, size: number, half: number): number {
  return size <= 2 * half ? size / 2 : Math.min(Math.max(v, half), size - half);
}

const CAMERA_UP = CAMERA_BACK * Math.sin(CAMERA_PITCH);
const CAMERA_SOUTH = CAMERA_BACK * Math.cos(CAMERA_PITCH);
const CAMERA_FOV_HALF = 0.4; // Babylon's default field of view, 0.8 rad, halved
const VIEW_HALF_DEPTH = CAMERA_UP * Math.tan(CAMERA_FOV_HALF); // the ground the view spans either side of its middle
export const CHEST_M = 0.9; // the camera looks at the player's chest

/** What the camera looks at for follow on a map sizeX by sizeZ seen at aspect: follow, held so the view doesn't run past the map's edge; set into out. */
export function heldView<T extends { x: number; y: number; z: number }>(
  follow: Readonly<{ x: number; y: number; z: number }>,
  sizeX: number,
  sizeZ: number,
  aspect: number,
  out: T,
): T {
  out.x = heldIn(follow.x, sizeX, VIEW_HALF_DEPTH * aspect);
  out.y = follow.y;
  out.z = heldIn(follow.z, sizeZ, VIEW_HALF_DEPTH);
  return out;
}

/** Where the camera sits for a target point, behind it to the south and above it, set into out. */
export function cameraAt<T extends { x: number; y: number; z: number }>(
  x: number,
  y: number,
  z: number,
  out: T,
): T {
  out.x = x;
  out.y = y + CAMERA_UP;
  out.z = z - CAMERA_SOUTH;
  return out;
}
