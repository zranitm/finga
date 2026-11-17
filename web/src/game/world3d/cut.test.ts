// The place's cut-away round the player (3d-world.md section 10), checked against the drawn ground of every map.
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DiscardBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/discardBlock';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';
import { movement } from '@/gamedata/movement';
import { steep, top } from '@/sim/terrain';
import type { Terrain } from '@/sim/terrain';
import { servePublic } from '@/testing/public-on-disk';

import { CHEST_M, cameraAt, heldView } from './follow';
import { groundUnder, loadMap } from './map';
import type { WorldMap } from './map';
import { Palette } from './toon';

const SPOT = 0.5; // a player every half metre
const STEP = 0.04; // along a line
const UNDER = 0.02; // a line this far under the drawn ground is hidden by it
const ASPECT = 1280 / 800;
const JUMP = movement.jump_velocity ** 2 / (2 * movement.gravity);
const FEET_M = 1; // the floor this far round the feet stays

let engine: NullEngine;
let scene: Scene;
let palette: Palette;

beforeEach(() => {
  servePublic();
  engine = new NullEngine();
  scene = new Scene(engine);
  palette = new Palette(scene);
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

const input = (m: NodeMaterial, name: string) => m.getBlockByName(name) as InputBlock;

/** m once its build, which loads its blocks' code first, is done. */
async function built(m: NodeMaterial): Promise<NodeMaterial> {
  while (m.buildIsInProgress) {
    await new Promise((r) => setTimeout(r, 1));
  }
  return m;
}

/** The cut's reach, off a place material: how much nearer the camera than the chest, and how near it on screen, a point opens fully, and where the dither ends. */
function reachOf(m: NodeMaterial) {
  return {
    nearer: input(m, 'cutNearer').value as number,
    inner: input(m, 'cutInner').value as number,
    outer: input(m, 'cutOuter').value as number,
  };
}
type Reach = ReturnType<typeof reachOf>;

/** The drawn ground's height at (x, z), two triangles a cell split from (x+1, z) to (x, z+1) as place.ts lays it; null off the grid. */
function drawn(t: Terrain, x: number, z: number): number | null {
  const fx = x / t.cell;
  const fz = z / t.cell;
  const ix = Math.floor(fx);
  const iz = Math.floor(fz);
  if (ix < 0 || iz < 0 || ix >= t.nx - 1 || iz >= t.nz - 1) {
    return null;
  }
  const ax = fx - ix;
  const az = fz - iz;
  const h = (i: number, k: number) => t.heights[(iz + k) * t.nx + ix + i]!;
  return ax + az <= 1
    ? h(0, 0) + (h(1, 0) - h(0, 0)) * ax + (h(0, 1) - h(0, 0)) * az
    : h(1, 1) + (h(0, 1) - h(1, 1)) * (1 - ax) + (h(1, 0) - h(1, 1)) * (1 - az);
}

/** The spots a player reaches, every SPOT m out from the map's portals: clear of the solids by a body's radius, and up no cliff higher than a jump. */
function reachable(map: WorldMap): { x: number; z: number; y: number }[] {
  const g = map.geometry;
  const r = movement.radius;
  const nx = Math.round(g.size_x / SPOT);
  const nz = Math.round(g.size_z / SPOT);
  const at = (i: number) => (i + 0.5) * SPOT;
  const clear = (x: number, z: number) =>
    x >= r &&
    z >= r &&
    x <= g.size_x - r &&
    z <= g.size_z - r &&
    !map.solids.some((s) => {
      if (s.kind === 'cylinder') {
        return Math.hypot(x - s.cylinder.x, z - s.cylinder.z) < s.cylinder.radius + r;
      }
      const b = s.box;
      const u = (x - b.x) * b.ux + (z - b.z) * b.uz;
      const v = -(x - b.x) * b.uz + (z - b.z) * b.ux;
      return Math.abs(u) < b.half_w + r && Math.abs(v) < b.half_d + r;
    });
  const ys: number[] = [];
  const ok: boolean[] = [];
  for (let j = 0; j < nz; j++) {
    for (let i = 0; i < nx; i++) {
      ok.push(clear(at(i), at(j)));
      ys.push(groundUnder(map, at(i), at(j)));
    }
  }
  const seen = new Array<boolean>(nx * nz).fill(false);
  const queue = map.portals.map((p) => Math.floor(p.z / SPOT) * nx + Math.floor(p.x / SPOT));
  for (const k of queue) {
    seen[k] = true;
  }
  while (queue.length > 0) {
    const k = queue.pop()!;
    const i = k % nx;
    const j = Math.floor(k / nx);
    for (const [di, dj] of [
      [1, 0],
      [-1, 0],
      [0, 1],
      [0, -1],
    ] as const) {
      const a = i + di;
      const b = j + dj;
      const n = b * nx + a;
      if (a < 0 || b < 0 || a >= nx || b >= nz || seen[n] || !ok[n]) {
        continue;
      }
      const cliff = ys[n]! > ys[k]! && steep(g.terrain, at(a), at(b), dj !== 0);
      if (cliff && ys[k]! + JUMP < top(g.terrain, at(a), at(b))) {
        continue;
      }
      seen[n] = true;
      queue.push(n);
    }
  }
  return seen.flatMap((s, k) =>
    s ? [{ x: at(k % nx), z: at(Math.floor(k / nx)), y: ys[k]! }] : [],
  );
}

/** How a point stands to the cut round chest, seen by a camera with this view: fully open, dithered, or kept. */
function cutOf(
  view: Matrix,
  chest: Vector3,
  p: Vector3,
  reach: Reach,
): 'open' | 'dithered' | 'kept' {
  const c = Vector3.TransformCoordinates(chest, view);
  const q = Vector3.TransformCoordinates(p, view);
  if (c.z - q.z < reach.nearer) {
    return 'kept';
  }
  const k = c.z / Math.max(q.z, 0.01);
  const away = Math.hypot(q.x * k - c.x, q.y * k - c.y);
  return away <= reach.inner ? 'open' : away < reach.outer ? 'dithered' : 'kept';
}

/** The points on the line from cam to target that the drawn ground covers, sampled where the line is low enough to meet it. */
function covered(t: Terrain, cam: Vector3, target: Vector3, highest: number): Vector3[] {
  const len = Vector3.Distance(cam, target);
  const rise = (cam.y - target.y) / len;
  const reach = Math.min(len - STEP, (highest - target.y) / rise);
  const out: Vector3[] = [];
  for (let s = STEP; s <= reach; s += STEP) {
    const p = Vector3.Lerp(target, cam, s / len);
    const h = drawn(t, p.x, p.z);
    if (h !== null && p.y < h - UNDER) {
      out.push(p);
    }
  }
  return out;
}

/** Each reachable spot of map as the game's camera sees it: the camera, its view, the chest and the head. */
function* seen(map: WorldMap) {
  const g = map.geometry;
  const look = Vector3.Zero();
  for (const s of reachable(map)) {
    heldView({ x: s.x, y: s.y + CHEST_M, z: s.z }, g.size_x, g.size_z, ASPECT, look);
    const cam = cameraAt(look.x, look.y, look.z, Vector3.Zero());
    yield {
      spot: s,
      cam,
      view: Matrix.LookAtLH(cam, look, Vector3.Up()), // as the game's TargetCamera sets it

      chest: new Vector3(s.x, s.y + CHEST_M, s.z),
      head: new Vector3(s.x, s.y + movement.height, s.z),
    };
  }
}

describe('the cut-away round the player', () => {
  it('drops a place fragment over 0.5 m nearer the camera than the chest and within about 1.2 m of it on screen, dithered at the edge; a body’s material has no cut', async () => {
    const base = { texture: null, color: new Color3(0.4, 0.5, 0.3) };
    const cut = await built(palette.toon(base, 1, true));
    const whole = await built(palette.toon(base));
    const discards = (m: NodeMaterial) => m.attachedBlocks.filter((b) => b instanceof DiscardBlock);
    expect(discards(cut).map((b) => b.name)).toEqual(['cut']);
    expect(discards(whole)).toEqual([]);
    expect(cut.name).toBe(`${whole.name} cut`);
    expect(palette.toon(base, 0.3, true).name).toBe(`${whole.name} cut 0.3`);
    const { nearer, inner, outer } = reachOf(cut);
    expect(nearer).toBe(0.5);
    expect((inner + outer) / 2).toBeCloseTo(1.2);
    expect(inner).toBeLessThan(outer);
    // the alpha-test queue, after what's opaque; its shadow is drawn whole, as no texture's alpha tests it
    expect(cut.needAlphaTesting()).toBe(true);
    expect(whole.needAlphaTesting()).toBe(false);
    expect(cut.getAlphaTestTexture()).toBeNull();

    const box = CreateBox('rock', {}, scene);
    palette.outline(box, true);
    const plain = CreateBox('body', {}, scene);
    palette.outline(plain);
    const inkOf = (m: typeof box) => built(m.getChildMeshes(true)[0]!.material as NodeMaterial);
    expect((await inkOf(box)).name).toBe('ink cut');
    expect((await inkOf(box)).needAlphaTesting()).toBe(true);
    expect((await inkOf(plain)).needAlphaTesting()).toBe(false);

    expect(input(cut, 'cutOn').valueCallback()).toBe(0); // shut till there's a player to open round
    palette.cutAround(3, 1.9, 7);
    expect(input(cut, 'cutOn').valueCallback()).toBe(1);
    expect(input(cut, 'cutChest').valueCallback()).toEqual(new Vector3(3, 1.9, 7));
    expect(input(await inkOf(box), 'cutChest').valueCallback()).toEqual(new Vector3(3, 1.9, 7));
  });

  it('opens every line from the game’s camera to a reachable chest and head that the drawn ground covers, on every map, and keeps the floor round the feet', async () => {
    const reach = reachOf(palette.toon({ texture: null, color: Color3.Gray() }, 1, true));
    const hidden: string[] = [];
    const shut: string[] = [];
    const floor: string[] = [];
    for (const { id } of maps) {
      const map = await loadMap(id);
      const t = map.geometry.terrain;
      const highest = Math.max(...t.heights);
      for (const { spot, cam, view, chest, head } of seen(map)) {
        const at = `${id} (${spot.x}, ${spot.z})`;
        const chestCover = covered(t, cam, chest, highest);
        if (chestCover.length > 0) {
          hidden.push(at);
        }
        for (const p of [...chestCover, ...covered(t, cam, head, highest)]) {
          if (cutOf(view, chest, p, reach) !== 'open') {
            shut.push(`${at}: (${p.x.toFixed(2)}, ${p.y.toFixed(2)}, ${p.z.toFixed(2)})`);
            break;
          }
        }
        for (let k = 0; k < 8; k++) {
          const a = (k * Math.PI) / 4;
          const x = spot.x + FEET_M * Math.sin(a);
          const z = spot.z + FEET_M * Math.cos(a);
          const h = drawn(t, x, z);
          const walked =
            h !== null &&
            Math.abs(h - spot.y) <= movement.max_slope * FEET_M &&
            !steep(t, x, z, false) &&
            !steep(t, x, z, true);
          if (!walked) {
            continue; // a cliff's face or a ledge's top, which may hide the player, not floor they walk on
          }
          if (cutOf(view, chest, new Vector3(x, h, z), reach) !== 'kept') {
            floor.push(`${at}: floor at (${x.toFixed(2)}, ${z.toFixed(2)})`);
          }
        }
      }
    }
    expect(hidden.length).toBeGreaterThan(3000); // the foot of every hunting map's south bank, its portal notches and its mesas
    expect(shut).toEqual([]);
    expect(floor).toEqual([]);
  });

  it('opens the sandstone cliffs’ south bank before a player at (40, 6), whom it hides from the camera', async () => {
    const map = await loadMap('sandstone_cliffs');
    const reach = reachOf(palette.toon({ texture: null, color: Color3.Gray() }, 1, true));
    const at = [...seen(map)].find((s) => s.spot.x === 40.25 && s.spot.z === 5.75)!;
    const cover = covered(map.geometry.terrain, at.cam, at.chest, 10);
    expect(cover.length).toBeGreaterThan(0);
    expect(cover.map((p) => cutOf(at.view, at.chest, p, reach))).toEqual(cover.map(() => 'open'));
  });
});
