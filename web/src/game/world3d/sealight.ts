// A sea's light (underground-and-underwater.md section 7): its sun nearly overhead, cyan and softer than the land's, and the caustics it ripples on what faces up.
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';

/** The sea's sun's way, nearly straight down, so its shadows fall almost under what casts them. */
export const SEA_SUN = new Vector3(0.1, -1, 0.18);

/** The light at its shade band, sRGB: blue-green, and nearer the lit band than the land's, so the sea's light is softer. */
export const SEA_SHADE = new Color3(0.46, 0.6, 0.66).toLinearSpace();
/** The light at its top band, sRGB: the sun's, tinted cyan by the water. */
export const SEA_LIT = new Color3(0.84, 0.97, 1).toLinearSpace();

/** The caustics' web: the gap between the nearest two of 3D Worley noise's points, cells about 2.5 m across over x and z; time its third axis, so the web shifts, as it drifts. */
export const CAUSTICS = {
  scale: 0.4, // cells a metre
  drift: [0.05, 0.02] as const, // cells a second along x and z
  shift: 0.3, // cells a second along time's axis
  width: 0.08, // a line's reach either side of a cell's edge, in cells, where F2 − F1 runs from 0 to this
  up: [0.5, 0.9] as const, // from facing this far up to this, a face takes from none of the web to all of it
  light: new Color3(0.3, 0.42, 0.42), // linear, added to a face's light on the web where the sun reaches it in full
};
