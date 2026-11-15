import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';

import type { Beats } from '../world3d/beats';
import { loadMap } from '../world3d/map';

import { drawnVersion } from './drawn';
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
    expect(effectIds.filter((id) => !drawnVersion(id))).toEqual([]);
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

  it('leaves the particles’ shared texture to the next look when one ends or is stopped', () => {
    fx.play('skill.fire_arrow', at());
    for (let t = BEATS.hit; t <= BEATS.end + 600; t += 16) {
      to(t);
    }
    const fresh = () =>
      at({
        beats: { start: now, hit: now, strikeEnd: now + 150, readyAt: now + 450, end: now + 500 },
      });
    fx.play('skill.heal', fresh())();
    fx.play('skill.heal', fresh());
    to(now + 50);
    expect(scene.particleSystems.length).toBeGreaterThan(0);
    expect(scene.particleSystems.every((p) => p.particleTexture?.getInternalTexture())).toBe(true);
  });

  describe('a mob attack', () => {
    const SLIME: Body = { x: 20, y: 0, z: 20, radius: 0.5, height: 0.875 };
    const WINDUP: Beats = { start: 1000, hit: 1600, strikeEnd: 2000, readyAt: 2250, end: 2250 };
    const mobAt = (o: Partial<PlayOptions> = {}): PlayOptions => ({
      ...SLIME,
      yaw: EAST,
      size: SLIME,
      reach: 0.75,
      to: { x: 22, y: 0, z: 20 },
      width: 1,
      beats: WINDUP,
      follow: () => SLIME,
      shots: () => [],
      ...o,
    });
    /** How far the patches' corners reach from (x, z): the area's, then what has filled of it. */
    const reaches = (x: number, z: number) =>
      named('patch').map((m) => {
        const p = m.getVerticesData(VertexBuffer.PositionKind)!;
        let far = 0;
        for (let i = 0; i < p.length; i += 3) {
          far = Math.max(far, Math.hypot(p[i]! - x, p[i + 2]! - z));
        }
        return far;
      });
    const mobLooks = effectIds.filter((id) => id.startsWith('mob.'));

    it.each(mobLooks)(
      '%s lays its area red on the ground through the windup, and stop takes all it made away',
      (id) => {
        const before = made();
        const stop = fx.play(id, mobAt());
        to(WINDUP.start + 300);
        expect(named('patch')).toHaveLength(2);
        expect(named('patch').every((m) => m.visibility > 0)).toBe(true);
        to(WINDUP.hit + 10);
        expect(named('patch').every((m) => m.visibility === 0)).toBe(true); // gone as it acts
        stop();
        expect(made()).toEqual(before);
      },
    );

    it('a bite’s area reaches its radius and reach ahead, filling out from the mob over its windup', () => {
      fx.play('mob.bite', mobAt());
      to(WINDUP.start + 300);
      const [zone, fill] = reaches(SLIME.x, SLIME.z);
      expect(zone).toBeCloseTo(0.5 + 0.75, 3);
      expect(fill).toBeCloseTo((0.5 + 0.75) / 2, 3);
    });

    it('a hop’s area is round its landing', () => {
      fx.play('mob.hop', mobAt({ reach: 0.375 }));
      to(WINDUP.start + 300);
      expect(reaches(22, 20)[0]).toBeCloseTo(0.5 + 0.375, 3);
    });

    it('a spit’s lane runs as far as its ball flies, its tip at the ball’s front', () => {
      fx.play('mob.spit', mobAt({ reach: 2, width: 0.4 }));
      to(WINDUP.start + 300);
      expect(reaches(SLIME.x, SLIME.z)[0]).toBeCloseTo(Math.hypot(2 + 0.2, 0.2), 3);
    });

    it('a spit’s balls show while they fly, in the mob’s colour', () => {
      const balls = [{ x: 21, y: 0.4, z: 20, size: 0.375 }];
      fx.play('mob.spit', mobAt({ color: 0xaa3366, shots: () => balls }));
      to(WINDUP.hit + 100);
      const [b] = named('ball');
      expect([b?.position.x, b?.scaling.x]).toEqual([21, 0.375]);
      balls.length = 0;
      to(WINDUP.hit + 150);
      expect(named('ball')).toHaveLength(0);
    });
  });

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
    expect(arrow.position.x).toBeCloseTo(CASTER.x + 0.3 + 4.375, 3); // 2D's 1.4 px/ms, 100 ms on
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
