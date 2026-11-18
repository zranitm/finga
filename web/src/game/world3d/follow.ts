// How the view follows the player (3d-world.md section 10): the body turns toward its heading, the camera eases after it.

/** The yaw a heading faces: radians from +z toward +x. */
export function yawOf(hx: number, hz: number): number {
  return Math.atan2(hx, hz);
}

/** The camera sits 16 m back, 50° down, looking north, and zoomed out, 40 m back, 35° down (large-regions.md section 9). */
const NEAR = { back: 16, pitch: (50 * Math.PI) / 180 };
const FAR = { back: 40, pitch: (35 * Math.PI) / 180 };

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

const CAMERA_FOV_HALF = 0.4; // Babylon's default field of view, 0.8 rad, halved
export const CHEST_M = 0.9; // the camera looks at the player's chest

/** How high the camera sits over its target at zoom, 0 close to 1 far. */
function upAt(zoom: number): number {
  return backAt(zoom) * Math.sin(pitchAt(zoom));
}

function backAt(zoom: number): number {
  return NEAR.back + (FAR.back - NEAR.back) * zoom;
}

function pitchAt(zoom: number): number {
  return NEAR.pitch + (FAR.pitch - NEAR.pitch) * zoom;
}

/** What the camera looks at for follow on a map sizeX by sizeZ seen at aspect and zoom: follow, held so the view doesn't run past the map's edge; set into out. */
export function heldView<T extends { x: number; y: number; z: number }>(
  follow: Readonly<{ x: number; y: number; z: number }>,
  sizeX: number,
  sizeZ: number,
  aspect: number,
  zoom: number,
  out: T,
): T {
  const halfDepth = upAt(zoom) * Math.tan(CAMERA_FOV_HALF); // the ground the view spans either side of its middle
  out.x = heldIn(follow.x, sizeX, halfDepth * aspect);
  out.y = follow.y;
  out.z = heldIn(follow.z, sizeZ, halfDepth);
  return out;
}

/** Where the camera sits for a target point at zoom, behind it to the south and above it, set into out. */
export function cameraAt<T extends { x: number; y: number; z: number }>(
  x: number,
  y: number,
  z: number,
  zoom: number,
  out: T,
): T {
  out.x = x;
  out.y = y + upAt(zoom);
  out.z = z - backAt(zoom) * Math.cos(pitchAt(zoom));
  return out;
}
