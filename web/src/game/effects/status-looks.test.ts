import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { statuses } from '@/gamedata/statuses';
import { steepMap } from '@/testing/baked-map';
import { servePublic } from '@/testing/public-on-disk';

import { Palette } from '../world3d/toon';

import { drawnVersion } from './drawn';
import { Fx } from './fx';
import { effectIds, statusEffect } from './ids';
import { emptyManifest } from './manifest';
import type { Body } from './types';

let engine: NullEngine;
let scene: Scene;
let fx: Fx;
let now = 0;

const ids = statuses.map((s) => s.id);

/** What a scene holds that a look makes, past the shapes fx keeps for the map. */
const made = () => [
  scene.meshes.filter((m) => !m.name.startsWith('kept ')).length,
  scene.transformNodes.length,
  scene.particleSystems.length,
];

describe('status marks', () => {
  beforeEach(async () => {
    engine = new NullEngine();
    scene = new Scene(engine);
    servePublic();
    now = 1000;
    const map = await steepMap();
    fx = new Fx(
      scene,
      map,
      { manifest: emptyManifest, sound: () => undefined },
      () => now,
      new Palette(scene),
    );
  });

  afterEach(() => {
    fx.dispose();
    scene.dispose();
    engine.dispose();
  });

  it('names a mark for every status, each with a drawn version', () => {
    expect(ids.map(statusEffect).filter((id) => !effectIds.includes(id))).toEqual([]);
    expect(ids.map(statusEffect).filter((id) => !drawnVersion(id))).toEqual([]);
  });

  it.each(ids)(
    '%s marks its body until stopped: drawn about it as it moves, still there long after, and stop takes all it made away',
    (id) => {
      const body: { -readonly [K in keyof Body]: Body[K] } = {
        x: 10,
        y: 0,
        z: 10,
        radius: 0.5,
        height: 1,
      };
      const before = made();
      const meshes = new Set(scene.meshes);
      const stop = fx.play(statusEffect(id), {
        ...body,
        yaw: 0,
        size: { radius: 0.5, height: 1 },
        follow: () => body,
      });
      for (let t = 1000; t <= 11000; t += 50) {
        now = t;
        body.x = 10 + (t - 1000) / 2000; // 5 m east over 10 s
        fx.update(t);
      }
      expect(made().some((n, i) => n > before[i]!)).toBe(true);
      const at = new Vector3(body.x, body.y + body.height / 2, body.z);
      const far = (p: Vector3) => Math.hypot(p.x - at.x, p.z - at.z);
      for (const m of scene.meshes.filter((m) => !meshes.has(m) && m.isEnabled())) {
        m.computeWorldMatrix(true);
        m.refreshBoundingInfo({});
        expect(far(m.getBoundingInfo().boundingBox.centerWorld), m.name).toBeLessThan(1.5);
      }
      for (const ps of scene.particleSystems) {
        expect(far(ps.emitter as Vector3), 'its particles').toBeLessThan(1.5);
      }
      stop();
      stop();
      expect(made()).toEqual(before);
    },
  );
});
