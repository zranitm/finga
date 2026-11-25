import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
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

  /** The battlefield, any point named in moved standing there instead, then its marks. */
  async function built(moved: Record<string, number[]> = {}): Promise<WorldMap> {
    const raw = structuredClone(json('battlefield')) as {
      battle: { points: { id: string; at: number[] }[] };
    };
    for (const p of raw.battle.points) {
      p.at = moved[p.id] ?? p.at;
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
    marks!.show([], 1, 1);
    expect(walls()).toEqual([false, true]);
    marks!.show([], 2, 2);
    expect(walls()).toEqual([true, false]);
    marks!.show([], 2, 0); // in prep, behind the gates
    expect(walls()).toEqual([false, false]);
  });

  it('takes its meshes and materials with it', async () => {
    await built();
    const before = scene.materials.length;
    expect(scene.meshes.filter((m) => /^(point|base wall)/.test(m.name))).toHaveLength(8);
    marks!.dispose();
    marks = undefined;
    expect(scene.meshes.filter((m) => /^(point|base wall)/.test(m.name))).toEqual([]);
    expect(before - scene.materials.length).toBe(7); // three rings', three glows', the wall's
  });
});
