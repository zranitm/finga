// The place's cut-away round the player (3d-world.md section 10), checked against the drawn ground of every map.
import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DiscardBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/discardBlock';
import type { FragmentOutputBlock } from '@babylonjs/core/Materials/Node/Blocks/Fragment/fragmentOutputBlock';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { LerpBlock } from '@babylonjs/core/Materials/Node/Blocks/lerpBlock';
import type { StepBlock } from '@babylonjs/core/Materials/Node/Blocks/stepBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Matrix, Vector3, Vector4 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';
import { movement } from '@/gamedata/movement';
import { mobById } from '@/progression/mobs';
import { height, steep, top } from '@/sim/terrain';
import type { Terrain } from '@/sim/terrain';
import { servePublic } from '@/testing/public-on-disk';

import { CUT_MOBS } from './cut-mobs';
import { CHEST_M, cameraAt, heldView } from './follow';
import { loadMap } from './map';
import type { WorldMap } from './map';
import { GHOST } from './see-through';
import { Palette } from './toon';
import type { Opening } from './toon';

const SPOT = 0.5; // a player every half metre
const STEP = 0.04; // along a line
const UNDER = 0.02; // a line this far under the drawn ground is hidden by it
const ASPECT = 1280 / 800;
const JUMP = movement.jump_velocity ** 2 / (2 * movement.gravity);
const FLOOR_M = 2; // the walkable floor this far toward the camera stays; rising at most max_slope, it is off the circle past 1.4 m
const FLOOR_STEP = 0.5;
const LIFT = 0.05; // a decal over the drawn ground: the lock ring's, over the red areas' 0.04
const BACK = 0.005; // a step back toward the camera, finding where a line meets a decal
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
  const i = iz * t.nx + ix;
  const h00 = t.heights[i]!;
  const h10 = t.heights[i + 1]!;
  const h01 = t.heights[i + t.nx]!;
  const h11 = t.heights[i + t.nx + 1]!;
  return ax + az <= 1
    ? h00 + (h10 - h00) * ax + (h01 - h00) * az
    : h11 + (h01 - h11) * (1 - ax) + (h10 - h11) * (1 - az);
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

/** Whether a spot is on the fixed lattice a sweep checks, every `along` m east and every `across` m north from the map's corner. A sweep keeps every row across a bank, where the covering ground changes most: the row by its foot is the one a shallow cut fails. */
const onLattice = (s: { readonly x: number; readonly z: number }, along: number, across: number) =>
  s.x % along === SPOT / 2 && s.z % across === SPOT / 2;

/** A spot named in a failure. */
const where = (id: string, s: { readonly x: number; readonly z: number }) =>
  `${id} (${s.x}, ${s.z})`;

/** The game camera's turn, as its TargetCamera sets it: every camera sits the same way from what it looks at, so one does for all. */
const TURN = Matrix.LookAtLH(cameraAt(0, 0, 0, 0, Vector3.Zero()), Vector3.Zero(), Vector3.Up()).m;

const [T0, T1, T2, , T4, T5, T6, , T8, T9, T10] = TURN as unknown as number[];

/** A point in the world, as plain numbers: the sweeps make many. */
type Point = [number, number, number];

/** A point in the view of the camera at cam: across, up and away from it. */
type View = readonly [number, number, number];
function inView(cam: Vector3, p: Vector3): View {
  const dx = p.x - cam.x;
  const dy = p.y - cam.y;
  const dz = p.z - cam.z;
  return [
    dx * T0! + dy * T4! + dz * T8!,
    dx * T1! + dy * T5! + dz * T9!,
    dx * T2! + dy * T6! + dz * T10!,
  ];
}

/** How the point (x, y, z) stands to the cut round c, c in the view of the camera at cam: fully open, dithered, or kept. */
function cutOf(
  cam: Vector3,
  c: View,
  x: number,
  y: number,
  z: number,
  reach: Reach,
): 'open' | 'dithered' | 'kept' {
  const dx = x - cam.x;
  const dy = y - cam.y;
  const dz = z - cam.z;
  const qz = dx * T2! + dy * T6! + dz * T10!;
  if (c[2] - qz < reach.nearer) {
    return 'kept';
  }
  const k = c[2] / Math.max(qz, 0.01);
  const across = (dx * T0! + dy * T4! + dz * T8!) * k - c[0];
  const up = (dx * T1! + dy * T5! + dz * T9!) * k - c[1];
  const away = Math.hypot(across, up < 0 ? up * reach.below : up);
  return away <= reach.inner ? 'open' : away < reach.outer ? 'dithered' : 'kept';
}

