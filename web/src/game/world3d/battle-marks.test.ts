import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { height } from '@/sim/terrain';
import { mapJson as json, servePublic } from '@/testing/public-on-disk';

import { BattleMarks } from './battle-marks';
import { groundUnder, loadMap } from './map';
import type { WorldMap } from './map';
import { Palette } from './toon';

describe('a battle’s marks in the world', () => {
  let engine: NullEngine;
  let scene: Scene;
  let marks: BattleMarks | undefined;

  beforeEach(() => {
    engine = new NullEngine();
    scene = new Scene(engine);
  });

  afterEach(() => {
    marks?.dispose();
    marks = undefined;
    scene.dispose();
    engine.dispose();
    vi.unstubAllGlobals();
  });

  /** The battlefield, any point named in moved standing there instead and any in grown that wide, then its marks. */
  async function built(
    moved: Record<string, number[]> = {},
    grown: Record<string, number> = {},
  ): Promise<WorldMap> {
    const raw = structuredClone(json('battlefield')) as {
      battle: { points: { id: string; at: number[]; radius: number }[] };
    };
    for (const p of raw.battle.points) {
      p.at = moved[p.id] ?? p.at;
      p.radius = grown[p.id] ?? p.radius;
    }
    servePublic({ any: raw });
    const map = await loadMap('any');
    marks = new BattleMarks(scene, map, map.battle!.points, map.battle!.bases, new Palette(scene));
    return map;
  }

  it('lays each point’s ring and glow 4 cm over the ground, up the mounds by the middle point too, the ring 25 cm wide at the point’s edge', async () => {
    const map = await built();
    for (const p of map.battle!.points) {
      for (const [kind, inner, outer] of [
        ['ring', p.radius - 0.25, p.radius],
        ['glow', 0, p.radius - 0.25],
      ] as const) {
        const m = scene.getMeshByName(`point ${kind} ${p.id}`) as Mesh;
        const at = m.getVerticesData(VertexBuffer.PositionKind)!;
        const ground: number[] = [];
        const reach: number[] = [];
        let off = 0;
        for (let i = 0; i < at.length; i += 3) {
          const [x, z] = [m.position.x + at[i]!, m.position.z + at[i + 2]!];
          const under = groundUnder(map, x, z);
          ground.push(under);
          reach.push(Math.hypot(at[i]!, at[i + 2]!));
          off = Math.max(off, Math.abs(m.position.y + at[i + 1]! - (under + 0.04)));
        }
        expect([p.id, kind, off < 1e-9]).toEqual([p.id, kind, true]);
        expect(Math.min(...reach)).toBeCloseTo(inner, 9);
        expect(Math.max(...reach)).toBeCloseTo(outer, 9);
        if (p.id === 'middle' && kind === 'ring') {
          expect(Math.max(...ground) - Math.min(...ground)).toBeGreaterThan(0.2); // the mounds at x 60 and 80 reach under its edge
        }
      }
    }
  });

  it('keeps a wide point’s ring and a base’s wall no more than half a metre straight between vertices round them', async () => {
    await built({}, { middle: 30 });
    const gaps = (name: string, ring: (x: number, z: number) => boolean) => {
      const m = scene.getMeshByName(name) as Mesh;
      const at = m.getVerticesData(VertexBuffer.PositionKind)!;
      const round: [number, number][] = [];
      for (let i = 0; i < at.length; i += 3) {
        if (ring(at[i]!, at[i + 2]!)) {
          round.push([at[i]!, at[i + 2]!]);
        }
      }
      round.sort((a, b) => Math.atan2(a[0], a[1]) - Math.atan2(b[0], b[1]));
      return round.map(([x, z], k) => {
        const [nx, nz] = round[(k + 1) % round.length]!;
        return Math.hypot(nx - x, nz - z);
      });
    };
    const ring = gaps('point ring middle', (x, z) => Math.abs(Math.hypot(x, z) - 30) < 1e-6);
    expect(ring.length).toBeGreaterThan(64);
    expect(Math.max(...ring)).toBeLessThanOrEqual(0.5 + 1e-6);
    const wall = gaps('base wall 1', () => true).filter((d) => d > 1e-6); // its top and bottom share each angle
    expect(Math.max(...wall)).toBeLessThanOrEqual(0.5 + 1e-6);
  });

  it('lays a point whose middle stands on a mound on the ground round it, not at the middle’s height', async () => {
    const map = await built({ middle: [60, 45] }); // the mound west of it, 1 m high
    const m = scene.getMeshByName('point ring middle') as Mesh;
    expect(m.position.y).toBeGreaterThan(0.5);
    const at = m.getVerticesData(VertexBuffer.PositionKind)!;
    for (let i = 0; i < at.length; i += 3) {
      const under = groundUnder(map, m.position.x + at[i]!, m.position.z + at[i + 2]!);
      expect(m.position.y + at[i + 1]!).toBeCloseTo(under + 0.04, 9);
    }
  });

  it('walls each base, hidden till shown, from half a metre under its lowest ground round it to 3 m over its highest', async () => {
    const map = await built();
    for (const [i, b] of map.battle!.bases.entries()) {
      const wall = scene.getMeshByName(`base wall ${i + 1}`)!;
      wall.computeWorldMatrix(true);
      wall.refreshBoundingInfo({});
      const box = wall.getBoundingInfo().boundingBox;
      const round = Array.from({ length: 64 }, (_, k) => {
        const a = (k / 64) * 2 * Math.PI;
        return height(
          map.geometry.terrain,
          b.x + Math.sin(a) * b.radius,
          b.z + Math.cos(a) * b.radius,
        );
      });
      expect(wall.isEnabled()).toBe(false);
      expect([wall.position.x, wall.position.z]).toEqual([b.x, b.z]);
      expect(box.extendSize.x).toBeCloseTo(b.radius, 9);
      expect(box.minimumWorld.y).toBeCloseTo(Math.min(...round) - 0.5, 9);
      expect(box.maximumWorld.y).toBeCloseTo(Math.max(...round) + 3, 9);
    }
  });

  it('shows the other side’s base’s wall alone while the player walks their side’s ground, though no point changes hands', async () => {
    await built();
    const walls = () => [1, 2].map((i) => scene.getMeshByName(`base wall ${i}`)!.isEnabled());
    marks!.show([], 1, 1, 0);
    expect(walls()).toEqual([false, true]);
    marks!.show([], 2, 2, 0);
    expect(walls()).toEqual([true, false]);
    marks!.show([], 2, 0, 0); // in prep, behind the gates
    expect(walls()).toEqual([false, false]);
  });

  it('takes its meshes and materials with it', async () => {
    await built();
    const before = scene.materials.length;
    expect(scene.meshes.filter((m) => /^(point|base wall)/.test(m.name))).toHaveLength(8);
    marks!.dispose();
    marks = undefined;
    expect(scene.meshes.filter((m) => /^(point|base wall)/.test(m.name))).toEqual([]);
    expect(before - scene.materials.length).toBe(9); // four rings', four glows' (a contested one each), the wall's
  });

  /** The colour each point's ring and glow are drawn in, by point. */
  const drawn = (kind: 'ring' | 'glow') =>
    Object.fromEntries(
      ['west', 'middle', 'east'].map((id) => {
        const m = scene.getMeshByName(`point ${kind} ${id}`)!.material as NodeMaterial;
        return [id, (m.getBlockByName('color') as InputBlock).value as Color3];
      }),
    );
  const hex = (c: Color3) => c.toHexString().toLowerCase();

  it('draws each point in its holder’s colour from the player’s side, grey for no one’s', async () => {
    await built();
    const held = [
      { id: 'west', holder: 1, progress: -1, contested: false },
      { id: 'east', holder: 2, progress: 1, contested: false },
    ];
    marks!.show(held, 1, 1, 0);
    expect(Object.values(drawn('ring')).map(hex)).toEqual(['#7fe08a', '#d8d8d8', '#ff6b62']);
    marks!.show(held, 2, 2, 0);
    expect(Object.values(drawn('ring')).map(hex)).toEqual(['#ff6b62', '#d8d8d8', '#7fe08a']);
  });

  it('pulses a contested point, ring and glow, from the player’s side’s colour to the other’s and back each second, then shows its holder’s again', async () => {
    await built();
    const fight = (contested: boolean) => [{ id: 'middle', holder: 2, progress: 0.4, contested }];
    const at = (now: number) => {
      marks!.show(fight(true), 1, 1, now);
      const glow = drawn('glow').middle!.scale(1 / 0.45); // the glow's colour is scaled by its alpha and glow
      return [hex(drawn('ring').middle!), hex(glow)];
    };
    expect(at(0)).toEqual(['#7fe08a', '#7fe08a']);
    expect(at(500)).toEqual(['#ff6b62', '#ff6b62']);
    at(250);
    expect(hex(drawn('ring').middle!)).toBe(
      hex(Color3.Lerp(Color3.FromHexString('#7fe08a'), Color3.FromHexString('#ff6b62'), 0.5)),
    );
    expect(at(1000)).toEqual(['#7fe08a', '#7fe08a']);
    expect(hex(drawn('ring').west!)).toBe('#d8d8d8'); // the others as they were
    marks!.show(fight(false), 1, 1, 1250);
    expect(hex(drawn('ring').middle!)).toBe('#ff6b62'); // the other side's, as it holds it
  });
});
