import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { ClampBlock } from '@babylonjs/core/Materials/Node/Blocks/clampBlock';
import type { TextureBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/textureBlock';
import { AnimatedInputBlockTypes } from '@babylonjs/core/Materials/Node/Blocks/Input/animatedInputBlockTypes';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { TrigonometryBlock } from '@babylonjs/core/Materials/Node/Blocks/trigonometryBlock';
import { TrigonometryBlockOperations } from '@babylonjs/core/Materials/Node/Blocks/trigonometryBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { NodeMaterialConnectionPoint } from '@babylonjs/core/Materials/Node/nodeMaterialBlockConnectionPoint';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector2, Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { grounds } from '@/gamedata/grounds';
import { fixtureFile } from '@/testing/region-fixture';

import { readLight } from './region-bake';
import { Palette } from './toon';
import { flicker, FLICKERS, FLOOR, TORCH, Torchlight } from './torchlight';

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

/** Where a fragment samples the light map: its texel's two groups, 0–1, and the seconds the engine has run. */
interface Sample {
  readonly a: number;
  readonly b: number;
  readonly seconds: number;
}

/** The value p carries for a fragment at sample, worked out from the material's own blocks, as its shader runs them. */
function run(p: NodeMaterialConnectionPoint, at: Sample): number {
  const block = p.ownerBlock;
  const input = (name: string) => run(block.getInputByName(name)!.connectedPoint!, at);
  switch (block.getClassName()) {
    case 'InputBlock': {
      const b = block as InputBlock;
      return b.animationType === AnimatedInputBlockTypes.RealTime
        ? at.seconds
        : (b.value as number);
    }
    case 'TextureBlock':
      expect((block as TextureBlock).name).toBe('lightMap');
      return { r: at.a, g: at.b }[p.name]!;
    case 'AddBlock':
      return input('left') + input('right');
    case 'MultiplyBlock':
      return input('left') * input('right');
    case 'ScaleBlock':
      return input('input') * input('factor');
    case 'MinBlock':
      return Math.min(input('left'), input('right'));
    case 'TrigonometryBlock':
      expect((block as TrigonometryBlock).operation).toBe(TrigonometryBlockOperations.Sin);
      return Math.sin(input('input'));
    case 'PosterizeBlock':
      return Math.floor(input('value') * input('steps')) / input('steps');
    case 'ClampBlock': {
      const c = block as ClampBlock;
      return Math.min(Math.max(input('value'), c.minimum), c.maximum);
    }
    default:
      throw new Error(`no ${block.getClassName()} in the torchlight`);
  }
}

/** A cave palette's plain toon, lit by map's light map, or by one dark texel for a cave with no torches. */
function caveToon(map: Parameters<typeof readLight>[0] | null): NodeMaterial {
  const torches = new Torchlight(scene, map && readLight(map));
  return new Palette(scene, torches).toon({ texture: null, color: Color3.White() });
}

/** What m's block name gives a texel of groups a and b at seconds: 'banded' its band, 0 the floor, ½, 1 the torches' full light; 'torchlight' the light before banding. */
function value(m: NodeMaterial, name: string, a: number, b: number, seconds: number): number {
  return run(m.getBlockByName(name)!.outputs[0]!, { a, b, seconds });
}

const band = (m: NodeMaterial, a: number, b: number, seconds: number) =>
  value(m, 'banded', a, b, seconds);

describe('a cave’s toon materials (underground-and-underwater.md section 7)', () => {
  it('take the light map for the sun: its texture over the cave, metre for metre, from its floor to the torches’ warm light', () => {
    const cave = caveToon(fixtureFile('hollow', 'light.bin'));
    expect(cave.getBlockByName('light')).toBeNull();
    const map = cave.getBlockByName('lightMap') as TextureBlock;
    expect(map.texture?.getSize()).toEqual({ width: 128, height: 128 });
    expect((cave.getBlockByName('lightSize') as InputBlock).value).toEqual(new Vector2(128, 128));
    expect((cave.getBlockByName('shade') as InputBlock).value).toEqual(FLOOR);
    expect((cave.getBlockByName('torch') as InputBlock).value).toEqual(TORCH);
    const land = new Palette(scene).toon({ texture: null, color: Color3.White() });
    expect(land.getBlockByName('light')).not.toBeNull();
    expect(land.getBlockByName('lightMap')).toBeNull();
  });

  it('keep the darkest spot readable: its floor is the land’s shade band, so the cave’s ground shows as in a shadow on the land, its walls darker and a torch’s pool still brighter', () => {
    const cave = caveToon(fixtureFile('hollow', 'light.bin'));
    const land = new Palette(scene).toon({ texture: null, color: Color3.White() });
    expect((cave.getBlockByName('shade') as InputBlock).value).toEqual(
      (land.getBlockByName('shade') as InputBlock).value,
    );
    const onScreen = (ground: number, light: Color3) => {
      const c = Color3.FromInts((ground >> 16) & 255, (ground >> 8) & 255, ground & 255)
        .toLinearSpace()
        .multiply(light)
        .toGammaSpace();
      return ((c.r + c.g + c.b) / 3) * 255;
    };
    const floor = onScreen(grounds.cave_floor, FLOOR);
    expect(floor).toBeGreaterThanOrEqual(40); // at 0.3 it was 23 of 255: near-black
    expect(onScreen(grounds.cave_rock, FLOOR)).toBeLessThan(floor);
    expect(onScreen(grounds.cave_floor, TORCH)).toBeGreaterThan(1.5 * floor);
  });

  it('draw a body under a flame in the full band and one no torch reaches at the floor, at every flicker', () => {
    const cave = caveToon(fixtureFile('hollow', 'light.bin'));
    for (let seconds = 0; seconds < 60; seconds += 0.25) {
      expect(band(cave, 1, 0, seconds)).toBe(1);
      expect(band(cave, 0, 1, seconds)).toBe(1);
      expect(band(cave, 0, 0, seconds)).toBe(0);
      expect(band(cave, 0.37, 0.37, seconds)).toBeGreaterThanOrEqual(band(cave, 0.2, 0, seconds));
    }
  });

  it('flicker each group by ±8% at its own rates, so neighbours along a wall flicker apart', () => {
    const cave = caveToon(fixtureFile('hollow', 'light.bin'));
    for (const group of [0, 1]) {
      expect(FLICKERS[group]!.reduce((sum, s) => sum + s.amp, 0)).toBeCloseTo(0.08);
    }
    const lit = (a: number, b: number, seconds: number) => value(cave, 'torchlight', a, b, seconds);
    const swings = [0, 1].map((g) => {
      const seen = Array.from({ length: 240 }, (_, i) =>
        lit(g === 0 ? 0.5 : 0, g === 1 ? 0.5 : 0, i / 4),
      );
      expect(Math.min(...seen)).toBeGreaterThanOrEqual(0.5 * 0.92 - 1e-9);
      expect(Math.max(...seen)).toBeLessThanOrEqual(0.5 * 1.08 + 1e-9);
      expect(Math.max(...seen) - Math.min(...seen)).toBeGreaterThan(0.5 * 0.1);
      return seen;
    });
    expect(swings[0]).not.toEqual(swings[1]);
    expect(lit(1, 1, 3)).toBe(1); // two torches' light held to full
  });

  it('flicker as the flames do: the same table, run in JS, gives the shader’s swing at every moment', () => {
    const cave = caveToon(fixtureFile('hollow', 'light.bin'));
    for (const group of [0, 1]) {
      for (let seconds = 0; seconds < 30; seconds += 0.37) {
        expect(value(cave, `flickered${group}1`, 0, 0, seconds)).toBeCloseTo(
          flicker(group, seconds),
          12,
        );
      }
    }
  });

  it('draw a cave with no torches at its floor everywhere', () => {
    const dark = caveToon(null);
    expect((dark.getBlockByName('lightMap') as TextureBlock).texture?.getSize()).toEqual({
      width: 1,
      height: 1,
    });
  });

  it('leave a warning and a glow unlit, as on the land', () => {
    const palette = new Palette(scene, new Torchlight(scene, null));
    expect(palette.decal(Color3.Red(), 0.5, 0).getBlockByName('lightMap')).toBeNull();
    expect(palette.glow(Color3.Red(), 0.5).getBlockByName('lightMap')).toBeNull();
  });
});