/** A mob slot's reach, off a place material once it opens round a mob: how far past its radius on screen it opens fully, the dither past that, and how far over its feet the place must be. */
function mobReachOf(m: NodeMaterial) {
  const probe = { x: 0, y: 0, z: 0, radius: 1, height: 1 };
  palette.cutAround(0, 0, 0, [probe]);
  return {
    pad: (input(m, 'cutMob0').valueCallback() as Vector4).w - probe.radius,
    edge: input(m, 'cutEdgeBy').value as number,
    over: (input(m, 'cutMob0Over').valueCallback() as number) - probe.y,
  };
}
type MobReach = ReturnType<typeof mobReachOf>;

/** How a place point stands to the hole round a mob of radius, its feet at y feet and its middle at mid in the camera's view: kept unless it's over the height there and in front of the middle, and round it on screen as the player's is, but not squashed. */
function mobCutOf(
  cam: Vector3,
  mid: View,
  feet: number,
  radius: number,
  [x, y, z]: readonly [number, number, number],
  r: MobReach,
): 'open' | 'dithered' | 'kept' {
  if (y <= feet + r.over) {
    return 'kept';
  }
  const inner = radius + r.pad;
  return cutOf(cam, mid, x, y, z, { nearer: 0, inner, outer: inner + r.edge, below: 1 });
}

/** Where the line a covered point lies on enters the drawn ground from the camera's side: the point of each run nearest the camera, on the ground. These are the fragments drawn over what the line reaches; the ground beyond is seen from inside, and its back faces aren't drawn. */
function fragments(t: Terrain, cover: readonly Point[]): Point[] {
  const out: Point[] = [];
  for (let i = 0; i < cover.length; i++) {
    const [x, y, z] = cover[i]!;
    const n = cover[i + 1];
    if (n === undefined || Math.hypot(n[0] - x, n[1] - y, n[2] - z) > STEP * 1.5) {
      out.push([x, drawn(t, x, z) ?? y, z]);
    }
  }
  return out;
}

/** Where the line from cam that a covered point lies on meets a decal laid LIFT m over the drawn ground, from the camera's side: back from each run's point nearest the camera, the first point over the decal's height. A red area or a ring drawn there lies in front of what the line reaches. */
function decals(t: Terrain, cam: Vector3, cover: readonly Point[]): Point[] {
  const out: Point[] = [];
  for (let i = 0; i < cover.length; i++) {
    const [x, y, z] = cover[i]!;
    const n = cover[i + 1];
    if (n !== undefined && Math.hypot(n[0] - x, n[1] - y, n[2] - z) <= STEP * 1.5) {
      continue;
    }
    const len = Math.hypot(cam.x - x, cam.y - y, cam.z - z);
    for (let s = BACK; s < 1; s += BACK) {
      const p: Point = [
        x + ((cam.x - x) * s) / len,
        y + ((cam.y - y) * s) / len,
        z + ((cam.z - z) * s) / len,
      ];
      if (p[1] >= (drawn(t, p[0], p[2]) ?? -Infinity) + LIFT) {
        out.push(p);
        break;
      }
    }
  }
  return out;
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
function covered(g: Ground, cam: Vector3, target: Vector3): Point[] {
  const c = g.t.cell;
  const row = Math.min(Math.max(Math.floor(target.z / c), 0), g.cz - 1); // the camera is south of what it looks at, so the line runs south from here
  const last = Math.min(Math.max(Math.floor(Math.max(cam.x, target.x) / c), 0), g.cx - 1);
  let highest = -Infinity;
  for (
    let ix = Math.min(Math.max(Math.floor(Math.min(cam.x, target.x) / c), 0), g.cx - 1);
    ix <= last;
    ix++
  ) {
    highest = Math.max(highest, g.south[row * g.cx + ix]!);
  }
  const out: Point[] = [];
  if (highest <= target.y) {
    return out; // no ground the line passes over rises to it
  }
  const len = Vector3.Distance(cam, target);
  const rise = (cam.y - target.y) / len;
  const end = Math.min(len - STEP, (highest - target.y) / rise);
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
        out.push([px, py, pz]);
      }
    }
  }
  return out;
}

