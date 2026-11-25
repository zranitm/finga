// The player's own pouches where they fell: KayKit's sack for a kill's, its chest for a boss's box, the gold one for a box that fell holding a 4★ item or better.
import type { Material } from '@babylonjs/core/Materials/material';
import type { PBRMaterial } from '@babylonjs/core/Materials/PBR/pbrMaterial';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Scene } from '@babylonjs/core/scene';

import { items } from '@/gamedata/items';
import { PouchKind } from '@/protocol/messages';
import type { Pouch } from '@/state/pouches';

import type { Lighting } from './body';
import type { Models } from './models';
import type { ToonBase } from './toon';

/** Each look's file, scaled evenly from the pack's size: a sack 0.43 m across, a chest 0.85 m; the gold chest's metal is gilded in its file. */
export const POUCH_LOOKS = {
  pouch: { file: 'kaykit/sack.glb', scale: 4 },
  box: { file: 'kaykit/chest.glb', scale: 0.5 },
  gold: { file: 'kaykit/chest_gold.glb', scale: 0.5 },
} as const;

export type PouchLook = keyof typeof POUCH_LOOKS;

const RARE_STARS = 4;
const GOLDEN_ANGLE = 2.399963; // turns each pouch its own way, never two alike in a row

/** A pouch's look as it falls: a box holding a 4★ item or better is gold. */
export function lookOf(p: Pick<Pouch, 'kind' | 'items'>): PouchLook {
  if (p.kind !== PouchKind.BOX) {
    return 'pouch';
  }
  const rare = p.items.some(
    (it) => (items.find((t) => t.id === it.template)?.stars ?? 1) >= RARE_STARS,
  );
  return rare ? 'gold' : 'box';
}

export class PouchMarks {
  private readonly drawn = new Map<number, TransformNode>();

  constructor(
    private readonly scene: Scene,
    private readonly models: Models,
    private readonly lighting: Lighting,
  ) {}

  /** Draws each pouch not yet drawn, in the look it fell in, and takes away those gone. */
  show(pouches: readonly Pouch[]): void {
    const ids = new Set(pouches.map((p) => p.id));
    for (const [id, root] of this.drawn) {
      if (!ids.has(id)) {
        root.dispose();
        this.drawn.delete(id);
      }
    }
    for (const p of pouches) {
      if (!this.drawn.has(p.id)) {
        this.drawn.set(p.id, this.place(p));
      }
    }
  }

  dispose(): void {
    for (const root of this.drawn.values()) {
      root.dispose();
    }
    this.drawn.clear();
  }

  private place(p: Pouch): TransformNode {
    const look = POUCH_LOOKS[lookOf(p)];
    const root = new TransformNode(`pouch ${p.id}`, this.scene);
    root.position.set(p.x, p.y, p.z);
    root.rotation.y = (p.id * GOLDEN_ANGLE) % (2 * Math.PI);
    root.scaling.setAll(look.scale);
    this.models
      .load(look.file)
      .then((container) => {
        if (root.isDisposed()) {
          return; // taken or gone while it loaded
        }
        const made = container.instantiateModelsToScene((n) => `pouch ${p.id} ${n}`, false, {
          doNotInstantiate: true,
        });
        for (const r of made.rootNodes) {
          r.parent = root;
        }
        const { palette, shadows } = this.lighting;
        for (const m of root.getChildMeshes(false)) {
          if (!(m instanceof Mesh) || !m.material) {
            continue;
          }
          m.material = palette.toon(baseOf(m.material));
          m.receiveShadows = true;
          shadows?.addShadowCaster(m, false);
          palette.outline(m);
        }
      })
      .catch((err: unknown) => {
        console.info(`finga: ${look.file} didn't load: ${String(err)}`); // the pouch is still in reach and in the window
      });
    return root;
  }
}

/** The toon base for a glTF material: its texture and colour as authored, as the kit props draw. */
function baseOf(source: Material): ToonBase {
  const pbr = source as Partial<Pick<PBRMaterial, 'albedoTexture' | 'albedoColor'>>;
  return {
    texture: pbr.albedoTexture instanceof Texture ? pbr.albedoTexture : null,
    color: pbr.albedoColor ?? Color3.White(),
  };
}
