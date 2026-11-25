import { readFileSync } from 'node:fs';
import path from 'node:path';

import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { CascadedShadowGenerator } from '@babylonjs/core/Lights/Shadows/cascadedShadowGenerator';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { builtShaders } from '@/testing/shaders';

import type { Swing } from '../systems/mob-attacks';

import { MOB_FASTEST, mobBeats } from './beats';
import type { Beats } from './beats';
import { BLEND_MS } from './blend';
import { Body, FADED, regionShadows, RISE_MS, STILL, sunShadows } from './body';
import type { Gait, Lighting } from './body';
import { classLook, mobLook, npcLook } from './looks';
import type { ModelLook } from './looks';
import { Models } from './models';
import { linearOf, Palette } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');
const FRAME_MS = 16;
const ATTACK = '1H_Melee_Attack_Slice_Diagonal';
const SKILL = '1H_Melee_Attack_Chop';
const WALK: Gait = { pace: 'walk', onGround: true };

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
let models: Models;

beforeAll(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
  models = new Models(scene, (f) => new Uint8Array(readFileSync(path.join(MODELS, f))));
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

async function body(look: ModelLook): Promise<Body> {
  return new Body(scene, await models.load(look.file), look, lighting);
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
    b.update(t, FRAME_MS, gait, true);
  }
  return t;
}

/** The share group name draws at: Babylon's -1, unweighted, is all of it. */
function weight(name: string): number {
  const w = scene.animationGroups.filter((g) => g.name === name).at(-1)?.weight ?? NaN;
  return w === -1 ? 1 : w;
}

const beats = (start: number, hit: number, readyAt: number): Beats => ({
  start,
  hit,
  strikeEnd: hit + (readyAt - hit) / 3,
  readyAt,
  end: readyAt + 100,
});

