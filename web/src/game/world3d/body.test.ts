import { readFileSync } from 'node:fs';
import path from 'node:path';

import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { LoadAssetContainerAsync } from '@babylonjs/core/Loading/sceneLoader';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { registerBuiltInLoaders } from '@babylonjs/loaders/dynamic';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';

import type { Beats } from './beats';
import { BLEND_MS } from './blend';
import { Body, RISE_MS, STILL } from './body';
import type { Gait, Lighting } from './body';
import { classLook, mobLook, npcLook } from './looks';
import type { ModelLook } from './looks';
import { linearOf, Palette } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');
const FRAME_MS = 16;
const ATTACK = '1H_Melee_Attack_Slice_Diagonal';
const SKILL = '1H_Melee_Attack_Chop';
const WALK: Gait = { pace: 'walk', onGround: true };

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
const containers = new Map<string, Promise<AssetContainer>>();

beforeAll(() => {
  registerBuiltInLoaders();
  engine = new NullEngine();
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: new ShadowGenerator(256, sun) };
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

async function body(look: ModelLook): Promise<Body> {
  let c = containers.get(look.file);
  if (!c) {
    c = LoadAssetContainerAsync(new Uint8Array(readFileSync(path.join(MODELS, look.file))), scene, {
      pluginExtension: '.glb',
      pluginOptions: { gltf: { alwaysComputeSkeletonRootNode: true } },
    });
    containers.set(look.file, c);
  }
  return new Body(scene, await c, look, lighting);
}

function modelOf(type: string): ModelLook {
  const l = mobLook(type);
  if (!('model' in l)) {
    throw new Error(`${type} has no model`);
  }
  return l.model;
}

function heightOf(root: TransformNode): number {
  const { min, max } = root.getHierarchyBoundingVectors(true, (m) => m.isEnabled());
  return max.y - min.y;
}

function meshes(b: Body): AbstractMesh[] {
  return b.root
    .getChildMeshes(false)
    .filter((m) => m.isEnabled() && m.getTotalVertices() > 0 && !m.name.endsWith(' ink'));
}

/** The colour block of mesh's toon material, linear. */
function colorOf(m: AbstractMesh): { equals(c: unknown): boolean } | undefined {
  const block = (m.material as NodeMaterial | null)?.getBlockByName('color') as
    { value: { equals(c: unknown): boolean } } | null | undefined;
  return block?.value;
}

function enabled(b: Body, name: string): boolean | undefined {
  return b.root
    .getDescendants(false)
    .find((n) => n.name === name)
    ?.isEnabled();
}

/** Steps b frame by frame from `from` for ms at gait, returning the time reached. */
function frames(b: Body, from: number, ms: number, gait: Gait = STILL): number {
  let t = from;
  for (let left = ms; left > 0; left -= FRAME_MS) {
    t += FRAME_MS;
    b.update(t, FRAME_MS, gait);
  }
  return t;
}

function weight(name: string): number {
  return scene.animationGroups.filter((g) => g.name === name).at(-1)?.weight ?? NaN;
}

const beats = (start: number, hit: number, readyAt: number): Beats => ({
  start,
  hit,
  strikeEnd: hit + (readyAt - hit) / 3,
  readyAt,
  end: readyAt + 100,
});

describe('the classes', () => {
  it('are each a KayKit adventurer 1.75 m tall, the gear another look shows hidden and the rest left out of the file', async () => {
    const gear: Record<string, [shown: string[], hidden: string[], absent: string[]]> = {
      beginner: [
        ['1H_Axe'],
        ['Barbarian_Hat', 'Barbarian_Cape'],
        ['Barbarian_Round_Shield', 'Mug'],
      ],
      warrior: [['1H_Sword'], [], ['2H_Sword', 'Round_Shield', 'Spike_Shield']],
      thief: [['Knife', 'Rogue_Cape'], [], ['Knife_Offhand', '2H_Crossbow']],
      bowman: [['2H_Crossbow', 'Rogue_Head_Hooded'], [], ['Knife', '1H_Crossbow']],
      magician: [['2H_Staff', 'Mage_Hat'], [], ['1H_Wand', 'Spellbook']],
    };
    for (const c of classes) {
      const b = await body(classLook(c.id));
      const [shown, hidden, absent] = gear[c.id] ?? [[], [], []];
      expect([c.id, heightOf(b.root)]).toEqual([c.id, expect.closeTo(1.75, 2)]);
      for (const g of shown) {
        expect([c.id, g, enabled(b, g)]).toEqual([c.id, g, true]);
      }
      for (const g of hidden) {
        expect([c.id, g, enabled(b, g)]).toEqual([c.id, g, false]);
      }
      for (const g of absent) {
        expect([c.id, g, enabled(b, g)]).toEqual([c.id, g, undefined]);
      }
      b.dispose();
    }
  });

  it('draw every mesh in a toon material, inked', async () => {
    const b = await body(classLook('beginner'));
    expect(meshes(b).length).toBeGreaterThan(0);
    for (const m of meshes(b)) {
      expect(m.material?.name).toMatch(/^toon /);
      const ink = m.getChildMeshes(true).find((c) => c.name === `${m.name} ink`);
      expect([m.name, ink?.material?.name, ink?.skeleton === m.skeleton]).toEqual([
        m.name,
        'ink',
        true,
      ]);
    }
    b.dispose();
  });

  it('tint the cape alone: the thief purple, the Beginner none', async () => {
    const thief = await body(classLook('thief'));
    const purple = linearOf(0x9a70d8);
    for (const m of meshes(thief)) {
      expect([m.name, colorOf(m)?.equals(purple)]).toEqual([m.name, m.name === 'Rogue_Cape']);
    }
    thief.dispose();
    expect(classLook('beginner').tint).toBeUndefined();
  });
});

describe('the NPCs', () => {
  it('draw a trainer as its class, the guide as the Barbarian with hat and cape', async () => {
    expect(npcLook('trainer_warrior').file).toBe('kaykit/Knight.glb');
    const guide = await body(npcLook('guide'));
    expect([enabled(guide, 'Barbarian_Hat'), enabled(guide, 'Barbarian_Cape')]).toEqual([
      true,
      true,
    ]);
    guide.dispose();
  });
});

describe('the mobs with a model', () => {
  it('fill their cylinders, never wider than their circles: the king slime three slimes big', async () => {
    const drawn = new Map<string, { width: number; height: number }>();
    for (const type of ['green_slime', 'orange_mushroom', 'king_slime']) {
      const look = modelOf(type);
      const b = await body(look);
      const { min, max } = b.root.getHierarchyBoundingVectors(
        true,
        (m) => m.isEnabled() && !m.name.endsWith(' ink'),
      );
      const size = { width: Math.max(max.x - min.x, max.z - min.z), height: max.y - min.y };
      drawn.set(type, size);
      const fits = size.width <= look.width! + 1e-3 && size.height <= look.height + 1e-3;
      const fills =
        Math.abs(size.width - look.width!) < 1e-3 || Math.abs(size.height - look.height) < 1e-3;
      expect([type, fits, fills]).toEqual([type, true, true]);
      b.dispose();
    }
    expect(drawn.get('green_slime')!.width).toBeCloseTo(1, 3); // the blob is wider than tall: its circle sets it
    expect(drawn.get('king_slime')!.height).toBeCloseTo(3 * drawn.get('green_slime')!.height, 2);
  });

  it('stand upright, facing north', async () => {
    const b = await body(modelOf('orange_mushroom'));
    const centre = (mesh: string) => {
      const { min, max } = b.root.getHierarchyBoundingVectors(true, (m) => m.name.endsWith(mesh));
      return min.add(max).scale(0.5);
    };
    expect(centre('Blob_primitive0').y).toBeGreaterThan(centre('Blob_primitive1').y); // cap over stem
    expect(centre('Blob_primitive2').z).toBeGreaterThan(centre('Blob_primitive1').z); // eyes ahead
    b.dispose();
  });

  it('draw their tint on the main material: the mushroom’s cap orange, the king slime dark green', async () => {
    for (const [type, hex] of [
      ['orange_mushroom', 0xe08a3c],
      ['king_slime', 0x4a6b3c],
    ] as const) {
      const b = await body(modelOf(type));
      expect([type, meshes(b).some((m) => colorOf(m)?.equals(linearOf(hex)))]).toEqual([
        type,
        true,
      ]);
      b.dispose();
    }
    const slime = await body(modelOf('green_slime'));
    expect(meshes(slime).some((m) => colorOf(m)?.equals(linearOf(0x4a6b3c)))).toBe(false);
    slime.dispose();
  });
});

describe('actions', () => {
  it('play on the whole body while standing, and on the upper body alone while walking', async () => {
    const b = await body(classLook('beginner'));
    b.act('attack', beats(0, 0, 900));
    frames(b, 0, BLEND_MS + FRAME_MS);
    expect([weight(ATTACK), weight(`${ATTACK}/upper`), weight('Idle')]).toEqual([1, 1, 0]);
    b.act('attack', beats(200, 200, 1100));
    frames(b, 200, BLEND_MS + 2 * FRAME_MS, WALK);
    expect([
      weight(`${ATTACK}/upper`),
      weight(ATTACK),
      weight('Walking_A'),
      weight('Walking_A/upper'),
    ]).toEqual([1, 0, 1, 0]);
    b.dispose();
  });

  it('pose the strike on the hit and the end on the ready point', async () => {
    const b = await body(classLook('beginner'));
    const g = scene.animationGroups.filter((x) => x.name === ATTACK).at(-1)!;
    const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
    const strike = classLook('beginner').clips.attack!.strike;
    b.act('attack', beats(0, 0, 900));
    frames(b, 0, FRAME_MS);
    expect(share()).toBeGreaterThanOrEqual(strike);
    expect(share()).toBeLessThan(strike + 0.03);
    frames(b, FRAME_MS, 900 - 2 * FRAME_MS);
    expect(share()).toBeGreaterThan(0.97);
    b.dispose();
  });

  it('let a new action cut the last within 80 ms', async () => {
    const b = await body(classLook('beginner'));
    b.act('attack', beats(0, 0, 900));
    let t = frames(b, 0, 300);
    b.act('skill', beats(t, t + 100, t + 900));
    t = frames(b, t, BLEND_MS + FRAME_MS);
    expect([weight(ATTACK), weight(SKILL)]).toEqual([0, 1]);
    b.dispose();
  });

  it('mask the upper body from the spine up', async () => {
    const b = await body(classLook('beginner'));
    const groups = scene.animationGroups;
    const upper = groups.filter((g) => g.name === `${ATTACK}/upper`).at(-1)!.mask!;
    const lower = groups.filter((g) => g.name === ATTACK).at(-1)!.mask!;
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
    b.dispose();
  });
});

describe('poses', () => {
  it('flinch through the hit clip once, then stand', async () => {
    const b = await body(modelOf('green_slime'));
    b.pose('flinch', 0);
    frames(b, 0, BLEND_MS + FRAME_MS);
    expect(weight('CharacterArmature|HitRecieve')).toBe(1);
    frames(b, BLEND_MS + FRAME_MS, 2000);
    expect([weight('CharacterArmature|HitRecieve'), weight('CharacterArmature|Idle')]).toEqual([
      0, 1,
    ]);
    b.dispose();
  });

  it('hold a fall down until it rises, and stand once risen', async () => {
    const b = await body(modelOf('green_slime'));
    const death = scene.animationGroups.filter((g) => g.name === 'CharacterArmature|Death').at(-1)!;
    const share = () => (death.getCurrentFrame() - death.from) / (death.to - death.from);
    b.pose('down', 0);
    let t = frames(b, 0, 3000);
    expect([weight('CharacterArmature|Death'), share()]).toEqual([1, expect.closeTo(1, 2)]);
    b.pose('rising', t);
    t = frames(b, t, RISE_MS / 2);
    expect(share()).toBeGreaterThan(0.3);
    expect(share()).toBeLessThan(0.7);
    b.pose('up', t);
    frames(b, t, RISE_MS + BLEND_MS + FRAME_MS);
    expect([weight('CharacterArmature|Death'), weight('CharacterArmature|Idle')]).toEqual([0, 1]);
    b.dispose();
  });
});

describe('dispose', () => {
  it('lets go of all a body made and none of what other bodies share', async () => {
    const counts = () =>
      [
        scene.meshes,
        scene.transformNodes,
        scene.skeletons,
        scene.animationGroups,
        scene.materials,
      ].map((a) => a.length);
    const kept = await body(classLook('warrior'));
    const before = counts();
    const b = await body(classLook('warrior'));
    b.dispose();
    expect(counts()).toEqual(before);
    expect(meshes(kept).every((m) => m.material && scene.materials.includes(m.material))).toBe(
      true,
    );
    kept.dispose();
  });
});
