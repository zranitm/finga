// An item on the ground: a small gem in its tab's colour, turning and bobbing over its spot; collected, it flies into its owner, and gone otherwise it shrinks away.
import { CreatePolyhedron } from '@babylonjs/core/Meshes/Builders/polyhedronBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { items } from '@/gamedata/items';

import type { Point } from '../systems/drops';

import type { Lighting } from './body';
import { linearOf, warm } from './toon';
import type { Palette } from './toon';

const SIZE = 0.3; // metres across
const FLOAT = 0.35; // the gem's middle over the ground
const BOB = 0.08;
const TURN_MS = 2000; // a whole turn
const LEAVE_MS = 250; // flying to its owner, or shrinking away

/** Each item tab's gem colour, sRGB. */
const TAB_COLOR: Readonly<Record<string, number>> = {
  equip: 0x7fb2ff,
  use: 0xff6b6b,
  etc: 0xffc857,
};

/** Its tab's gem material: a tab without a colour takes etc's. */
function gemMaterial(palette: Palette, tab: string) {
  return palette.toon({ texture: null, color: linearOf(TAB_COLOR[tab] ?? TAB_COLOR.etc!) });
}

/** A gem, as every drop draws. */
function gem(name: string, scene: Scene): Mesh {
  return CreatePolyhedron(name, { type: 1, size: SIZE / 2 }, scene); // an octahedron
}

/** Builds each tab's gem shaders on a hidden gem kept for the map, so a first drop of its colour doesn't stall a frame. */
export function warmGems(scene: Scene, palette: Palette): void {
  const kept = gem('kept gem', scene);
  kept.setEnabled(false);
  warm(Object.keys(TAB_COLOR).map((tab) => [gemMaterial(palette, tab), kept] as const));
}

export class DropMark {
  private readonly mesh: Mesh;
  private leaving: { readonly from: number; readonly to: (() => Point) | null } | null = null;

  constructor(
    scene: Scene,
    template: string,
    private readonly at: Point,
    lighting: Lighting,
  ) {
    const tab = items.find((i) => i.id === template)?.tab ?? 'etc';
    this.mesh = gem('drop', scene);
    this.mesh.material = gemMaterial(lighting.palette, tab);
    lighting.palette.outline(this.mesh);
    lighting.shadows.addShadowCaster(this.mesh, false);
    this.mesh.position.set(at.x, at.y + FLOAT, at.z);
  }

  /** Flies into the owner at owner(), or with none shrinks where it lies, from now. */
  leave(now: number, owner: (() => Point) | null): void {
    this.leaving ??= { from: now, to: owner };
  }

  /** Steps it to now; false once it has left and is gone. */
  update(now: number): boolean {
    const m = this.mesh;
    m.rotation.y = ((now % TURN_MS) / TURN_MS) * 2 * Math.PI;
    if (!this.leaving) {
      m.position.y = this.at.y + FLOAT + BOB * Math.sin((now / TURN_MS) * 2 * Math.PI);
      return true;
    }
    const f = Math.min(1, (now - this.leaving.from) / LEAVE_MS);
    const to = this.leaving.to?.();
    if (to) {
      m.position.set(
        this.at.x + (to.x - this.at.x) * f,
        this.at.y + FLOAT + (to.y - this.at.y - FLOAT) * f,
        this.at.z + (to.z - this.at.z) * f,
      );
    }
    m.scaling.setAll(1 - f);
    if (f < 1) {
      return true;
    }
    this.dispose();
    return false;
  }

  dispose(): void {
    this.mesh.dispose(false, false);
  }
}