/** A reachable spot as the game's camera sees it: the camera, the chest, also in the camera's view, and the head. */
interface Seen {
  readonly spot: { readonly x: number; readonly z: number; readonly y: number };
  readonly cam: Vector3;
  readonly chest: Vector3;
  readonly chestView: View;
  readonly head: Vector3;
}

/** Each reachable spot of map as the game's camera sees it. */
function* seen(map: WorldMap, spots = reachable(map)): Generator<Seen> {
  const g = map.geometry;
  const look = Vector3.Zero();
  for (const s of spots) {
    heldView({ x: s.x, y: s.y + CHEST_M, z: s.z }, g.size_x, g.size_z, ASPECT, 0, look);
    const cam = cameraAt(look.x, look.y, look.z, 0, Vector3.Zero());
    const chest = new Vector3(s.x, s.y + CHEST_M, s.z);
    yield {
      spot: s,
      cam,
      chest,
      chestView: inView(cam, chest),
      head: new Vector3(s.x, s.y + movement.height, s.z),
    };
  }
}

/** Every map as the sweeps read it, built once for the file: its drawn ground, the spots a player reaches, and each as the camera sees it. */
let swept: {
  readonly id: string;
  readonly map: WorldMap;
  readonly ground: Ground;
  readonly spots: readonly { x: number; z: number; y: number }[];
  readonly seen: readonly Seen[];
}[] = [];

