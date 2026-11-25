import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { ClampBlock } from '@babylonjs/core/Materials/Node/Blocks/clampBlock';
import { AnimatedInputBlockTypes } from '@babylonjs/core/Materials/Node/Blocks/Input/animatedInputBlockTypes';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { TrigonometryBlock } from '@babylonjs/core/Materials/Node/Blocks/trigonometryBlock';
import { TrigonometryBlockOperations } from '@babylonjs/core/Materials/Node/Blocks/trigonometryBlock';
import type { WorleyNoise3DBlock } from '@babylonjs/core/Materials/Node/Blocks/worleyNoise3DBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector2, Vector3, Vector4 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { Palette } from './toon';
import { Torchlight } from './torchlight';

let engine: NullEngine;
let scene: Scene;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  new TargetCamera('eye', new Vector3(0, 1, -4), scene).setTarget(Vector3.Zero());
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

/** A fragment: where it is and which way it faces, the sun's light on it 0–1, the seconds the engine has run, and the nearest two of the Worley points' distances from it, in cells. */
interface Sample {
  readonly at: readonly [number, number, number];
  readonly base?: readonly [number, number, number]; // where a thin instance's model stands
  readonly normal: readonly [number, number, number];
  readonly sun: number;
  readonly seconds: number;
  readonly f1: number;
  readonly f2: number;
}

type Value = number | number[];

const FLOOR_UP: Omit<Sample, 'sun' | 'f1' | 'f2'> = {
  at: [3, 0, 7],
  normal: [0, 1, 0],
  seconds: 5,
};
const ON_EDGE = { f1: 0.5, f2: 0.5 }; // between two cells
const MIDDLE = { f1: 0.1, f2: 0.9 }; // well inside one

function each(a: Value, b: Value, f: (x: number, y: number) => number): Value {
  if (typeof a === 'number' && typeof b === 'number') {
    return f(a, b);
  }
  const n = Math.max(typeof a === 'number' ? 0 : a.length, typeof b === 'number' ? 0 : b.length);
  return Array.from({ length: n }, (_, i) =>
    f(typeof a === 'number' ? a : a[i]!, typeof b === 'number' ? b : b[i]!),
  );
}

function asValue(v: unknown): Value {
  if (typeof v === 'number') {
    return v;
  }
  if (v instanceof Color3) {
    return [v.r, v.g, v.b];
  }
  if (v instanceof Vector2 || v instanceof Vector3 || v instanceof Vector4) {
    return v.asArray();
  }
  throw new Error(`no value from ${String(v)}`);
}

const smooth = (e0: number, e1: number, x: number) => {
  const t = Math.min(1, Math.max(0, (x - e0) / (e1 - e0)));
  return t * t * (3 - 2 * t);
};

/** The value p carries for a fragment at sample, worked out from the material's own blocks as its shader runs them; seeds gathers what the Worley noise is asked at. */
function run(p: NodeMaterialConnectionPoint, at: Sample, seeds: Value[] = []): Value {
  const block = p.ownerBlock;
  const input = (name: string) => run(block.getInputByName(name)!.connectedPoint!, at, seeds);
  switch (block.getClassName()) {
    case 'InputBlock': {
      const b = block as InputBlock;
      return b.animationType === AnimatedInputBlockTypes.RealTime ? at.seconds : asValue(b.value);
    }
    case 'TransformBlock': {
      const v =
        block.name === 'worldPos'
          ? [...at.at, 1]
          : block.name === 'swayBase'
            ? [...at.base!, 1]
            : (expect(block.name).toBe('worldNormal'), [...at.normal, 0]);
      return p.name === 'xyz' ? v.slice(0, 3) : v;
    }
    case 'TrigonometryBlock':
      expect((block as TrigonometryBlock).operation).toBe(TrigonometryBlockOperations.Sin);
      return each(input('input'), 0, (v) => Math.sin(v));
    case 'LightBlock':
      expect(p.name).toBe('diffuseOutput');
      return [at.sun, at.sun, at.sun];
    case 'VectorSplitterBlock': {
      const from = ['xyzw', 'xyz ', 'xy ']
        .map((n) => block.getInputByName(n)!)
        .find((i) => i.isConnected)!;
      const v = run(from.connectedPoint!, at, seeds) as number[];
      return v['xyzw'.indexOf(p.name)]!;
    }
    case 'VectorMergerBlock':
      expect(p.name).toBe('xyz');
      return [input('x') as number, input('y') as number, input('z') as number];
    case 'WorleyNoise3DBlock': {
      seeds.push(run((block as WorleyNoise3DBlock).seed.connectedPoint!, at, seeds));
      expect(input('jitter')).toBe(1);
      return { x: at.f1, y: at.f2 }[p.name]!;
    }
    case 'NormalizeBlock': {
      const v = input('input') as number[];
      const n = Math.hypot(...v);
      return v.map((c) => c / n);
    }
    case 'AddBlock':
      return each(input('left'), input('right'), (a, b) => a + b);
    case 'SubtractBlock':
      return each(input('left'), input('right'), (a, b) => a - b);
    case 'MultiplyBlock':
      return each(input('left'), input('right'), (a, b) => a * b);
    case 'ScaleBlock':
      return each(input('input'), input('factor'), (a, b) => a * b);
    case 'OneMinusBlock':
      return each(input('input'), 0, (a) => 1 - a);
    case 'SmoothStepBlock': {
      const [e0, e1] = [input('edge0') as number, input('edge1') as number];
      return each(input('value'), 0, (v) => smooth(e0, e1, v));
    }
    case 'PosterizeBlock': {
      const steps = input('steps') as number;
      return each(input('value'), 0, (v) => Math.floor(v * steps) / steps);
    }
    case 'ClampBlock': {
      const c = block as ClampBlock;
      return each(input('value'), 0, (v) => Math.min(Math.max(v, c.minimum), c.maximum));
    }
    case 'LerpBlock': {
      const [l, r] = [input('left'), input('right')];
      const d = each(r, l, (b, a) => b - a);
      return each(
        l,
        each(d, input('gradient'), (x, t) => x * t),
        (a, x) => a + x,
      );
    }
    default:
      throw new Error(`no ${block.getClassName()} in the sea's light`);
  }
}

