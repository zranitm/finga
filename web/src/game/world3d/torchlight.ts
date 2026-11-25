// A cave's torchlight (underground-and-underwater.md section 7): the light map as a texture its toon materials sample for the sun's light, and the flicker of its two groups.
import { Constants } from '@babylonjs/core/Engines/constants';
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Scene } from '@babylonjs/core/scene';

import type { LightMap } from './region-bake';

/** Each flicker group's sines, an amplitude, a rate in radians a second and a phase: two slow sums at different rates, each group's amplitudes adding to 0.08, so its light swings ±8%. */
export const FLICKERS: readonly (readonly { amp: number; rate: number; phase: number }[])[] = [
  [
    { amp: 0.05, rate: 2.1, phase: 0 },
    { amp: 0.03, rate: 5.3, phase: 1.3 },
  ],
  [
    { amp: 0.05, rate: 1.7, phase: 2.2 },
    { amp: 0.03, rate: 4.6, phase: 0.4 },
  ],
];

/** The light's floor where no torch reaches, sRGB, a little cool: dim, never black, so a body and a warning always show. */
export const FLOOR = new Color3(0.3, 0.3, 0.34).toLinearSpace();
/** The light at its top band, sRGB, warmed toward the torches' orange. */
export const TORCH = new Color3(1, 0.86, 0.66).toLinearSpace();

/** A cave's light map as a texture, red and green its groups', sampled between texel centres and held at its edges; a cave with no torches gets one dark texel. */
export class Torchlight {
  readonly texture: Texture;
  /** The metres the texture spans along x and z. */
  readonly size: readonly [number, number];

  constructor(scene: Scene, map: LightMap | null) {
    const [nx, nz] = map ? [map.nx, map.nz] : [1, 1];
    const rgba = new Uint8Array(4 * nx * nz);
    for (let i = 0; map && i < nx * nz; i++) {
      rgba[4 * i] = map.texels[2 * i]!;
      rgba[4 * i + 1] = map.texels[2 * i + 1]!;
    }
    this.texture = RawTexture.CreateRGBATexture(
      rgba,
      nx,
      nz,
      scene,
      false,
      false,
      Constants.TEXTURE_BILINEAR_SAMPLINGMODE,
    );
    this.texture.wrapU = Texture.CLAMP_ADDRESSMODE;
    this.texture.wrapV = Texture.CLAMP_ADDRESSMODE;
    this.size = [nx, nz];
  }
}