beforeAll(async () => {
  servePublic();
  swept = [];
  for (const { id } of maps) {
    if (id === 'willowmere' || id === 'sunreach') {
      continue; // 1 km of ground: a sweep costs tens of seconds under coverage, and test_region and the small maps run the same code
    }
    const map = await loadMap(id);
    const spots = reachable(map);
    swept.push({
      id,
      map,
      ground: groundOf(map.geometry.terrain),
      spots,
      seen: [...seen(map, spots)],
    });
  }
});

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
    palette.cutAround(3, 1.9, 7, []);
    expect(input(cut, 'cutOn').valueCallback()).toBe(1);
    expect(input(cut, 'cutChest').valueCallback()).toEqual(new Vector3(3, 1.9, 7));
    expect(input(await inkOf(box), 'cutChest').valueCallback()).toEqual(new Vector3(3, 1.9, 7));
  });

  it(`opens round up to ${CUT_MOBS} mobs, each from its middle out to its radius and half a metre on screen, where the place is over 1.5 m above its feet; an empty slot opens nothing`, async () => {
    const cut = await built(
      palette.toon({ texture: null, color: new Color3(0.4, 0.5, 0.3) }, 1, true),
    );
    const slot = (i: number) => input(cut, `cutMob${i}`).valueCallback() as Vector4;
    const empty = () => Array.from({ length: CUT_MOBS }, (_, i) => slot(i).w);
    expect(empty().every((w) => w < 0)).toBe(true); // a reach under 0 opens nothing, as a fragment is never nearer than that
    const boar: Opening = { x: 3, y: 1, z: 7, radius: 0.875, height: 1.25 };
    const five = Array.from({ length: CUT_MOBS + 1 }, (_, i) => ({ ...boar, x: i }));
    palette.cutAround(0, 1.9, 0, [boar]);
    expect(slot(0)).toEqual(new Vector4(3, 1.625, 7, 1.375));
    expect(input(cut, 'cutMob0Over').valueCallback()).toBe(2.5); // a bank's top, 2.5 m and more, opens; a ramp or ridge beside the mob stays
    expect(input(cut, 'cutEdgeBy').value).toBe(0.4); // dithered as the player's hole is, from 1 to 1.4 m
    expect(
      empty()
        .slice(1)
        .every((w) => w < 0),
    ).toBe(true);
    palette.cutAround(0, 1.9, 0, five);
    expect(empty().every((w) => w > 0)).toBe(true);
    expect(slot(CUT_MOBS - 1).x).toBe(CUT_MOBS - 1);
    palette.cutAround(0, 1.9, 0, []);
    expect(empty().every((w) => w < 0)).toBe(true);
  });

  it('lays a boss’s area or the lock ring on the place as a decal, drawn faint, not dropped, wherever the place opens round the player or a mob, and only where the place is nearest', async () => {
    const place = await built(palette.toon({ texture: null, color: Color3.Gray() }, 1, true));
    const red = new Color3(1, 0.16, 0.12);
    const fill = await built(palette.decal(red, 0.6, Constants.ALPHA_COMBINE));
    const glow = await built(palette.decal(red, 0.4, Constants.ALPHA_PREMULTIPLIED));
    const ring = await built(palette.decal(new Color3(1, 0.86, 0.45), 1, Constants.ALPHA_COMBINE));
    const decals = [fill, glow, ring];
    const discards = (m: NodeMaterial) => m.attachedBlocks.filter((b) => b instanceof DiscardBlock);
    expect(decals.map((m) => discards(m).length)).toEqual([0, 0, 0]); // the part on opened place still shows
    const drop = discards(place)[0]!;
    const from = (p: NodeMaterialConnectionPoint) => p.connectedPoint?.ownerBlock.name;
    for (const m of decals) {
      const kept = m.getBlockByName('cutKept') as StepBlock;
      expect([from(kept.value), from(kept.edge)]).toEqual([from(drop.value), from(drop.cutoff)]); // where the place's own fragment goes, by the same noise
      const faint = m.getBlockByName('cutFaint') as LerpBlock;
      expect([
        (faint.left.connectedPoint!.ownerBlock as InputBlock).value,
        (faint.right.connectedPoint!.ownerBlock as InputBlock).value,
        faint.gradient.connectedPoint!.ownerBlock,
      ]).toEqual([GHOST, 1, kept]); // as faint as a ghost there, whole where kept
    }
    const out = (m: NodeMaterial, to: 'rgb' | 'a') =>
      (m.getBlockByName('fragmentOutput') as FragmentOutputBlock)[to].connectedPoint!.ownerBlock;
    for (const m of decals) {
      expect(out(m, 'a').inputs.map((i) => i.connectedPoint?.ownerBlock.name)).toEqual([
        'alpha',
        'cutFaint',
      ]); // its alpha, faded
    }
    expect(out(glow, 'rgb').inputs.map((i) => i.connectedPoint?.ownerBlock.name)).toEqual([
      'color',
      'cutFaint',
    ]); // a glow's light fades with its alpha, as the blend takes its colour as written
    expect([fill, ring].map((m) => out(m, 'rgb').name)).toEqual(['color', 'color']); // the blend takes the rest's by their alpha
    expect(reachOf(fill)).toEqual(reachOf(place));
    expect(mobReachOf(fill)).toEqual(mobReachOf(place));
    const boar: Opening = { x: 3, y: 1, z: 7, radius: 0.875, height: 1.25 };
    palette.cutAround(3, 1.9, 7, [boar]);
    for (const name of ['cutOn', 'cutChest', 'cutMob0', 'cutMob0Over']) {
      expect(input(fill, name).valueCallback()).toEqual(input(place, name).valueCallback()); // the place's own state
    }
    expect([
      fill.alphaMode,
      fill.disableDepthWrite,
      fill.zOffset,
      fill.stencil.func,
      fill.stencil.funcRef,
      fill.stencil.opStencilDepthPass,
    ]).toEqual([Constants.ALPHA_COMBINE, true, -2, Constants.EQUAL, 0, Constants.KEEP]); // on the place, never on a body, even faint
    expect(decals.map((m) => m.needAlphaBlending())).toEqual([true, true, true]); // the ring too, to be faint
  });

  it('opens every line from the game’s camera to a reachable chest and head that the drawn ground covers, and draws a decal laid on that ground faint there, at spots every 2 m east and every row north on every map, and keeps the walkable floor before the feet', () => {
    const reach = reachOf(palette.toon({ texture: null, color: Color3.Gray() }, 1, true));
    const decal = reachOf(palette.decal(Color3.Red(), 1, Constants.ALPHA_PREMULTIPLIED));
    const hidden: string[] = [];
    const shut: string[] = [];
    const whole: string[] = []; // a decal drawn whole in front of what the line reaches
    let crossed = 0; // where a line meets a decal in front of the chest or head
    const floor: string[] = [];
    for (const { id, map, ground, seen: all } of swept) {
      const t = map.geometry.terrain;
      for (const { spot, cam, chestView, chest, head } of all) {
        if (!onLattice(spot, 2, SPOT)) {
          continue;
        }
        const chestCover = covered(ground, cam, chest);
        if (chestCover.length > 0) {
          hidden.push(where(id, spot));
        }
        const headCover = chestCover.length > 0 ? covered(ground, cam, head) : []; // the head's line runs over the chest's, on the same ground
        for (const p of [...chestCover, ...headCover]) {
          if (cutOf(cam, chestView, ...p, reach) !== 'open') {
            shut.push(`${where(id, spot)}: (${p.map((v) => v.toFixed(2)).join(', ')})`);
            break;
          }
        }
        for (const p of [...decals(t, cam, chestCover), ...decals(t, cam, headCover)]) {
          crossed++;
          if (cutOf(cam, chestView, ...p, decal) !== 'open') {
            whole.push(`${where(id, spot)}: (${p.map((v) => v.toFixed(2)).join(', ')})`);
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
            if (cutOf(cam, chestView, x, h, z, reach) !== 'kept') {
              floor.push(`${where(id, spot)}: floor ${d} m out`);
              break;
            }
          }
        }
      }
    }
    expect(hidden.length).toBeGreaterThan(300); // the foot of the south bank of each land map swept (the crossroads, the frost trail and the rest the data still has), its portal notches and its mesas; 323 on the maps swept now, over 700 with the six the desert retired
    expect(shut).toEqual([]);
    expect(crossed).toBeGreaterThan(495); // 522 on the maps swept now, over 1,100 with the six the desert retired
    expect(whole).toEqual([]);
    expect(floor).toEqual([]);
  });

  it('opens round the mobs by a hidden player on every map, so a bank hides neither their bodies nor their lock rings and no decal on it hangs whole in front of them, and opens no walkable floor round them or round a player above them', async () => {
    const cut = await built(
      palette.toon({ texture: null, color: new Color3(0.4, 0.5, 0.3) }, 1, true),
    );
    const reach = reachOf(cut);
    const mob = mobReachOf(cut);
    const ring = await built(palette.decal(Color3.Red(), 1, Constants.ALPHA_COMBINE));
    const decal = { reach: reachOf(ring), mob: mobReachOf(ring) };
    let near = 0; // the mobs placed by a hidden player that the ground covers
    let crossed = 0; // where a line the cut opens meets a decal in front of a mob
    const whole: string[] = []; // a decal drawn whole in front of what the line reaches
    const hidden: string[] = [];
    const floor: string[] = [];
    for (const { id, map, ground, spots, seen: all } of swept) {
      const t = map.geometry.terrain;
      const kinds = (maps.find((m) => m.id === id)?.spawns ?? []).flatMap((s) => {
        const m = mobById(s.mob);
        return m && !m.boss ? [m] : [];
      });
      if (kinds.length === 0) {
        continue;
      }
      const { radius, height: tall } = kinds.reduce((a, b) => (b.radius > a.radius ? b : a)); // the widest, whose ring reaches furthest
      const stands = new Set(spots.map((s) => `${s.x} ${s.z}`));
      const on = (x: number, z: number) =>
        stands.has(`${(Math.floor(x / SPOT) + 0.5) * SPOT} ${(Math.floor(z / SPOT) + 0.5) * SPOT}`);
      /** Whether the hole round a mob at feet (x, y, z) opens walkable floor walked out from (fx, fz) at fy, out to `out`. */
      const opensFloor = (
        cam: Vector3,
        x: number,
        y: number,
        z: number,
        fx: number,
        fy: number,
        fz: number,
        out: number,
      ) => {
        const mid = inView(cam, new Vector3(x, y + tall / 2, z));
        for (let k = 0; k < 8; k++) {
          const a = (k * Math.PI) / 4;
          let was = fy;
          for (let d = FLOOR_STEP; d <= out; d += FLOOR_STEP) {
            const px = fx + Math.sin(a) * d;
            const pz = fz + Math.cos(a) * d;
            const h = drawn(t, px, pz);
            if (h === null || Math.abs(h - was) > movement.max_slope * FLOOR_STEP) {
              break;
            }
            was = h;
            if (mobCutOf(cam, mid, y, radius, [px, h, pz], mob) !== 'kept') {
              return true;
            }
          }
        }
        return false;
      };
      for (const { spot, cam, chest, chestView } of all) {
        if (onLattice(spot, 8, 4)) {
          // the player's floor, with mobs round them
          for (let k = 0; k < 8; k++) {
            for (const d of [1.5, 3, 4.5]) {
              const x = spot.x + Math.sin((k * Math.PI) / 4) * d;
              const z = spot.z + Math.cos((k * Math.PI) / 4) * d;
              if (on(x, z) && opensFloor(cam, x, height(t, x, z), z, spot.x, spot.y, spot.z, 1)) {
                floor.push(
                  `${where(id, spot)}: the player's floor, a mob at (${x.toFixed(2)}, ${z.toFixed(2)})`,
                );
              }
            }
          }
        }
        // mobs round the hidden spots every 16 m along the bank, in every row
        if (!onLattice(spot, 16, SPOT) || covered(ground, cam, chest).length === 0) {
          continue;
        }
        for (const [dx, dz] of [
          [1, 0],
          [-1, 0],
          [0, 1],
          [Math.SQRT1_2, Math.SQRT1_2],
        ] as const) {
          for (const d of [1.5, 3]) {
            const x = spot.x + dx * d;
            const z = spot.z + dz * d;
            if (!on(x, z)) {
              continue;
            }
            const y = height(t, x, z);
            const midView = inView(cam, new Vector3(x, y + tall / 2, z));
            const points: Vector3[] = [new Vector3(x, y + tall, z)];
            for (let a = -2; a <= 2; a++) {
              for (const up of [0.05, tall / 2, tall * 0.95]) {
                const px = x + 0.8 * radius * Math.sin((a * Math.PI) / 4);
                const pz = z - 0.8 * radius * Math.cos((a * Math.PI) / 4);
                if (y + up >= (drawn(t, px, pz) ?? y) + 0.05) {
                  points.push(new Vector3(px, y + up, pz)); // its front, but not where its bottom sinks into a slope
                }
              }
            }
            for (let a = 0; a < 12; a++) {
              const px = x + (radius + 0.15) * Math.sin((a * Math.PI) / 6);
              const pz = z + (radius + 0.15) * Math.cos((a * Math.PI) / 6);
              if ((drawn(t, px, pz) ?? y) < y + 0.05) {
                points.push(new Vector3(px, y + 0.05, pz)); // the lock ring, flat at its feet as lock-ring.ts lays it, where it isn't under the ground
              }
            }
            const lines = points.map((q) => covered(ground, cam, q)).filter((c) => c.length > 0);
            if (lines.length === 0) {
              continue;
            }
            near++;
            const shut = (c: readonly Point[]) =>
              fragments(t, c).some(
                (q) =>
                  cutOf(cam, chestView, ...q, reach) !== 'open' &&
                  mobCutOf(cam, midView, y, radius, q, mob) !== 'open',
              );
            if (lines.some(shut)) {
              hidden.push(`${where(id, spot)}: a mob at (${x.toFixed(2)}, ${z.toFixed(2)})`);
            }
            for (const c of lines.filter((l) => !shut(l))) {
              for (const d of decals(t, cam, c)) {
                crossed++;
                if (
                  cutOf(cam, chestView, ...d, decal.reach) !== 'open' &&
                  mobCutOf(cam, midView, y, radius, d, decal.mob) !== 'open'
                ) {
                  whole.push(`${where(id, spot)}: a mob at (${x.toFixed(2)}, ${z.toFixed(2)})`);
                }
              }
            }
            if (opensFloor(cam, x, y, z, x, y, z, 3)) {
              floor.push(
                `${where(id, spot)}: the floor round a mob at (${x.toFixed(2)}, ${z.toFixed(2)})`,
              );
            }
          }
        }
      }
    }
    expect(near).toBeGreaterThan(190); // 208 on the maps swept now, over 450 with the six the desert retired
    expect(hidden.length / near).toBeLessThan(0.02); // a dune under 1.5 m over a mob's feet may still cover its bottom
    expect(crossed).toBeGreaterThan(3550); // 3,737 on the maps swept now, over 8,500 with the six the desert retired
    expect(whole).toEqual([]);
    expect(floor).toEqual([]);
  });
});