/** What m's block name gives at sample. */
function value(m: NodeMaterial, name: string, at: Sample, seeds?: Value[]): Value {
  return run(m.getBlockByName(name)!.outputs[0]!, at, seeds);
}

const toonOf = (p: Palette) => p.toon({ texture: null, color: Color3.White() });

/** The light the caustics add to a fragment at sample: the rippled light less the banded tone. */
function ripple(m: NodeMaterial, at: Sample): number[] {
  return each(value(m, 'rippled', at), value(m, 'tone', at), (r, t) => r - t) as number[];
}

const mean = (v: Value) => (typeof v === 'number' ? v : v.reduce((s, c) => s + c, 0) / v.length);

/** How much of the caustics' light the patches give the floor at x, z at seconds, 0 to 1. */
const patch = (m: NodeMaterial, x: number, z: number, seconds: number) =>
  value(m, 'causticPatches', { ...FLOOR_UP, at: [x, 0, z], seconds, sun: 1, ...MIDDLE }) as number;

/** A spot on the floor where the patches give the web all its light at FLOOR_UP's moment. */
function brightest(m: NodeMaterial): readonly [number, number, number] {
  for (let x = 0; x < 200; x += 2) {
    for (let z = 0; z < 200; z += 2) {
      if (patch(m, x, z, FLOOR_UP.seconds) === 1) {
        return [x, 0, z];
      }
    }
  }
  throw new Error('no patch at its brightest in 200 m');
}

/** The caustics' light in full, half what it was before the patches (0.3, 0.42, 0.42): linear, the sun's cyan. */
const HALF = [0.15, 0.21, 0.21];

