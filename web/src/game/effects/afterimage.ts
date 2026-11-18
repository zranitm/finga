// A cancel's afterimage (combat-effects.md section 6): the pose the cancel cut off, left behind in white with a dark rim, fading as it drifts back.
import { FresnelParameters } from '@babylonjs/core/Materials/fresnelParameters';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Scene } from '@babylonjs/core/scene';

import type { Figure } from '../world3d/body';
import { warm } from '../world3d/toon';

import { EDGE } from './colors';
import { forward } from './draw';
import type { Fx } from './fx';
import type { Drawn, Stop } from './types';

const FADE_MS = 300;
const FROM_ALPHA = 0.8;
const DRIFT_M = 0.44; // 2D's 14 px: most of it shows from behind the new pose

const materials = new WeakMap<Scene, StandardMaterial>();
const fading = new WeakMap<Figure, Stop>();

/** White, darkening to a rim at its edges, so it reads on pale ground and dark caves alike; one silhouette, not its insides. */
function ghostMaterial(scene: Scene): StandardMaterial {
  let m = materials.get(scene);
  if (!m) {
    m = new StandardMaterial('afterimage', scene);
    m.disableLighting = true;
    m.diffuseColor = Color3.Black();
    m.specularColor = Color3.Black();
    m.emissiveColor = Color3.White();
    m.emissiveFresnelParameters = new FresnelParameters({
      leftColor: Color3.FromHexString(`#${EDGE.toString(16).padStart(6, '0')}`),
      rightColor: Color3.White(),
      power: 3,
      bias: 0.1,
    });
    m.alpha = 0.999; // blended, so each mesh's visibility fades it
    m.needDepthPrePass = true;
    m.fogEnabled = false;
    materials.set(scene, m);
  }
  return m;
}

export const afterimage: Drawn = (fx, o, now) => {
  const figure = o.copy;
  if (!figure) {
    return () => undefined;
  }
  fading.get(figure)?.(); // a cancel during the last one's fade takes its copy over
  const still = figure.afterimage(ghostMaterial(fx.scene));
  if (!still) {
    return () => undefined;
  }
  const { x, z } = still.root.position;
  const back = forward(o.yaw);
  const stop = fx.everyFrame(
    now,
    now + FADE_MS,
    (t) => {
      const k = (t - now) / FADE_MS;
      for (const m of still.meshes) {
        m.visibility = FROM_ALPHA * (1 - k);
      }
      still.root.position.x = x - back.x * DRIFT_M * k;
      still.root.position.z = z - back.z * DRIFT_M * k;
    },
    () => {
      still.root.setEnabled(false);
      fading.delete(figure);
    },
  );
  fading.set(figure, stop);
  return stop;
};

/** Makes figure's copy and its shaders ahead, hidden, so the first cancel doesn't hitch. */
export function warmAfterimage(fx: Fx, figure: Figure): void {
  const material = ghostMaterial(fx.scene);
  const still = figure.afterimage(material);
  if (!still) {
    return;
  }
  warm(still.meshes.map((m) => [material, m] as const));
  still.root.setEnabled(false);
}
