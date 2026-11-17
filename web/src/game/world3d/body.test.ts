import { readFileSync } from 'node:fs';
import path from 'node:path';

import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { LoadAssetContainerAsync } from '@babylonjs/core/Loading/sceneLoader';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { registerBuiltInLoaders } from '@babylonjs/loaders/dynamic';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';

import type { Swing } from '../systems/mob-attacks';

import { MOB_FASTEST, mobBeats } from './beats';
import type { Beats } from './beats';
import { BLEND_MS } from './blend';
import { Body, FADED, RISE_MS, STILL, sunShadows } from './body';
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
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
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

  it('fade, as a dodge shows them: each mesh its own colour at FADED, the ink hidden, and whole again after', async () => {
    const b = await body(classLook('thief'));
    const whole = meshes(b).map((m) => [m.material, colorOf(m)] as const);
    const inks = () =>
      meshes(b).map((m) =>
        m
          .getChildMeshes(true)
          .find((c) => c.name === `${m.name} ink`)!
          .isEnabled(),
      );
    const alpha = (m: AbstractMesh) =>
      ((m.material as NodeMaterial).getBlockByName('alpha') as { value: number } | null)?.value;
    b.fade(true);
    meshes(b).forEach((m, i) => {
      expect([
        m.name,
        alpha(m),
        colorOf(m)?.equals(whole[i]![1]),
        m.material!.needDepthPrePass,
      ]).toEqual([
        m.name,
        FADED,
        true,
        true, // its front alone
      ]);
    });
    expect(inks().some(Boolean)).toBe(false);
    b.fade(true); // again: no change
    b.fade(false);
    expect(meshes(b).map((m) => m.material)).toEqual(whole.map(([m]) => m));
    expect(inks().every(Boolean)).toBe(true);
    b.dispose();
  });

  it('warm each faded material’s shader for each kind of mesh it draws, so a sword sharing the body’s material doesn’t leave the skinned one to the first dodge', async () => {
    const ready = vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh');
    const b = await body(classLook('warrior'));
    const warmed = new Set(
      ready.mock.calls.map(
        ([mesh], i) => `${(ready.mock.contexts[i] as NodeMaterial).uniqueId} ${mesh.useBones}`,
      ),
    );
    ready.mockRestore();
    b.fade(true);
    const drawn = meshes(b).map((m) => `${m.material!.uniqueId} ${m.useBones}`);
    expect(new Set(drawn.map((k) => k.split(' ')[1])).size).toBe(2); // skinned and not, one material
    expect(drawn.filter((k) => !warmed.has(k))).toEqual([]);
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
  it('fill their cylinders, never wider than their circles, a flyer at their middle, what they wear over that on their heads: the king slime three slimes big', async () => {
    const drawn = new Map<string, { width: number; height: number }>();
    for (const { id: type } of mobs) {
      const look = mobLook(type);
      const b = await body(look);
      const worn = (m: AbstractMesh) => look.over?.includes(m.name) ?? false;
      const { min, max } = b.root.getHierarchyBoundingVectors(
        true,
        (m) => m.isEnabled() && !m.name.endsWith(' ink') && !worn(m),
      );
      if (look.over) {
        const over = b.root.getHierarchyBoundingVectors(true, (m) => m.isEnabled() && worn(m));
        expect([type, over.min.y + over.max.y > min.y + max.y, over.max.y > max.y]).toEqual([
          type,
          true,
          true,
        ]); // worn up top, rising over the body
      }
      const size = { width: Math.max(max.x - min.x, max.z - min.z), height: max.y - min.y };
      drawn.set(type, size);
      const fits = size.width <= look.width! + 1e-3 && size.height <= look.height + 1e-3;
      const fills =
        Math.abs(size.width - look.width!) < 1e-3 || Math.abs(size.height - look.height) < 1e-3;
      const stands = look.flies ? (min.y + max.y) / 2 : min.y; // a flyer's middle at its cylinder's, a walker's feet on the ground
      expect([type, fits, fills, stands]).toEqual([
        type,
        true,
        true,
        expect.closeTo(look.flies ? look.height / 2 : 0, 3),
      ]);
      b.dispose();
    }
    expect(drawn.get('green_slime')!.width).toBeCloseTo(1, 3); // the blob is wider than tall: its circle sets it
    expect(drawn.get('king_slime')!.width).toBeCloseTo(3 * drawn.get('green_slime')!.width, 3); // its crowned blob, re-exported by Blender, rests 3% squatter: circles set both
  });

  it('stand upright, facing north', async () => {
    const b = await body(mobLook('orange_mushroom'));
    const centre = (mesh: string) => {
      const { min, max } = b.root.getHierarchyBoundingVectors(true, (m) => m.name.endsWith(mesh));
      return min.add(max).scale(0.5);
    };
    expect(centre('Blob_primitive0').y).toBeGreaterThan(centre('Blob_primitive1').y); // cap over stem
    expect(centre('Blob_primitive2').z).toBeGreaterThan(centre('Blob_primitive1').z); // eyes ahead
    b.dispose();
  });

  it('draw their tint on the main material: the mushroom’s cap orange, the king slime dark green', async () => {
    for (const { id: type, look } of mobs.filter((m) => m.look.tint !== null)) {
      const b = await body(mobLook(type));
      expect([type, meshes(b).some((m) => colorOf(m)?.equals(linearOf(look.tint!)))]).toEqual([
        type,
        true,
      ]);
      b.dispose();
    }
    const slime = await body(mobLook('green_slime'));
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

  it.each([
    ['glacier_snail', 'charge'],
    ['green_slime', 'hop'],
    ['king_slime', 'hop'],
    ['tomb_rat', 'bite'],
    ['king_slime', 'slam'],
    ['shade_lord', 'slam'],
    ['dust_vulture', 'spit'],
    ['blizzard_wing', 'spit'],
    ['snow_weasel', 'bite'],
  ])(
    'pose a mob’s strike on the world’s hit, %s’s %s, each side of it no slower than a third of its clip nor faster than 1.5 times, standing outside that',
    async (id, name) => {
      const mob = mobs.find((m) => m.id === id)!;
      const a = mob.attacks.find((x) => x.name === name)!;
      const look = mobLook(id);
      const clip = look.clips.attack!;
      const b = await body(look);
      const g = scene.animationGroups.filter((x) => x.name === clip.clip).at(-1)!;
      const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
      const ms = ((g.to - g.from) / g.targetedAnimations[0]!.animation.framePerSecond) * 1000;
      const start = 1000;
      const acts = start + a.windup_ms;
      const travel = a.kind === 'leap' || a.kind === 'rush' ? a.travel_ms : 0;
      // A player 3 m north, on a charge's lane: the world's hit is a hop's landing, the charge's meeting them, the others' windup's end.
      const me = { x: 0, y: 0, z: 3, radius: 0.4, height: 1.8 };
      const met = a.kind === 'rush' ? (3 - mob.radius - me.radius) / a.distance : 1;
      const hit = acts + travel * met;
      const readyAt = hit + a.recover_ms;
      const lane = a.kind === 'rush' ? a.distance : 0;
      const swing: Swing = {
        ...{ mob: 1, attack: a, yaw: 0, from: start, hit: acts, end: acts + travel, blow: hit },
        ...{ at: { x: 0, y: 0, z: 0 }, to: { x: 0, y: 0, z: lane } },
      };
      const from = Math.max(start, hit - 3 * clip.strike * ms);
      const to = Math.min(readyAt, hit + 3 * (1 - clip.strike) * ms);
      const begins = Math.max(0, clip.strike - ((hit - from) * 1.5) / ms); // a lead-in too short starts part-way
      const ends = Math.min(1, clip.strike + ((to - hit) * 1.5) / ms); // a follow-through too short fades out short of the end
      let last = start - FRAME_MS;
      let was = 0;
      let fastest = 0;
      let slowest = Infinity;
      const at = (t: number) => {
        b.update(t, t - last, STILL);
        const now = share();
        if (last >= from && t <= to && weight(clip.clip) > 0) {
          const rate = ((now - was) * ms) / (t - last); // of its clip's own pace
          [fastest, slowest] = [Math.max(fastest, rate), Math.min(slowest, rate)];
        }
        [last, was] = [t, now];
      };
      const through = (until: number) => {
        while (last + FRAME_MS < until) {
          at(last + FRAME_MS);
        }
        at(until);
      };
      at(start);
      b.act('attack', mobBeats(swing, hit, start, 50), MOB_FASTEST); // as the scene's combat does
      if (from > start) {
        through(from - 1);
        expect(weight(clip.clip)).toBe(0);
      }
      through((from + hit) / 2);
      expect(share()).toBeCloseTo((begins + clip.strike) / 2, 2);
      through(hit);
      expect(share()).toBeCloseTo(clip.strike, 2);
      through((hit + to) / 2);
      expect(share()).toBeCloseTo((clip.strike + ends) / 2, 2);
      through(to + FRAME_MS);
      expect(weight(clip.clip)).toBe(0);
      expect([fastest <= 1.5 + 1e-6, slowest >= 1 / 3 - 1e-6]).toEqual([true, true]);
      b.dispose();
    },
  );

  describe('restrike', () => {
    const charge = mobs.find((m) => m.id === 'glacier_snail')!.attacks[0]; // 800 ms windup, 400 ms run
    const acts = 1000 + charge.windup_ms;
    /** The snail's charge acted from 1000, planned to strike at its run's end, stepped to acts. */
    async function charging() {
      const look = mobLook('glacier_snail');
      const clip = look.clips.attack!;
      const b = await body(look);
      const g = scene.animationGroups.filter((x) => x.name === clip.clip).at(-1)!;
      const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
      const ms = ((g.to - g.from) / g.targetedAnimations[0]!.animation.framePerSecond) * 1000;
      b.update(1000, FRAME_MS, STILL);
      const end = acts + charge.travel_ms;
      b.act(
        'attack',
        { start: 1000, hit: end, strikeEnd: end, readyAt: end + 600, end: end + 600 },
        1.5,
      );
      const t = frames(b, 1000, acts - 1000);
      return { b, clip, share, ms, t };
    }

    it('brings the strike to where the run meets the player, judged as it starts', async () => {
      const { b, clip, share, t } = await charging();
      expect(share()).toBeLessThan(clip.strike);
      b.restrike(acts + 200, t);
      frames(b, t, 200 - FRAME_MS);
      b.update(acts + 200, FRAME_MS, STILL);
      expect(share()).toBeCloseTo(clip.strike, 2);
      b.dispose();
    });

    it('brings it to the world’s hit heard first, no faster than 1.5 times its clip', async () => {
      const { b, clip, share, ms, t } = await charging();
      const left = (clip.strike - share()) * ms;
      b.restrike(t, t); // the world's hit, now
      const struck = t + left / 1.5;
      frames(b, t, struck - t - FRAME_MS);
      expect(share()).toBeLessThan(clip.strike);
      b.update(struck, FRAME_MS, STILL);
      expect(share()).toBeCloseTo(clip.strike, 2);
      b.dispose();
    });

    it('leaves a strike already played, and one a flinch broke off', async () => {
      const struck = await charging();
      frames(struck.b, struck.t, charge.travel_ms + 50); // past its strike
      const after = struck.share();
      struck.b.restrike(acts, acts + charge.travel_ms + 50);
      struck.b.update(acts + charge.travel_ms + 50 + FRAME_MS, FRAME_MS, STILL);
      expect(struck.share()).toBeGreaterThan(after);
      struck.b.dispose();

      const broken = await charging();
      broken.b.pose('flinch', broken.t);
      broken.b.restrike(broken.t, broken.t);
      frames(broken.b, broken.t, 200);
      expect(weight(broken.clip.clip)).toBeLessThan(1);
      broken.b.dispose();
    });
  });

  it('drop a mob’s attack still waiting for its clip when it dies', async () => {
    const look = mobLook('glacier_snail');
    const b = await body(look);
    b.update(0, FRAME_MS, STILL);
    b.act('attack', { start: 0, hit: 800, strikeEnd: 1200, readyAt: 1800, end: 1800 });
    let t = frames(b, 0, 160);
    b.pose('dead', t);
    t = frames(b, t, 800);
    expect([weight(look.clips.attack!.clip), weight(look.clips.death)]).toEqual([0, 1]);
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
    const b = await body(mobLook('green_slime'));
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
    const b = await body(mobLook('green_slime'));
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