describe('a sea’s toon materials (underground-and-underwater.md section 7)', () => {
  it('take the sun for their light, cyan at its top band and softer than the land’s', () => {
    const sea = toonOf(new Palette(scene, 'sea'));
    const land = toonOf(new Palette(scene));
    expect(sea.getBlockByName('light')).not.toBeNull(); // the sun's
    const tone = (m: NodeMaterial, sun: number) =>
      value(m, 'tone', { ...FLOOR_UP, sun, ...MIDDLE }) as number[];
    const [r, g, b] = tone(sea, 1);
    expect(g).toBeGreaterThan(r!);
    expect(b).toBeGreaterThan(r!); // cyan
    const contrast = (m: NodeMaterial) => mean(tone(m, 1)) / mean(tone(m, 0));
    expect(contrast(sea)).toBeLessThan(contrast(land)); // softer: its shade nearer its light
    expect(contrast(sea)).toBeGreaterThan(1.2); // yet still banded
  });

  it('ripple caustics on what faces up where the sun reaches it: light on the web between cells, none inside one, on a wall or in shade, half in the middle band', () => {
    const sea = toonOf(new Palette(scene, 'sea'));
    const lit = { ...FLOOR_UP, at: brightest(sea) };
    const full = ripple(sea, { ...lit, sun: 1, ...ON_EDGE });
    full.forEach((c, i) => expect(c).toBeCloseTo(HALF[i]!, 9)); // a patch at its brightest: all of the light, the sun's cyan
    expect(ripple(sea, { ...lit, sun: 1, ...MIDDLE })).toEqual([0, 0, 0]);
    expect(ripple(sea, { ...lit, normal: [1, 0, 0], sun: 1, ...ON_EDGE })).toEqual([0, 0, 0]);
    expect(ripple(sea, { ...lit, sun: 0.2, ...ON_EDGE })).toEqual([0, 0, 0]);
    const mid = ripple(sea, { ...lit, sun: 0.5, ...ON_EDGE });
    mid.forEach((c, i) => expect(c).toBeCloseTo(full[i]! / 2, 9));
    const leaning = ripple(sea, { ...lit, normal: [0.3, 0.95, 0], sun: 1, ...ON_EDGE });
    expect(mean(leaning)).toBeGreaterThan(mean(full) * 0.9); // a gentle slope ripples as the floor does
  });

  it('ripple by x, z and time: a head and the floor under it share the web, which drifts and shifts as time goes', () => {
    const sea = toonOf(new Palette(scene, 'sea'));
    const seed = (at: readonly [number, number, number], seconds: number) => {
      const seeds: Value[] = [];
      value(sea, 'rippled', { ...FLOOR_UP, at, seconds, sun: 1, ...ON_EDGE }, seeds);
      expect(new Set(seeds.map(String)).size).toBe(1); // read for F1 and F2, one point
      return seeds[0] as number[];
    };
    expect(seed([3, 0, 7], 5)).toEqual(seed([3, 1.8, 7], 5));
    const step = (a: number[], b: number[]) => Math.hypot(...a.map((c, i) => c - b[i]!));
    expect(step(seed([3, 0, 7], 5), seed([3, 0, 7], 6))).toBeGreaterThan(0.1); // a second on, it's moved
    expect(step(seed([3, 0, 7], 5), seed([3, 0, 7], 6))).toBeLessThan(1); // under a cell, so it ripples rather than flickers
    const across = step(seed([3, 0, 7], 5), seed([4, 0, 7], 5)); // cells a metre
    expect(1 / across).toBeGreaterThan(1.5);
    expect(1 / across).toBeLessThan(4); // a cell a couple of metres across, a few of them round a body
    expect(step(seed([3, 0, 7], 5), seed([3, 0, 8], 5))).toBeCloseTo(across, 9);
  });

  it('brighten and fade the web in patches tens of metres across, drifting slowly, from none of its light to all of half what it was', () => {
    const sea = toonOf(new Palette(scene, 'sea'));
    const shares: number[] = [];
    for (let x = 0; x <= 300; x += 10) {
      for (let z = 0; z <= 300; z += 10) {
        const at = { ...FLOOR_UP, at: [x, 0, z] as const, sun: 1, ...ON_EDGE };
        const strength = value(sea, 'causticStrength', at) as number[];
        const share = patch(sea, x, z, FLOOR_UP.seconds);
        strength.forEach((c, i) => expect(c).toBeCloseTo(HALF[i]! * share, 12)); // never past half the old light, nor under none
        expect(share).toBeGreaterThanOrEqual(0);
        expect(share).toBeLessThanOrEqual(1);
        shares.push(share);
      }
    }
    const dark = shares.filter((s) => s < 0.05).length / shares.length;
    const bright = shares.filter((s) => s > 0.95).length / shares.length;
    expect(dark).toBeGreaterThan(0.1); // patches fade out
    expect(bright).toBeGreaterThan(0.1); // and in, in full
    const runs: number[] = []; // metres along x each bright stretch lasts, a row at a time
    for (let z = 0; z <= 300; z += 50) {
      let run = 0;
      for (let x = 0; x <= 600; x += 2) {
        if (patch(sea, x, z, FLOOR_UP.seconds) > 0.5) {
          run += 2;
        } else if (run > 0) {
          runs.push(run);
          run = 0;
        }
      }
    }
    const across = runs.reduce((a, b) => a + b, 0) / runs.length;
    expect(across).toBeGreaterThan(10); // tens of metres, so never in step with the web's 2.5 m cells
    expect(across).toBeLessThan(60);
    for (let x = 0; x <= 300; x += 7) {
      const here = patch(sea, x, 40, FLOOR_UP.seconds);
      expect(Math.abs(patch(sea, x + 2.5, 40, FLOOR_UP.seconds) - here)).toBeLessThan(0.5); // a cell over, much the same
      expect(Math.abs(patch(sea, x, 40, FLOOR_UP.seconds + 1) - here)).toBeLessThan(0.1); // a second on, much the same
    }
    const moved = shares
      .slice(0, 31)
      .map((s, k) => Math.abs(patch(sea, 0, 10 * k, FLOOR_UP.seconds + 60) - s));
    expect(Math.max(...moved)).toBeGreaterThan(0.5); // a minute on, they've drifted
  });

  it('ripple nowhere on the land or in a cave', () => {
    expect(toonOf(new Palette(scene)).getBlockByName('causticCells')).toBeNull();
    const cave = toonOf(new Palette(scene, new Torchlight(scene, null)));
    expect(cave.getBlockByName('causticCells')).toBeNull();
    expect(toonOf(new Palette(scene, 'sea')).getBlockByName('causticCells')).not.toBeNull();
  });
});

