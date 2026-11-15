import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';
import { ticksIn } from '@/lib/fixed-step';
import { actionPoints, readyPoint } from '@/progression/actions';

import { BLEND_MS } from './blend';
import { classLook, forestMobs } from './looks';
import { Stage } from './stage';
import { linearOf } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');
const TICK_MS = 50;
const FRAME_MS = 16;
const ATTACK = '1H_Melee_Attack_Slice_Diagonal';
const SKILL = '1H_Melee_Attack_Chop';

let t = 0;
let stage: Stage;
let engine: NullEngine;

async function open(cls: string): Promise<Stage> {
  return Stage.open(
    engine,
    { cls, tickMs: TICK_MS },
    (file) => new Uint8Array(readFileSync(path.join(MODELS, file))),
    () => t,
  );
}

function frames(ms: number): void {
  for (let left = ms; left > 0; left -= FRAME_MS) {
    t += FRAME_MS;
    vi.advanceTimersByTime(FRAME_MS);
    stage.step(FRAME_MS);
  }
}

function weight(name: string): number {
  const g = stage.scene.getAnimationGroupByName(name);
  if (!g) {
    throw new Error(`no group ${name}`);
  }
  return g.weight;
}

function heightOf(root: TransformNode): number {
  const { min, max } = root.getHierarchyBoundingVectors(true, (m) => m.isEnabled());
  return max.y - min.y;
}

beforeAll(async () => {
  engine = new NullEngine();
  stage = await open('beginner');
});

afterAll(() => {
  stage.dispose();
  engine.dispose();
});

describe('the Beginner', () => {
  it('is the Barbarian without hat, cape or shield, 1.75 m tall', () => {
    const root = stage.scene.getTransformNodeByName(classLook('beginner').file);
    expect(root).not.toBeNull();
    expect(heightOf(root!)).toBeCloseTo(1.75, 2);
    for (const gear of ['Barbarian_Hat', 'Barbarian_Cape', 'Barbarian_Round_Shield']) {
      expect(stage.scene.getNodeByName(gear)?.isEnabled()).toBe(false);
    }
    expect(stage.scene.getMeshByName('1H_Axe')?.isEnabled()).toBe(true);
  });

  it('draws every mesh in a toon material, inked', () => {
    const root = stage.scene.getTransformNodeByName(classLook('beginner').file)!;
    const meshes = root
      .getChildMeshes(false)
      .filter((m) => m.isEnabled() && m.getTotalVertices() > 0 && !m.name.endsWith(' ink'));
    expect(meshes.length).toBeGreaterThan(0);
    for (const m of meshes) {
      expect(m.material?.name).toMatch(/^toon /);
      const ink = m.getChildMeshes(true).find((c) => c.name === `${m.name} ink`);
      expect([m.name, ink?.material?.name, ink?.skeleton === m.skeleton]).toEqual([
        m.name,
        'ink',
        true,
      ]);
    }
  });
});

describe('the forest mobs', () => {
  it('stand at their heights', () => {
    for (const look of forestMobs) {
      const roots = stage.scene.transformNodes.filter((n) => n.name === look.file);
      const heights = roots.map(heightOf);
      expect(heights.some((h) => Math.abs(h - look.height) < 0.01)).toBe(true);
    }
  });

  it('stand upright, facing the camera to the south', () => {
    const root = stage.scene.transformNodes.find((n) => n.name === 'quaternius/Mushnub.glb')!;
    const centre = (mesh: string) => {
      const { min, max } = root.getHierarchyBoundingVectors(true, (m) => m.name === mesh);
      return min.add(max).scale(0.5);
    };
    const cap = centre('Mushnub_Blob_primitive0');
    const stem = centre('Mushnub_Blob_primitive1');
    const eyes = centre('Mushnub_Blob_primitive2');
    expect(cap.y).toBeGreaterThan(stem.y);
    expect(eyes.z).toBeLessThan(stem.z);
  });

  it('draw the orange mushroom’s cap orange', () => {
    const orange = linearOf(0xe08a3c);
    const caps = stage.scene.meshes
      .map((m) => m.material as NodeMaterial | null)
      .filter((m) => m?.getClassName() === 'NodeMaterial')
      .map((m) => m!.getBlockByName('color') as { value: { equals(c: unknown): boolean } } | null);
    expect(caps.some((c) => c?.value.equals(orange))).toBe(true);
  });
});

