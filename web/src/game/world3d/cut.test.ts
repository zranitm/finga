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
import { height, steep, top } from '@/sim/terrain';
import type { Terrain } from '@/sim/terrain';
import { servePublic } from '@/testing/public-on-disk';

import { CHEST_M, cameraAt, heldView } from './follow';
import { loadMap } from './map';
import type { WorldMap } from './map';
import { Palette } from './toon';

const SPOT = 0.5; // a player every half metre
const STEP = 0.04; // along a line
const UNDER = 0.02; // a line this far under the drawn ground is hidden by it
const ASPECT = 1280 / 800;
const JUMP = movement.jump_velocity ** 2 / (2 * movement.gravity);
const FLOOR_M = 2; // the walkable floor this far toward the camera stays; rising at most max_slope, it is off the circle past 1.4 m
const FLOOR_STEP = 0.5;
// only floor toward the camera, south of the feet, comes into the cut: beside or behind them it's off the circle before it's near enough
const TOWARD = [
  [0, -1],
  [Math.SQRT1_2, -Math.SQRT1_2],
  [-Math.SQRT1_2, -Math.SQRT1_2],
] as const;

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
    below: input(m, 'cutBelow').value as number,
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
  const blocked = new Uint8Array(nx * nz); // each solid marks the spots its shape, widened by a body's radius, holds
  for (const s of map.solids) {
    const [x, z, ex, ez] =
      s.kind === 'cylinder'
        ? [s.cylinder.x, s.cylinder.z, s.cylinder.radius, s.cylinder.radius]
        : [
            s.box.x,
            s.box.z,
            Math.abs(s.box.ux) * s.box.half_w + Math.abs(s.box.uz) * s.box.half_d,
            Math.abs(s.box.uz) * s.box.half_w + Math.abs(s.box.ux) * s.box.half_d,
          ];
    for (
      let j = Math.max(0, Math.floor((z - ez - r) / SPOT));
      j < Math.min(nz, Math.ceil((z + ez + r) / SPOT));
      j++
    ) {
      for (
        let i = Math.max(0, Math.floor((x - ex - r) / SPOT));
        i < Math.min(nx, Math.ceil((x + ex + r) / SPOT));
        i++
      ) {
        const dx = at(i) - x;
        const dz = at(j) - z;
        const holds =
          s.kind === 'cylinder'
            ? dx * dx + dz * dz < (s.cylinder.radius + r) ** 2
            : Math.abs(dx * s.box.ux + dz * s.box.uz) < s.box.half_w + r &&
              Math.abs(-dx * s.box.uz + dz * s.box.ux) < s.box.half_d + r;
        if (holds) {
          blocked[j * nx + i] = 1;
        }
      }
    }
  }
  const ys: number[] = [];
  const ok: boolean[] = [];
  for (let j = 0; j < nz; j++) {
    for (let i = 0; i < nx; i++) {
      const inside = at(i) >= r && at(j) >= r && at(i) <= g.size_x - r && at(j) <= g.size_z - r;
      ok.push(inside && blocked[j * nx + i] === 0);
      ys.push(height(g.terrain, at(i), at(j))); // clear of the solids, the feet stand on the terrain
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

/** The game camera's turn, as its TargetCamera sets it: every camera sits the same way from what it looks at, so one does for all. */
const TURN = Matrix.LookAtLH(cameraAt(0, 0, 0, Vector3.Zero()), Vector3.Zero(), Vector3.Up()).m;

/** How a point stands to the cut round chest, seen by the camera at cam: fully open, dithered, or kept. */
function cutOf(
  cam: Vector3,
  chest: Vector3,
  [px, py, pz]: readonly [number, number, number],
  reach: Reach,
): 'open' | 'dithered' | 'kept' {
  const at = (x: number, y: number, z: number, i: number) =>
    (x - cam.x) * TURN[i]! + (y - cam.y) * TURN[i + 4]! + (z - cam.z) * TURN[i + 8]!; // in the camera's view
  const cz = at(chest.x, chest.y, chest.z, 2);
  const qz = at(px, py, pz, 2);
  if (cz - qz < reach.nearer) {
    return 'kept';
  }
  const k = cz / Math.max(qz, 0.01);
  const across = at(px, py, pz, 0) * k - at(chest.x, chest.y, chest.z, 0);
  const up = at(px, py, pz, 1) * k - at(chest.x, chest.y, chest.z, 1);
  const away = Math.hypot(across, up < 0 ? up * reach.below : up);
  return away <= reach.inner ? 'open' : away < reach.outer ? 'dithered' : 'kept';
}

/** A map's drawn ground with, for each cell, the highest of its own and its neighbours' corners: no line passing over that within a cell of it meets the ground there. */
function groundOf(t: Terrain) {
  const cx = t.nx - 1;
  const cz = t.nz - 1;
  const own = new Float64Array(cx * cz);
  for (let iz = 0; iz < cz; iz++) {
    for (let ix = 0; ix < cx; ix++) {
      const i = iz * t.nx + ix;
      own[iz * cx + ix] = Math.max(
        t.heights[i]!,
        t.heights[i + 1]!,
        t.heights[i + t.nx]!,
        t.heights[i + t.nx + 1]!,
      );
    }
  }
  const near = new Float64Array(cx * cz).fill(-Infinity);
  for (let iz = 0; iz < cz; iz++) {
    for (let ix = 0; ix < cx; ix++) {
      for (let dz = -1; dz <= 1; dz++) {
        for (let dx = -1; dx <= 1; dx++) {
          const a = ix + dx;
          const b = iz + dz;
          if (a >= 0 && b >= 0 && a < cx && b < cz) {
            near[iz * cx + ix] = Math.max(near[iz * cx + ix]!, own[b * cx + a]!);
          }
        }
      }
    }
  }
  const south = new Float64Array(cx * cz); // the highest corner in the cell's column, from it to the map's south edge
  for (let iz = 0; iz < cz; iz++) {
    for (let ix = 0; ix < cx; ix++) {
      south[iz * cx + ix] = Math.max(
        own[iz * cx + ix]!,
        iz > 0 ? south[(iz - 1) * cx + ix]! : -Infinity,
      );
    }
  }
  return { t, near, south, cx, cz };
}
type Ground = ReturnType<typeof groundOf>;

const STRIDE = 0.25; // a line moves 0.16 m across the ground in a stride, under a cell, so a stride starting over its cell's neighbourhood stays over it

/** The points on the line from cam to target that the drawn ground covers, sampled every STEP m along the strides that start low enough to meet it. */
function covered(g: Ground, cam: Vector3, target: Vector3): Vector3[] {
  const len = Vector3.Distance(cam, target);
  const rise = (cam.y - target.y) / len;
  const cell = (v: number, n: number) => Math.min(Math.max(Math.floor(v / g.t.cell), 0), n - 1);
  const row = cell(target.z, g.cz); // the camera is south of what it looks at, so the line runs south from here
  let highest = -Infinity;
  for (
    let ix = cell(Math.min(cam.x, target.x), g.cx);
    ix <= cell(Math.max(cam.x, target.x), g.cx);
    ix++
  ) {
    highest = Math.max(highest, g.south[row * g.cx + ix]!);
  }
  const end = Math.min(len - STEP, (highest - target.y) / rise);
  const out: Vector3[] = [];
  for (let from = 0; from < end; from += STRIDE) {
    const k = from / len;
    const x = target.x + (cam.x - target.x) * k;
    const y = target.y + (cam.y - target.y) * k;
    const z = target.z + (cam.z - target.z) * k;
    const ix = Math.floor(x / g.t.cell);
    const iz = Math.floor(z / g.t.cell);
    const near = ix >= 0 && iz >= 0 && ix < g.cx && iz < g.cz ? g.near[iz * g.cx + ix]! : -Infinity;
    if (y >= near - UNDER) {
      continue; // the line only rises along the stride, over ground no higher than near
    }
    for (let s = Math.max(from, STEP); s < Math.min(from + STRIDE, end + STEP / 2); s += STEP) {
      const f = s / len;
      const px = target.x + (cam.x - target.x) * f;
      const py = target.y + (cam.y - target.y) * f;
      const pz = target.z + (cam.z - target.z) * f;
      const h = drawn(g.t, px, pz);
      if (h !== null && py < h - UNDER) {
        out.push(new Vector3(px, py, pz));
      }
    }
  }
  return out;
}

/** Each reachable spot of map as the game's camera sees it: the camera, the chest and the head. */
function* seen(map: WorldMap) {
  const g = map.geometry;
  const look = Vector3.Zero();
  for (const s of reachable(map)) {
    heldView({ x: s.x, y: s.y + CHEST_M, z: s.z }, g.size_x, g.size_z, ASPECT, look);
    const cam = cameraAt(look.x, look.y, look.z, Vector3.Zero());
    yield {
      spot: s,
      cam,
      chest: new Vector3(s.x, s.y + CHEST_M, s.z),
      head: new Vector3(s.x, s.y + movement.height, s.z),
    };
  }
}

describe('the cut-away round the player', () => {
  it('drops a place fragment over 0.5 m nearer the camera than the chest and within about 1.2 m of it on screen, dithered at the edge, and squashed below it; a body’s material has no cut', async () => {
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
    expect(reachOf(cut).below).toBe(1.5); // the feet, 0.58 m below the chest on screen, stay well inside
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

  it('opens every line from the game’s camera to a reachable chest and head that the drawn ground covers, on every map, and keeps the walkable floor before the feet', async () => {
    const reach = reachOf(palette.toon({ texture: null, color: Color3.Gray() }, 1, true));
    const hidden: string[] = [];
    const shut: string[] = [];
    const floor: string[] = [];
    for (const { id } of maps) {
      const map = await loadMap(id);
      const t = map.geometry.terrain;
      const ground = groundOf(t);
      for (const { spot, cam, chest, head } of seen(map)) {
        const at = `${id} (${spot.x}, ${spot.z})`;
        const chestCover = covered(ground, cam, chest);
        if (chestCover.length > 0) {
          hidden.push(at);
        }
        const headCover = chestCover.length > 0 ? covered(ground, cam, head) : []; // the head's line runs over the chest's, on the same ground
        for (const p of [...chestCover, ...headCover]) {
          if (cutOf(cam, chest, [p.x, p.y, p.z], reach) !== 'open') {
            shut.push(`${at}: (${p.x.toFixed(2)}, ${p.y.toFixed(2)}, ${p.z.toFixed(2)})`);
            break;
          }
        }
        for (const [dx, dz] of TOWARD) {
          let was = spot.y;
          for (let d = FLOOR_STEP; d <= FLOOR_M; d += FLOOR_STEP) {
            const x = spot.x + dx * d;
            const z = spot.z + dz * d;
            const h = drawn(t, x, z);
            if (h === null || Math.abs(h - was) > movement.max_slope * FLOOR_STEP) {
              break; // off the walkable floor: a cliff's face or a ledge's top may open, as they can hide the player
            }
            was = h;
            if (cutOf(cam, chest, [x, h, z], reach) !== 'kept') {
              floor.push(`${at}: floor ${d} m out`);
              break;
            }
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
    const cover = covered(groundOf(map.geometry.terrain), at.cam, at.chest);
    expect(cover.length).toBeGreaterThan(0);
    expect(cover.map((p) => cutOf(at.cam, at.chest, [p.x, p.y, p.z], reach))).toEqual(
      cover.map(() => 'open'),
    );
  });
});