describe('a region’s sun shadows', () => {
  it('fall in three cascades out to 80 m from the camera, held steady as it moves and cast by see-through meshes too; the one map’s where the engine can’t cascade', () => {
    const own = new NullEngine();
    const at = new Scene(own);
    const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), at);
    const plain = regionShadows(sun, 256);
    expect([plain.constructor, plain.transparencyShadow]).toEqual([ShadowGenerator, true]); // the null engine can't
    plain.dispose();
    (own as unknown as { _features: { supportCSM: boolean } })._features.supportCSM = true;
    const map = vi
      .spyOn(
        CascadedShadowGenerator.prototype as unknown as { _initializeGenerator(): void },
        '_initializeGenerator',
      )
      .mockImplementation(() => undefined); // its shadow map, which wants a real GL context
    const cascaded = regionShadows(sun, 256) as CascadedShadowGenerator;
    map.mockRestore();
    new TargetCamera('camera', Vector3.Zero(), at); // made after, as the world makes it: the cascades read their reach from it
    expect(cascaded).toBeInstanceOf(CascadedShadowGenerator);
    expect([
      cascaded.numCascades,
      cascaded.shadowMaxZ,
      cascaded.stabilizeCascades,
      cascaded.transparencyShadow,
    ]).toEqual([3, 80, true, true]);
    at.dispose();
    own.dispose();
  });
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

  it('build no shader on a first dodge, though their clips stray from a uniform scale by a hair, which would take a shader of its own', async () => {
    const camera = new TargetCamera('eye', new Vector3(0, 1, -4), scene);
    camera.setTarget(new Vector3(0, 1, 0));
    scene.activeCamera = camera;
    const b = await body(classLook('magician'));
    for (let i = 0; i < 30; i++) {
      b.update(i * FRAME_MS, FRAME_MS, STILL, true);
      scene.render();
    }
    const built = new Set(builtShaders(engine));
    b.fade(true);
    scene.render();
    expect(builtShaders(engine).filter((k) => !built.has(k))).toEqual([]);
    expect(b.root.getChildMeshes(false).filter((m) => m.nonUniformScaling)).toEqual([]);
    b.dispose();
    camera.dispose();
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

  it('raise the guard as a block starts and hold it to the ready point, every class alike', async () => {
    for (const c of classes) {
      expect([c.id, classLook(c.id).clips.block]).toEqual([c.id, { clip: 'Block', strike: 0.28 }]);
    }
    const b = await body(classLook('beginner'));
    const g = scene.animationGroups.filter((x) => x.name === 'Block').at(-1)!;
    const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
    b.act('block', beats(0, 0, 600)); // a block's hit is its start
    frames(b, 0, FRAME_MS);
    expect(share()).toBeGreaterThanOrEqual(0.28);
    expect(share()).toBeLessThan(0.31);
    frames(b, FRAME_MS, 600 - 2 * FRAME_MS);
    expect(share()).toBeGreaterThan(0.95);
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
        b.update(t, t - last, STILL, true);
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
      b.update(1000, FRAME_MS, STILL, true);
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
      b.update(acts + 200, FRAME_MS, STILL, true);
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
      b.update(struck, FRAME_MS, STILL, true);
      expect(share()).toBeCloseTo(clip.strike, 2);
      b.dispose();
    });

    it('leaves a strike already played, and one a flinch broke off', async () => {
      const struck = await charging();
      frames(struck.b, struck.t, charge.travel_ms + 50); // past its strike
      const after = struck.share();
      struck.b.restrike(acts, acts + charge.travel_ms + 50);
      struck.b.update(acts + charge.travel_ms + 50 + FRAME_MS, FRAME_MS, STILL, true);
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
    b.update(0, FRAME_MS, STILL, true);
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

describe('a sprint', () => {
  it('plays the run 1.3 times as fast, the body leaning 8° forward from the feet, and eases back to the run as it ends', async () => {
    const look = classLook('warrior');
    const b = await body(look);
    const group = scene.animationGroups.filter((g) => g.name === look.clips.run).at(-1)!;
    const fps = group.targetedAnimations[0]!.animation.framePerSecond;
    let t = 0;
    /** How fast the run plays over 100 ms of gait, against its own clock. */
    const rate = (gait: Gait) => {
      const [before, from] = [group.animatables[0]!.masterFrame, t];
      t = frames(b, t, 100, gait);
      const length = group.to - group.from;
      return (
        ((group.animatables[0]!.masterFrame - before + length) % length) /
        ((fps * (t - from)) / 1000)
      );
    };
    const lean = () =>
      b.root.getChildren().find((n) => n.name === `${look.file} lean`) as TransformNode;
    const run: Gait = { pace: 'run', onGround: true };
    const sprint: Gait = { pace: 'sprint', onGround: true };
    t = frames(b, t, 300, run);
    expect([weight(look.clips.run), rate(run), lean().rotation.x]).toEqual([
      1,
      expect.closeTo(1, 1),
      0,
    ]);
    t = frames(b, t, 300, sprint);
    expect([weight(look.clips.run), rate(sprint), lean().rotation.x]).toEqual([
      1,
      expect.closeTo(1.3, 1),
      expect.closeTo((8 * Math.PI) / 180, 2), // all but eased in
    ]);
    t = frames(b, t, 300, run);
    expect([rate(run), lean().rotation.x]).toEqual([expect.closeTo(1, 1), expect.closeTo(0, 2)]);
    b.dispose();
  });
});

describe('posing', () => {
  /** Every joint's turn under b's model, as last posed. */
  const joints = (b: Body) =>
    b.root
      .getDescendants(false)
      .flatMap((n) =>
        'rotationQuaternion' in n && n.rotationQuaternion
          ? [...(n.rotationQuaternion as { asArray(): number[] }).asArray()]
          : [],
      );

  it('draws a clip that holds all the weight unweighted, and a blend weighted', async () => {
    const look = classLook('warrior');
    const b = await body(look);
    const raw = (name: string) =>
      scene.animationGroups.filter((g) => g.name === name).at(-1)!.weight;
    let t = frames(b, 0, 300, STILL);
    expect([raw(look.clips.idle), raw(look.clips.run)]).toEqual([-1, 0]);
    t = frames(b, t, FRAME_MS, { pace: 'run', onGround: true }); // a fade begun
    expect(raw(look.clips.run)).toBeGreaterThan(0);
    expect(raw(look.clips.run) + raw(look.clips.idle)).toBeCloseTo(1, 9);
    frames(b, t, 300, { pace: 'run', onGround: true });
    expect([raw(look.clips.idle), raw(look.clips.run)]).toEqual([0, -1]);
    b.dispose();
  });

  it('poses its bones on its first update even unasked, so it’s never drawn in its rig’s rest pose', async () => {
    const look = classLook('warrior');
    const [rest, asked, unasked] = [await body(look), await body(look), await body(look)];
    asked.update(FRAME_MS, FRAME_MS, STILL, true);
    unasked.update(FRAME_MS, FRAME_MS, STILL, false);
    scene.animate();
    expect(joints(unasked)).toEqual(joints(asked));
    expect(joints(unasked)).not.toEqual(joints(rest));
    const before = joints(unasked);
    unasked.update(2 * FRAME_MS, FRAME_MS, { pace: 'run', onGround: true }, false);
    expect(joints(unasked)).toEqual(before); // the next unasked is skipped
    for (const b of [rest, asked, unasked]) {
      b.dispose();
    }
  });

  it('poses its bones only when asked, its clips and actions running on meanwhile, so a skipped frame keeps the last pose and the next lands where every frame would', async () => {
    const look = classLook('warrior');
    const [every, some] = [await body(look), await body(look)];
    const run: Gait = { pace: 'run', onGround: true };
    expect(joints(every).length).toBeGreaterThan(4 * 30); // every joint of the rig
    let t = 0;
    for (let i = 1; i <= 36; i++) {
      t += FRAME_MS;
      if (i === 18) {
        every.act('attack', beats(t, t, t + 600)); // a swing begun on the run
        some.act('attack', beats(t, t, t + 600));
      }
      every.update(t, FRAME_MS, i < 12 ? STILL : run, true);
      const before = joints(some);
      some.update(t, FRAME_MS, i < 12 ? STILL : run, i % 4 === 0);
      scene.animate();
      if (i % 4 === 0 || i === 1) {
        joints(some).forEach((q, k) => expect(q, `frame ${i}`).toBeCloseTo(joints(every)[k]!, 6));
      } else {
        expect(joints(some), `frame ${i}`).toEqual(before);
      }
    }
    every.dispose();
    some.dispose();
  });
});

describe('shadows', () => {
  it('casts from every mesh it draws until told not to, and again when told to, once each way', async () => {
    const b = await body(classLook('warrior'));
    const list = () => lighting.shadows.getShadowMap()!.renderList!;
    const mine = () => list().filter((m) => b.root.getChildMeshes(false).includes(m));
    const drawn = meshes(b);
    expect(mine()).toEqual(expect.arrayContaining(drawn));
    const all = list().length;
    const removed = vi.spyOn(lighting.shadows, 'removeShadowCaster');
    const added = vi.spyOn(lighting.shadows, 'addShadowCaster');
    b.cast(false);
    b.cast(false);
    expect([mine(), list().length]).toEqual([[], all - drawn.length]);
    b.cast(true);
    b.cast(true);
    expect([mine().length, list().length]).toEqual([drawn.length, all]);
    expect([removed.mock.calls.length, added.mock.calls.length]).toEqual([
      drawn.length,
      drawn.length,
    ]); // told twice, it goes through its meshes once
    removed.mockRestore();
    added.mockRestore();
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
