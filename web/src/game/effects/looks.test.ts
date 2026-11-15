import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';

import type { Beats } from '../world3d/beats';
import { loadMap } from '../world3d/map';

import { drawnVersion } from './drawn';
import { SHOT_M_PER_MS } from './flight';
import { Fx } from './fx';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { emptyManifest } from './manifest';
import type { Body, PlayOptions } from './types';

const BEATS: Beats = { start: 1000, hit: 1000, strikeEnd: 1150, readyAt: 1450, end: 1500 };
const CASTER: Body = { x: 10, y: 0, z: 10, radius: 0.375, height: 1.75 };
const FOES: Body[] = [
  { x: 13, y: 0, z: 10.5, radius: 0.5, height: 0.875 },
  { x: 14, y: 0, z: 9, radius: 0.5, height: 0.875 },
  { x: 12, y: 0, z: 11.5, radius: 0.5, height: 0.875 },
];
const EAST = Math.PI / 2;

let engine: NullEngine;
let scene: Scene;
let fx: Fx;
let now = 0;

const looks = effectIds.filter((id) => id.startsWith('attack.') || id.startsWith('skill.'));

/** What a scene holds that a look makes. */
const made = () => [scene.meshes.length, scene.transformNodes.length, scene.particleSystems.length];

const at = (o: Partial<PlayOptions> = {}): PlayOptions => ({
  ...CASTER,
  yaw: EAST,
  beats: BEATS,
  targets: () => FOES,
  follow: () => CASTER,
  ...o,
});

/** Steps fx's plays to t. */
function to(t: number): void {
  now = t;
  fx.update(t);
}

const named = (name: string) => scene.meshes.filter((m) => m.name === name && m.isEnabled());

describe('looks', () => {
  beforeEach(async () => {
    engine = new NullEngine();
    scene = new Scene(engine);
    servePublic();
    now = 1000;
    fx = new Fx(
      scene,
      await loadMap('forest'),
      { manifest: emptyManifest, sound: () => undefined },
      () => now,
    );
  });

  afterEach(() => {
    fx.dispose();
    scene.dispose();
    engine.dispose();
  });

  it('names every attack and active skill, each with a drawn version to fall back to', () => {
    expect(effectIds).toContain('attack.warrior');
    expect(effectIds).toContain('skill.spark');
    expect(effectIds).not.toContain('skill.second_wind'); // passive: nothing to see
    expect(effectIds.filter((id) => !id.startsWith('mob.') && !drawnVersion(id))).toEqual([]);
  });

  it.each(looks)('%s draws through its strike, and stop takes all it made away', (id: EffectId) => {
    const before = made();
    const stop = fx.play(id, at());
    to(BEATS.hit + 60);
    expect(made().some((n, i) => n > before[i]!)).toBe(true);
    stop();
    stop();
    expect(made()).toEqual(before);
  });

  it.each(looks)(
    '%s ends on its own once its shot has flown and its beats are done',
    (id: EffectId) => {
      const before = made();
      fx.play(id, at());
      for (let t = BEATS.hit; t <= BEATS.end + 600; t += 16) {
        to(t);
      }
      expect(made()).toEqual(before);
    },
  );

  it('draws a swing out to its arc’s radius', () => {
    fx.play('attack.warrior', at());
    to(BEATS.strikeEnd);
    const band = named('band')[0] as Mesh;
    const p = band.getVerticesData(VertexBuffer.PositionKind)!;
    let far = 0;
    for (let i = 0; i < p.length; i += 3) {
      far = Math.max(far, Math.hypot(p[i]!, p[i + 2]!));
    }
    expect(far).toBeCloseTo(0.9 * 2.375, 3);
  });

  it('flies a shot from the caster’s chest along the aim at a shot’s pace', () => {
    fx.play('attack.bowman', at());
    to(BEATS.hit + 100);
    const arrow = scene.transformNodes.find((n) => n.name === 'arrow')!;
    expect(arrow.position.x).toBeCloseTo(CASTER.x + 0.3 + 100 * SHOT_M_PER_MS, 3);
    expect(arrow.position.z).toBeCloseTo(CASTER.z, 3);
  });

  it('draws each skill’s tiers differently: Strike cracks the ground, Volley throws three stones, Arc leaps between the foes', () => {
    const lines = () => named('line').length;
    const stopStrike = fx.play('skill.strike', at({ tier: 0 }));
    to(BEATS.strikeEnd);
    const plain = lines();
    stopStrike();
    fx.play('skill.strike', at({ tier: 2 }));
    to(BEATS.strikeEnd);
    expect(lines() - plain).toBe(1);
    fx.dispose();

    fx.play('skill.throw', at({ tier: 0 }));
    fx.play('skill.throw', at({ tier: 2 }));
    to(BEATS.hit + 30);
    expect(named('stone')).toHaveLength(4);
    fx.dispose();

    fx.play('skill.spark', at({ tier: 0 }));
    to(BEATS.hit + 30);
    const bolt = lines();
    fx.dispose();
    fx.play('skill.spark', at({ tier: 2 }));
    to(BEATS.hit + 30);
    expect([bolt, lines()]).toEqual([2, 6]); // a leg to each of three foes, each a violet line round a white one
  });
});
