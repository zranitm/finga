// An item on the ground: a small gem in its tab's colour, turning and bobbing over its spot; collected, it flies into its owner, and gone otherwise it shrinks away.
import { CreatePolyhedron } from '@babylonjs/core/Meshes/Builders/polyhedronBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { items } from '@/gamedata/items';

import type { Point } from '../systems/drops';

import type { Lighting } from './body';
import { linearOf } from './toon';

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
    this.mesh = CreatePolyhedron('drop', { type: 1, size: SIZE / 2 }, scene); // an octahedron
    this.mesh.material = lighting.palette.toon({
      texture: null,
      color: linearOf(TAB_COLOR[tab] ?? 0xffc857),
    });
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
