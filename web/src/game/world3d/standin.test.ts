import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';

import type { Beats } from './beats';
import { RISE_MS, STILL } from './body';
import type { Lighting } from './body';
import { mobLook } from './looks';
import type { StandInLook } from './looks';
import { StandIn } from './standin';
import { linearOf, Palette } from './toon';

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;

beforeAll(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: new ShadowGenerator(256, sun) };
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

function lookOf(type: string): StandInLook {
  const l = mobLook(type);
  if (!('standIn' in l)) {
    throw new Error(`${type} has a model`);
  }
  return l.standIn;
}

function bounds(s: StandIn): { min: Vector3; max: Vector3 } {
  s.root.computeWorldMatrix(true);
  return s.root.getHierarchyBoundingVectors(true, (m) => !m.name.endsWith(' ink'));
}

/** The body inside the root: where it lunges and how far it has tipped over. */
function body(s: StandIn): { z: number; tilt: number } {
  const b = s.root.getChildTransformNodes(true)[0]!;
  return { z: b.position.z, tilt: b.rotation.z };
}

const BEATS: Beats = { start: 1000, hit: 1200, strikeEnd: 1250, readyAt: 1600, end: 1800 };

describe('StandIn', () => {
  it('takes its family’s shape from its id', () => {
    expect(
      ['wild_boar', 'cave_bat', 'mossback_snail', 'shade_lord'].map((t) => lookOf(t).shape),
    ).toEqual(['beast', 'flyer', 'shell', 'brute']);
  });

  it.each(mobs.filter((m) => 'standIn' in mobLook(m.id)).map((m) => m.id))(
    'draws %s standing on the ground, as tall as its cylinder',
    (type) => {
      const look = lookOf(type);
      const s = new StandIn(scene, look, lighting);
      s.update(0, 16, STILL);
      const { min, max } = bounds(s);
      expect(min.y).toBeGreaterThanOrEqual(-1e-6);
      expect(max.y).toBeGreaterThan(look.height * 0.8);
      expect(max.y).toBeLessThan(look.height * 1.1);
      s.dispose();
    },
  );

  it('wears its mob’s tint, and a boss is three times its kind’s size', () => {
    const boar = mobs.find((m) => m.id === 'wild_boar')!;
    const s = new StandIn(scene, lookOf('wild_boar'), lighting);
    const skin = s.root.getChildMeshes().find((m) => m.name === 'body')!.material as NodeMaterial;
    const color = skin.getBlockByName('color') as { value: { equals(c: unknown): boolean } } | null;
    expect(color?.value.equals(linearOf(boar.look.tint))).toBe(true);
    expect(lookOf('vampire_bat').height).toBeCloseTo(lookOf('cave_bat').height * 3);
    s.dispose();
  });

  it('lets go of all it made and none of the materials other bodies share', () => {
    const kept = new StandIn(scene, lookOf('wild_boar'), lighting);
    const counts = () => [scene.meshes, scene.transformNodes, scene.materials].map((a) => a.length);
    const before = counts();
    new StandIn(scene, lookOf('wild_boar'), lighting).dispose();
    expect(counts()).toEqual(before);
    expect(
      kept.root.getChildMeshes().every((m) => m.material && scene.materials.includes(m.material)),
    ).toBe(true);
    kept.dispose();
  });

  it('lunges toward its strike and draws back by the ready point', () => {
    const s = new StandIn(scene, lookOf('wild_boar'), lighting);
    s.act('attack', BEATS);
    s.update(BEATS.hit, 16, STILL);
    const out = body(s).z;
    expect(out).toBeGreaterThan(0);
    s.update((BEATS.hit + BEATS.readyAt) / 2, 16, STILL);
    expect(body(s).z).toBeLessThan(out);
    s.update(BEATS.readyAt, 16, STILL);
    expect(body(s).z).toBe(0);
    s.dispose();
  });

  it('holds its lunge through a hit-stop', () => {
    const s = new StandIn(scene, lookOf('wild_boar'), lighting);
    s.act('attack', BEATS);
    s.hold({ from: 1100, until: 1180, readyAt: 1600 });
    s.update(1100, 16, STILL);
    const held = body(s).z;
    s.update(1170, 16, STILL);
    expect(body(s).z).toBe(held);
    s.dispose();
  });

  it('tips over when knocked down or killed and stands back up as it rises', () => {
    const s = new StandIn(scene, lookOf('wild_boar'), lighting);
    s.pose('down', 0);
    s.update(500, 16, STILL);
    expect(body(s).tilt).toBeCloseTo(Math.PI / 2);
    s.pose('rising', 500);
    s.update(500 + RISE_MS / 2, 16, STILL);
    expect(body(s).tilt).toBeCloseTo(Math.PI / 4);
    s.update(500 + RISE_MS, 16, STILL);
    expect(body(s).tilt).toBe(0);
    s.pose('dead', 2000);
    s.update(3000, 16, STILL);
    expect(body(s).tilt).toBeCloseTo(Math.PI / 2);
    s.dispose();
  });
});