describe('actions', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    stage.releaseAll();
    frames(3000); // whatever ran before is over and faded out
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('play the attack on the whole body while standing', () => {
    stage.press('attack', false);
    frames(BLEND_MS + FRAME_MS);
    expect(weight(ATTACK)).toBe(1);
    expect(weight(`${ATTACK}/upper`)).toBe(1);
    expect(weight('Idle')).toBe(0);
  });

  it('pose the attack’s strike on its hit and its end on its ready point', () => {
    const attack = classes.find((c) => c.id === 'beginner')!.attack;
    const ticks = ticksIn(attack.length_ms, TICK_MS);
    const readyMs = readyPoint(ticks, attack.hit, attack.ready) * TICK_MS;
    const g = stage.scene.getAnimationGroupByName(ATTACK)!;
    const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
    const start = t;
    stage.press('attack', false);
    frames(FRAME_MS); // the Beginner's hit lands as it starts
    expect(share()).toBeGreaterThanOrEqual(classLook('beginner').clips.attack!.strike);
    expect(share()).toBeLessThan(classLook('beginner').clips.attack!.strike + 0.03);
    frames(readyMs - FRAME_MS - (t - start) - FRAME_MS);
    expect(share()).toBeGreaterThan(0.97);
  });

  it('play the attack on the upper body alone while walking, the legs still walking', () => {
    stage.press('right', false);
    frames(200);
    stage.press('attack', false);
    frames(BLEND_MS + FRAME_MS);
    expect(weight(`${ATTACK}/upper`)).toBe(1);
    expect(weight(ATTACK)).toBe(0);
    expect(weight('Walking_A')).toBe(1);
    expect(weight('Walking_A/upper')).toBe(0);
  });

  it('let a skill cut the attack at its cancel point, the attack gone within 80 ms', () => {
    const attack = classes.find((c) => c.id === 'beginner')!.attack;
    const ticks = ticksIn(attack.length_ms, TICK_MS);
    const cancelMs = actionPoints(ticks, attack.hit, attack.cancel).cancel * TICK_MS;
    const start = t;
    stage.press('attack', false);
    stage.onAction('skill1');
    while (weight(SKILL) === 0 && t - start < 2000) {
      frames(FRAME_MS);
    }
    expect(t - start).toBeGreaterThanOrEqual(cancelMs);
    expect(t - start).toBeLessThanOrEqual(cancelMs + 2 * FRAME_MS);
    frames(BLEND_MS);
    expect(weight(ATTACK)).toBe(0);
    expect(weight(SKILL)).toBe(1);
  });

  it('mask the upper body from the spine up', () => {
    const upper = stage.scene.getAnimationGroupByName(`${ATTACK}/upper`)!.mask!;
    const lower = stage.scene.getAnimationGroupByName(ATTACK)!.mask!;
    for (const joint of ['spine', 'chest', 'head', 'upperarm.r', 'handslot.r']) {
      expect([joint, upper.retainsTarget(joint), lower.retainsTarget(joint)]).toEqual([
        joint,
        true,
        false,
      ]);
    }
    for (const joint of ['hips', 'upperleg.l', 'lowerleg.r']) {
      expect([joint, upper.retainsTarget(joint), lower.retainsTarget(joint)]).toEqual([
        joint,
        false,
        true,
      ]);
    }
  });
});

describe('the sim', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    stage.releaseAll();
    vi.useRealTimers();
  });

  it('stops the player walking east at the crate in its way', () => {
    const root = stage.scene.getTransformNodeByName(classLook('beginner').file)!;
    stage.press('right', false);
    frames(2000);
    expect(root.position.x).toBeCloseTo(34 - 0.5 - 0.375, 3);
  });
});

describe('the Warrior', () => {
  it('is the Knight with one sword and no shield', async () => {
    const knight = await open('warrior');
    try {
      expect(knight.scene.getMeshByName('1H_Sword')?.isEnabled()).toBe(true);
      for (const gear of ['2H_Sword', 'Round_Shield', 'Spike_Shield']) {
        expect(knight.scene.getMeshByName(gear)?.isEnabled()).toBe(false);
      }
    } finally {
      knight.dispose();
    }
  });
});
