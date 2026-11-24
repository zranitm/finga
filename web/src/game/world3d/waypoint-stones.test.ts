import { readFileSync } from 'node:fs';
import path from 'node:path';

import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { waypoints } from '@/state/waypoints';
import { tinyFile } from '@/testing/region-fixture';
import { resetStores } from '@/testing/reset-stores';

import { Models } from './models';
import { fade, STEPS } from './place';
import { Palette } from './toon';
import { WaypointStones } from './waypoint-stones';

const MODELS = path.resolve(process.cwd(), 'public/models');

const WEST = { id: 'west', name: 'West Field', x: 60, z: 85, y: 1.5, main: false };
const TOWN = { id: 'town', name: 'Test Town', x: 124, z: 85, y: 2, main: true };

describe('a region’s waypoint stones', () => {
  let engine: NullEngine;
  let scene: Scene;
  let palette: Palette;
  let clock: number;
  let stones: WaypointStones | undefined;
  let heightAt: (x: number, z: number) => number;

  const mesh = (name: string) => scene.getMeshByName(name) as Mesh | null;
  /** Where copy draws its stone, at each spot it stands. */
  const spots = (copy: string) => {
    const m = mesh(`${copy} Crystal`)!;
    if (!m.isEnabled()) {
      return [];
    }
    const at = m.thinInstanceGetWorldMatrices();
    return at.map((w) => {
      const t = Vector3.TransformCoordinates(Vector3.Zero(), w);
      return [t.x, t.y, t.z].map((v) => Math.round(v * 100) / 100);
    });
  };
  const frame = (t: number) => {
    clock = t;
    scene.onBeforeRenderObservable.notifyObservers(scene);
  };

  async function built(): Promise<void> {
    const models = new Models(scene, (f) => new Uint8Array(readFileSync(path.join(MODELS, f))));
    stones = new WaypointStones(
      scene,
      [WEST, TOWN],
      palette,
      models,
      () => clock,
      (x, z) => heightAt(x, z),
    );
    await vi.waitFor(() => {
      expect(mesh('waypoint stone found Crystal')).not.toBeNull();
    });
  }

  beforeEach(() => {
    resetStores();
    engine = new NullEngine();
    engine.getCaps().instancedArrays = true;
    scene = new Scene(engine);
    palette = new Palette(scene);
    clock = 1000;
    heightAt = () => 0;
  });

  afterEach(() => {
    stones?.dispose();
    stones = undefined;
    scene.dispose();
    engine.dispose();
  });

  it('stands each stone on the ground at its spot, dim with a small faint halo till found and lit with a wider one after, as the join’s list has it', async () => {
    waypoints.getState().applyWelcome(['town']);
    await built();
    expect(spots('waypoint stone')).toEqual([[60, 1.5, 85]]);
    expect(spots('waypoint stone found')).toEqual([[124, 2, 85]]);
    const dim = mesh('waypoint stone Crystal')!.material;
    const lit = mesh('waypoint stone found Crystal')!.material;
    expect(dim).not.toBe(lit);
    expect(mesh('waypoint stone Rune')!.material).not.toBe(
      mesh('waypoint stone found Rune')!.material,
    );
    const west = mesh('waypoint west halo')!;
    const town = mesh('waypoint town halo')!;
    expect(town.scaling.x).toBeGreaterThan(west.scaling.x);
    expect(town.material!.alpha).toBeGreaterThan(west.material!.alpha);
    expect(west.position.asArray()).toEqual([60, 1.5 + 3.24, 85]); // round the crystal
    expect(mesh('waypoint ring')!.isEnabled()).toBe(false); // the join's flares none
  });

  // From the look's pick: an added halo read mint on grass; drawn over it, its solid part must read the map's cyan.
  it('draws each halo over the ground, not added to it, its solid part reading cyan on a region’s grass', async () => {
    await built();
    const grass = (
      JSON.parse(new TextDecoder().decode(tinyFile('region.json'))) as {
        palette: { name: string; rgb: string }[];
      }
    ).palette.find((c) => c.name === 'grass')!.rgb;
    const ground = Color3.FromHexString(grass);
    for (const id of ['west', 'town']) {
      const m = mesh(`waypoint ${id} halo`)!.material as StandardMaterial;
      expect(m.alphaMode).toBe(Constants.ALPHA_COMBINE);
      const seen = m.emissiveColor.scale(m.alpha).add(ground.scale(1 - m.alpha));
      expect(seen.toHSV().r).toBeGreaterThanOrEqual(178); // cyan; grass is 95, mint about 150
      expect(seen.toHSV().r).toBeLessThanOrEqual(195);
    }
  });

  it('draws no copy where no stone is in its state, rather than one at the region’s corner, and lights a join’s list unflared', async () => {
    await built();
    expect(mesh('waypoint stone found Crystal')!.isEnabled()).toBe(false);
    expect(mesh('waypoint stone found Crystal ink')!.isEnabled()).toBe(false);
    waypoints.getState().applyWelcome(['west', 'town']);
    expect(mesh('waypoint ring')!.isEnabled()).toBe(false); // a join after the stones stand lights them without a flare
    expect(mesh('waypoint stone Crystal')!.isEnabled()).toBe(false);
    expect(spots('waypoint stone found')).toEqual([
      [60, 1.5, 85],
      [124, 2, 85],
    ]);
  });

  it('a find lights its stone at once and flares it: a beam gone by 0.6 s, a ring spreading 7 m across, the halo swelling and settling, then nothing more', async () => {
    await built();
    waypoints.getState().find('west');
    expect(spots('waypoint stone found')).toEqual([[60, 1.5, 85]]);
    expect(spots('waypoint stone')).toEqual([[124, 2, 85]]);
    const beam = mesh('waypoint beam')!;
    const ring = mesh('waypoint ring')!;
    const halo = mesh('waypoint west halo')!;
    expect([beam.isEnabled(), ring.isEnabled()]).toEqual([true, true]);
    expect(ring.position.asArray()).toEqual([60, 1.5 + 0.08, 85]);
    const found = halo.scaling.x;
    expect(found).toBeGreaterThan(mesh('waypoint town halo')!.scaling.x); // a found one's from the start
    frame(1400);
    expect(halo.scaling.x).toBeCloseTo(1.5 * found); // at its widest, half as wide again
    frame(1600);
    expect([beam.isEnabled(), ring.isEnabled()]).toEqual([false, true]);
    frame(1799);
    expect(ring.scaling.x).toBeCloseTo(7, 1);
    frame(1800);
    expect([beam.isEnabled(), ring.isEnabled()]).toEqual([false, false]);
    expect(halo.scaling.x).toBe(found);
  });

  // From the stones' live pass: arrivals were north of the stone, which stood between the camera and the player. A stone still fades for the player walking behind it, as a tall prop does.
  it('fades a stone out of the camera’s way to the player, halo and all, gone at the lens, and back whole after', async () => {
    await built();
    const instances = () => spots('waypoint stone');
    const copy = (lit: string) =>
      scene.meshes.filter(
        (m) => m.name.endsWith(` faded ${WEST.id}`) && m.name.startsWith(`waypoint stone ${lit}`),
      );
    const westHalo = mesh('waypoint west halo')!;
    const behind = new Vector3(60, 2.5, 90); // the player's head north of the west stone
    fade(new Vector3(60, 12, 72), behind, stones!.looks, 1000); // the camera south of it
    expect(instances()).toEqual([[124, 2, 85]]); // the west stone left the instances
    const faded = copy('').filter((m) => !m.name.startsWith('waypoint stone found'));
    expect(faded.length).toBeGreaterThan(0);
    for (const m of faded) {
      const alone = !m.hasThinInstances; // drawn here alone, not at every stone
      expect([m.name, m.isEnabled(), m.material!.name.endsWith(` ${STEPS[2]}`), alone]).toEqual([
        m.name,
        true,
        true,
        true,
      ]); // its toon at the faded step's alpha
    }
    expect(westHalo.visibility).toBe(STEPS[2]);
    waypoints.getState().find('west');
    expect(faded.every((m) => !m.isEnabled())).toBe(true); // found while faded: its found copy shows instead
    expect(copy('found').every((m) => m.isEnabled())).toBe(true);
    fade(new Vector3(60, 4, 86), behind, stones!.looks, 1000); // the lens at the stone
    expect([...copy(''), ...copy('found')].some((m) => m.isEnabled())).toBe(false);
    expect(westHalo.visibility).toBe(0);
    fade(new Vector3(80, 12, 72), new Vector3(80, 2.5, 90), stones!.looks, 1000); // off to the east
    expect(spots('waypoint stone found')).toEqual([[60, 1.5, 85]]);
    expect(westHalo.visibility).toBe(1);
  });

  // From the stones' live pass: on East Woods' slope the ground covered up to 98° of the flat ring by 8 cm.
  it('lifts the flare’s ring over the highest ground under it, on a slope too', async () => {
    heightAt = (x) => 1.5 + 0.1 * (x - WEST.x); // rising east
    await built();
    waypoints.getState().find('west');
    const ring = mesh('waypoint ring')!;
    frame(1799); // at its widest, 7 m across
    expect(ring.position.y).toBeCloseTo(1.5 + 0.1 * 3.5 + 0.08, 2);
    heightAt = () => TOWN.y;
    waypoints.getState().find('town');
    expect(ring.position.y).toBeCloseTo(TOWN.y + 0.08, 5); // on flat ground, just over it
  });

  it('a find of another region’s waypoint flares nothing here', async () => {
    await built();
    waypoints.getState().find('elsewhere');
    expect(mesh('waypoint ring')!.isEnabled()).toBe(false);
    expect(spots('waypoint stone found')).toEqual([]);
  });

  it('lets every mesh go with the place, and stops hearing finds and frames', async () => {
    const subscribe = waypoints.subscribe;
    const offs: (() => void)[] = [];
    vi.spyOn(waypoints, 'subscribe').mockImplementation((l) => {
      const off = vi.fn(subscribe(l));
      offs.push(off);
      return off;
    });
    const frames = scene.onBeforeRenderObservable.observers.length;
    await built();
    stones!.dispose();
    stones = undefined;
    expect(scene.meshes.filter((m) => m.name.startsWith('waypoint'))).toEqual([]);
    expect(offs).toHaveLength(1);
    expect(offs[0]).toHaveBeenCalledOnce();
    await new Promise((r) => setTimeout(r, 0)); // Babylon lets an observer go on a timer
    expect(scene.onBeforeRenderObservable.observers).toHaveLength(frames);
    vi.restoreAllMocks();
  });
});
