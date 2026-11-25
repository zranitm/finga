import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { ClampBlock } from '@babylonjs/core/Materials/Node/Blocks/clampBlock';
import { AnimatedInputBlockTypes } from '@babylonjs/core/Materials/Node/Blocks/Input/animatedInputBlockTypes';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { WorleyNoise3DBlock } from '@babylonjs/core/Materials/Node/Blocks/worleyNoise3DBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector2, Vector3, Vector4 } from '@babylonjs/core/Maths/math.vector';
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
    case 'TransformBlock':
      if (block.name === 'worldPos') {
        return [...at.at, 1];
      }
      expect(block.name).toBe('worldNormal');
      return [...at.normal, 0];
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
    const full = ripple(sea, { ...FLOOR_UP, sun: 1, ...ON_EDGE });
    expect(Math.min(...full)).toBeGreaterThan(0.2);
    expect(full[1]).toBeGreaterThan(full[0]!); // the sun's cyan
    expect(ripple(sea, { ...FLOOR_UP, sun: 1, ...MIDDLE })).toEqual([0, 0, 0]);
    expect(ripple(sea, { ...FLOOR_UP, normal: [1, 0, 0], sun: 1, ...ON_EDGE })).toEqual([0, 0, 0]);
    expect(ripple(sea, { ...FLOOR_UP, sun: 0.2, ...ON_EDGE })).toEqual([0, 0, 0]);
    const mid = ripple(sea, { ...FLOOR_UP, sun: 0.5, ...ON_EDGE });
    mid.forEach((c, i) => expect(c).toBeCloseTo(full[i]! / 2, 9));
    const leaning = ripple(sea, { ...FLOOR_UP, normal: [0.3, 0.95, 0], sun: 1, ...ON_EDGE });
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

  it('ripple nowhere on the land or in a cave', () => {
    expect(toonOf(new Palette(scene)).getBlockByName('causticCells')).toBeNull();
    const cave = toonOf(new Palette(scene, new Torchlight(scene, null)));
    expect(cave.getBlockByName('causticCells')).toBeNull();
    expect(toonOf(new Palette(scene, 'sea')).getBlockByName('causticCells')).not.toBeNull();
  });
});