describe('kelp and sea grass in a sea (underground-and-underwater.md section 7)', () => {
  const TALL = 3; // kelp's height

  /** Where a sea palette's material for a model TALL high, or its ink, draws a vertex at at over a base standing at base, at seconds. */
  function drawn(
    m: NodeMaterial,
    at: readonly [number, number, number],
    base: readonly [number, number, number],
    seconds: number,
  ): number[] {
    return value(m, 'swayed', {
      at,
      base,
      normal: [0, 1, 0],
      seconds,
      sun: 1,
      ...MIDDLE,
    }) as number[];
  }

  const palette = () => new Palette(scene, 'sea');
  const swaying = () =>
    palette().toon({ texture: null, color: Color3.White() }, 1, false, false, TALL);

  it('bend by their height over their base: the base still, the top swinging further than the middle, sideways only and not too far', () => {
    const m = swaying();
    let top = 0;
    for (let seconds = 0; seconds < 20; seconds += 0.25) {
      const at = (y: number) => drawn(m, [10, y, 4], [10, 0, 4], seconds);
      expect(at(0)).toEqual([10, 0, 4]);
      const off = (y: number) => Math.hypot(at(y)[0]! - 10, at(y)[2]! - 4);
      expect(at(TALL)[1]).toBe(TALL); // sideways only
      expect(off(TALL / 2)).toBeLessThanOrEqual(off(TALL) / 3 + 1e-12); // a bend, not a lean: the stem rises straight from its base
      top = Math.max(top, off(TALL));
    }
    expect(top).toBeGreaterThan(0.05 * TALL);
    expect(top).toBeLessThan(0.2 * TALL);
  });

  it('sway as a wave across a bed: plants side by side across the current together, plants a few metres along it out of step', () => {
    const m = swaying();
    const swing = (x: number, z: number, seconds: number) => {
      const [dx, , dz] = drawn(m, [x, TALL, z], [x, 0, z], seconds);
      return [dx! - x, dz! - z];
    };
    let apart = 0;
    for (let seconds = 0; seconds < 10; seconds += 0.5) {
      const here = swing(0, 0, seconds);
      const [hx, hz] = here as [number, number];
      const way = hx === 0 && hz === 0 ? null : Math.atan2(hz, hx);
      for (const across of [2, 5]) {
        const next = way === null ? [0, 0] : [-Math.sin(way) * across, Math.cos(way) * across];
        swing(next[0]!, next[1]!, seconds).forEach((v, i) => expect(v).toBeCloseTo(here[i]!, 9));
      }
      if (way !== null) {
        const along = swing(3 * Math.cos(way), 3 * Math.sin(way), seconds);
        apart = Math.max(apart, Math.hypot(along[0]! - here[0]!, along[1]! - here[1]!));
      }
    }
    expect(apart).toBeGreaterThan(0.05); // 3 m along, out of step
  });

  it('sway their ink with them', () => {
    const p = palette();
    const body = p.toon({ texture: null, color: Color3.White() }, 1, false, false, TALL);
    const box = CreateBox('kelp', {}, scene);
    p.outline(box, false, TALL);
    const ink = (box.getChildMeshes(true)[0] as Mesh).material as NodeMaterial;
    for (const seconds of [0, 1.3, 4.1]) {
      expect(drawn(ink, [1, 2, 3], [1, 0, 3], seconds)).toEqual(
        drawn(body, [1, 2, 3], [1, 0, 3], seconds),
      );
    }
    const still = CreateBox('rock', {}, scene);
    p.outline(still);
    expect(
      ((still.getChildMeshes(true)[0] as Mesh).material as NodeMaterial).getBlockByName('swayed'),
    ).toBeNull();
    expect(p.toon({ texture: null, color: Color3.White() }).getBlockByName('swayed')).toBeNull();
  });
});
