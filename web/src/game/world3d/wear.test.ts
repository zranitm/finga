import { readFileSync } from 'node:fs';
import path from 'node:path';

import { Bone } from '@babylonjs/core/Bones/bone';
import { Skeleton } from '@babylonjs/core/Bones/skeleton';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import type { TextureBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/textureBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import type { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';
import { HINGE, SPAN, TEST_WINGS, wingsFile } from '@/testing/wings-fixture';

import type { Beats } from './beats';
import { STILL, sunShadows } from './body';
import type { Body, Gait, Lighting } from './body';
import { Figures } from './figures';
import { classLook, mobLook } from './looks';
import type { ModelLook } from './looks';
import { Models } from './models';
import { PIECE_LOOKS, colourwayFile, partsFile } from './outfit-looks';
import type { PieceLook } from './outfit-looks';
import { linearOf, Palette } from './toon';
import { FLAPS, sameJoints, WING_EASE_MS, wingState } from './wear';

const MODELS = path.resolve(process.cwd(), 'public/models');
const FRAME_MS = 16;
const RAD = Math.PI / 180;
const RUN: Gait = { pace: 'run', onGround: true };
const SPRINT: Gait = { pace: 'sprint', onGround: true };
const JUMP: Gait = { pace: 'still', onGround: false };

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
let models: Models;
let figures: Figures;
const files = new Map<string, Uint8Array>();

beforeAll(async () => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
  files.set(partsFile(TEST_WINGS.file), await wingsFile());
  models = new Models(
    scene,
    (f) => files.get(f) ?? new Uint8Array(readFileSync(path.join(MODELS, f))),
  );
  figures = new Figures(scene, lighting, models);
  const used = new Set([
    ...classes.map((c) => classLook(c.id).file),
    ...Object.values(PIECE_LOOKS).map((p) => partsFile(p.file)),
  ]);
  await Promise.all([...used].map((f) => models.load(f))); // once, before any test times its own work
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

const wearing = (cls: string, ...looks: string[]) => classLook(cls, { looks, bareHead: false });
const make = async (look: ModelLook) => (await figures.make(look)) as Body;

function nodesOf(b: Body): Map<string, TransformNode> {
  const nodes = new Map<string, TransformNode>();
  for (const n of b.root.getDescendants(false)) {
    if (n instanceof TransformNode) {
      nodes.set(n.name, n);
    }
  }
  return nodes;
}

const mesh = (b: Body, name: string) => nodesOf(b).get(name) as AbstractMesh | undefined;

function textureOf(m: AbstractMesh): unknown {
  return ((m.material as NodeMaterial).getBlockByName('texture') as TextureBlock | null)?.texture;
}

/** A skinned mesh's bounds as its rig poses it, about the Rig node it hangs from. */
function boundsOnRig(m: AbstractMesh): number[] {
  m.skeleton!.prepare(true);
  m.refreshBoundingInfo({ applySkeleton: true });
  const { minimum, maximum } = m.getBoundingInfo().boundingBox;
  return [...minimum.asArray(), ...maximum.asArray()];
}

const JOINT: Readonly<Record<string, string>> = { headwear: 'head', weapon: 'handslot.r' };

describe('an outfit piece worn', () => {
  const bySlot = new Map<string, [string, PieceLook][]>();
  for (const entry of Object.entries(PIECE_LOOKS)) {
    bySlot.set(entry[1].slot, [...(bySlot.get(entry[1].slot) ?? []), entry]);
  }
  const slots = [...bySlot.values()];
  // each figure wears a piece of every slot at once, so every piece is worn in as few figures as the fullest slot has pieces
  const sets = Array.from({ length: Math.max(...slots.map((g) => g.length)) }, (_, i) =>
    slots.map((g) => g[i % g.length]!),
  );

  it('wears every piece in some figure', () => {
    expect(new Set(sets.flat().map(([key]) => key))).toEqual(new Set(Object.keys(PIECE_LOOKS)));
  });

  it.each(classes.map((c) => c.id))(
    'binds on the %s’s rig: skinned parts on its skeleton, the rest on their joints, its own meshes for the slot hidden, its size as before',
    async (cls) => {
      const plain = await make(classLook(cls));
      const scale = plain.root.scaling.x;
      plain.dispose();
      for (const set of sets) {
        const look = wearing(cls, ...set.map(([key]) => key));
        const b = await make(look);
        const own = [...b.root.getChildMeshes(false)].find(
          (m) => m.skeleton && !m.name.endsWith(' worn'),
        )!;
        for (const [key, piece] of set) {
          for (const name of piece.meshes) {
            const m = mesh(b, `${name} worn`);
            expect(m?.isEnabled(), name).toBe(true);
            if (m!.skeleton) {
              expect(m!.skeleton, name).toBe(own.skeleton);
              expect(m!.parent?.name).toBe('Rig');
            } else {
              expect(m!.parent?.name, name).toBe(
                name.endsWith('_Cape') ? 'chest' : JOINT[piece.slot],
              );
            }
          }
          if (piece.slot !== 'face' && piece.slot !== 'wings') {
            for (const name of look.own![piece.slot]) {
              expect(mesh(b, name)?.isEnabled(), `${key} ${name}`).toBe(false);
            }
          }
        }
        expect(b.root.scaling.x, set.map(([key]) => key).join(' ')).toBeCloseTo(scale, 9);
        b.dispose();
      }
    },
  );

  it('skins a part on another class’s rig just as on its own: a knight’s body on a magician lies where the knight’s own does', async () => {
    const knight = await make(classLook('warrior'));
    const mage = await make(wearing('magician', 'knight_body'));
    for (const name of PIECE_LOOKS.knight_body!.meshes.filter((n) => !n.endsWith('_Cape'))) {
      const theirs = boundsOnRig(mesh(knight, name)!);
      boundsOnRig(mesh(mage, `${name} worn`)!).forEach((v, i) =>
        expect(v, name).toBeCloseTo(theirs[i]!, 4),
      );
    }
    knight.dispose();
    mage.dispose();
  });

  it('draws a colourway’s pieces in its atlas, one material for all who wear it, and a plain piece in its own file’s', async () => {
    const a = await make(wearing('magician', 'knight_crimson'));
    const b = await make(wearing('thief', 'knight_crimson'));
    const plain = await make(wearing('thief', 'knight_body'));
    const atlas = textureOf(mesh(a, 'Knight_Body worn')!) as { name: string };
    expect(atlas.name).toContain(colourwayFile('knight_crimson'));
    expect(mesh(b, 'Knight_Body worn')!.material).toBe(mesh(a, 'Knight_Body worn')!.material);
    const like = (await models.load(partsFile('kaykit/Knight.glb'))).textures[0] as Texture;
    const sampled = atlas as unknown as Texture;
    expect([
      sampled.invertY,
      sampled.samplingMode,
      sampled.noMipmap,
      sampled.wrapU,
      sampled.wrapV,
    ]).toEqual([like.invertY, like.samplingMode, like.noMipmap, like.wrapU, like.wrapV]); // sampled as the glTF atlas it recolours
    const own = textureOf(mesh(plain, 'Knight_Body worn')!);
    expect(own).not.toBe(atlas);
    expect(own).toBe((await models.load(partsFile('kaykit/Knight.glb'))).textures[0]);
    expect(textureOf(mesh(a, 'Mage_Head')!)).toBe(
      (await models.load('kaykit/Mage.glb')).textures[0],
    ); // the wearer's own head as it was
    for (const x of [a, b, plain]) x.dispose();
  });

  it('keeps the class’s tint on its own cape, and a worn cape its own colour', async () => {
    const thief = classes.find((c) => c.id === 'thief')!;
    const own = await make(classLook('thief'));
    const worn = await make(wearing('thief', 'mage_body'));
    const colour = (m: AbstractMesh) =>
      (
        (m.material as NodeMaterial).getBlockByName('color') as unknown as {
          value: { equals(c: unknown): boolean };
        }
      ).value;
    expect(colour(mesh(own, 'Rogue_Cape')!).equals(linearOf(thief.look.tint))).toBe(true);
    expect(colour(mesh(worn, 'Mage_Cape worn')!).equals(linearOf(thief.look.tint))).toBe(false);
    own.dispose();
    worn.dispose();
  });

  it('swings its trail on the weapon look in place of the class’s own, and a class with no trail gets none', async () => {
    const b = await make(wearing('warrior', 'two_handed_sword'));
    expect(mesh(b, '1H_Sword')?.isEnabled()).toBe(false);
    expect(scene.getTransformNodeByName('blade')).not.toBeNull();
    const blades = scene.transformNodes.filter(
      (n) => n.name === 'blade' && n.parent?.name === '2H_Sword worn',
    );
    expect(blades).toHaveLength(1);
    b.dispose();
    const mage = await make(wearing('magician', 'two_handed_sword'));
    expect(
      scene.transformNodes.filter((n) => n.name === 'blade' && n.parent?.name === '2H_Sword worn'),
    ).toHaveLength(0);
    mage.dispose();
  });

  it('moves and scales a rigid piece by its fit to the wearer’s file, and leaves it as it hangs on another', async () => {
    const fitted: PieceLook = {
      ...PIECE_LOOKS.mage_hat!,
      fit: { 'kaykit/Knight.glb': { offset: [0, 0.1, -0.02], scale: 1.2 } },
    };
    const parts = await models.load(partsFile('kaykit/Mage.glb'));
    const hat = parts.meshes.find((m) => m.name === 'Mage_Hat')!;
    const knight = await make({ ...classLook('warrior'), wear: [fitted] });
    const worn = mesh(knight, 'Mage_Hat worn')!;
    expect(worn.position.asArray().map((v, i) => v - hat.position.asArray()[i]!)).toEqual([
      0,
      expect.closeTo(0.1, 9),
      expect.closeTo(-0.02, 9),
    ]);
    expect(worn.scaling.x).toBeCloseTo(hat.scaling.x * 1.2, 9);
    const thief = await make({ ...classLook('thief'), wear: [fitted] });
    expect(mesh(thief, 'Mage_Hat worn')!.position.equals(hat.position)).toBe(true);
    knight.dispose();
    thief.dispose();
  });

  it('shows no headwear on a bare head, the class’s own hidden and a worn one left off, at the size it stands in its own', async () => {
    const own = await make(classLook('magician'));
    const b = await make(
      classLook('magician', { looks: ['knight_helmet', 'mage_body'], bareHead: true }),
    );
    const bare = await make(classLook('magician', { looks: [], bareHead: true }));
    for (const x of [b, bare]) {
      expect(mesh(x, 'Mage_Hat')?.isEnabled()).toBe(false);
      expect(x.root.scaling.x).toBeCloseTo(own.root.scaling.x, 9); // its tall hat still counted
    }
    expect(mesh(b, 'Knight_Helmet worn')).toBeUndefined();
    expect(mesh(b, 'Mage_Body worn')?.isEnabled()).toBe(true);
    await expect(figures.make({ ...mobLook('green_slime'), bareHead: true })).rejects.toThrow(
      'wears no pieces',
    );
    for (const x of [own, b, bare]) x.dispose();
  });

  it('copies the look into the afterimage: the worn parts drawn, the class’s own they replace left out', async () => {
    const b = await make(wearing('warrior', 'mage_body', 'two_handed_axe'));
    const still = b.afterimage(new StandardMaterial('after', scene));
    const names = still.meshes.map((m) => m.name);
    expect(names).toEqual(
      expect.arrayContaining(['Mage_Body worn', 'Mage_Cape worn', '2H_Axe worn', 'Knight_Head']),
    );
    expect(names).not.toContain('Knight_Body');
    expect(names).not.toContain('1H_Sword');
    expect(still.meshes.find((m) => m.name === 'Mage_Body worn')!.skeleton).not.toBe(
      mesh(b, 'Mage_Body worn')!.skeleton,
    ); // the copy's own rig
    b.dispose();
  });

  it('refuses a piece from a rig with other joints, and on a body that wears no pieces', async () => {
    files.set(
      'test/parts/Blob.glb',
      new Uint8Array(readFileSync(path.join(MODELS, 'quaternius/GreenBlob.glb'))),
    );
    const odd: PieceLook = { slot: 'outfit', file: 'test/Blob.glb', meshes: ['Blob'] };
    await expect(figures.make({ ...classLook('warrior'), wear: [odd] })).rejects.toThrow(
      'other joints',
    );
    await expect(
      figures.make({ ...mobLook('green_slime'), wear: [PIECE_LOOKS.mage_hat!] }),
    ).rejects.toThrow('wears no pieces');
    await expect(
      figures.make({
        ...classLook('warrior'),
        wear: [{ ...PIECE_LOOKS.mage_hat!, meshes: ['Crown'] }],
      }),
    ).rejects.toThrow('no Crown');
    const knight = classLook('warrior');
    await expect(
      figures.make({ ...knight, own: { ...knight.own!, headwear: ['Crown'] } }),
    ).rejects.toThrow('kaykit/Knight.glb has no Crown'); // a class's own meshes out of step with its file
  });

  it('counts two rigs the same only joint for joint, by name and order', () => {
    const rig = (...names: string[]) => {
      const k = new Skeleton('rig', 'rig', scene);
      names.forEach((n) => new Bone(n, k));
      return k;
    };
    expect(sameJoints(rig('hips', 'chest'), rig('hips', 'chest'))).toBe(true);
    expect(sameJoints(rig('hips', 'chest'), rig('chest', 'hips'))).toBe(false);
    expect(sameJoints(rig('hips', 'chest'), rig('hips', 'neck'))).toBe(false);
    expect(sameJoints(rig('hips', 'chest'), rig('hips'))).toBe(false);
  });

  it('refuses a look key the client hasn’t mapped', () => {
    expect(() => wearing('warrior', 'golden_crown')).toThrow('no piece look golden_crown');
  });
});

describe('wings worn', () => {
  /** The hinge each half turns on, by side. */
  function hinges(b: Body): { left: TransformNode; right: TransformNode } {
    const nodes = nodesOf(b);
    return { left: nodes.get('Test_Wing_L hinge')!, right: nodes.get('Test_Wing_R hinge')! };
  }

  /** An action's beats from start: its hit 300 ms in, ready at readyAt. */
  const beatsAt = (start: number, readyAt: number): Beats => ({
    start,
    hit: start + 300,
    strikeEnd: start + 300,
    readyAt,
    end: readyAt,
  });

  /** Runs b's frames for ms in gait. */
  function run(b: Body, from: number, ms: number, gait: Gait): number {
    let now = from;
    for (; now < from + ms; now += FRAME_MS) {
      b.update(now, FRAME_MS, gait, true);
    }
    return now;
  }

  /** A half's open angle, in degrees, from its hinge's turn. */
  const openOf = (h: TransformNode, side: 1 | -1) => 90 - (side * h.rotation.y) / RAD;

  /** Where a half's tip lies, in the world, its body facing north from the origin. */
  function tip(b: Body, name: string): Vector3 {
    const m = mesh(b, `${name} worn`)!;
    b.root.computeWorldMatrix(true);
    m.computeWorldMatrix(true);
    const side = name.endsWith('_L') ? 1 : -1;
    return Vector3.TransformCoordinates(new Vector3(side * SPAN, 0.25, 0), m.getWorldMatrix());
  }

  it('hang each half on its own hinge at the back of the chest, where its root lies', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const { left, right } = hinges(b);
    expect(left.parent?.name).toBe('chest');
    expect(right.parent?.name).toBe('chest');
    expect(left.position.asArray()).toEqual([HINGE[0], HINGE[1], HINGE[2]]);
    expect(right.position.asArray()).toEqual([-HINGE[0], HINGE[1], HINGE[2]]);
    expect(mesh(b, 'Test_Wing_L worn')!.parent).toBe(left);
    b.dispose();
  });

  it('hide the cape while worn, the class’s own and an outfit’s, and only then', async () => {
    const winged = await make({
      ...wearing('warrior', 'mage_body'),
      wear: [PIECE_LOOKS.mage_body!, TEST_WINGS],
    });
    expect(mesh(winged, 'Knight_Cape')?.isEnabled()).toBe(false);
    expect(mesh(winged, 'Mage_Cape worn')).toBeUndefined();
    expect(mesh(winged, 'Mage_Body worn')?.isEnabled()).toBe(true);
    const cloaked = await make(wearing('warrior', 'mage_body'));
    expect(mesh(cloaked, 'Mage_Cape worn')?.isEnabled()).toBe(true);
    const own = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    expect(mesh(own, 'Knight_Cape')?.isEnabled()).toBe(false);
    expect(mesh(own, 'Knight_Body')?.isEnabled()).toBe(true);
    const bare = await make(classLook('warrior'));
    expect(mesh(bare, 'Knight_Cape')?.isEnabled()).toBe(true);
    for (const x of [winged, cloaked, own, bare]) x.dispose();
  });

  it('open and beat by the gait: within each state’s swing of its open angle, a beat its length', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const { left, right } = hinges(b);
    let now = 0;
    for (const [gait, state] of [
      [STILL, 'standing'],
      [RUN, 'moving'],
      [SPRINT, 'sprinting'],
      [JUMP, 'air'],
    ] as const) {
      const flap = FLAPS[state];
      now = run(b, now, WING_EASE_MS + FRAME_MS, gait);
      const seen: number[] = [];
      for (let t = 0; t < flap.beatMs; t += FRAME_MS) {
        now = run(b, now, FRAME_MS, gait);
        seen.push(openOf(left, 1));
        expect(openOf(right, -1)).toBeCloseTo(openOf(left, 1), 9); // mirrored
      }
      expect(Math.max(...seen), state).toBeCloseTo(Math.min(90, flap.open + flap.swing), 0); // never past spread flat
      expect(Math.min(...seen), state).toBeCloseTo(flap.open - flap.swing, 0);
    }
    b.dispose();
  });

  it('fold through an attack or a skill, and on a dead body, behind the back', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const { left } = hinges(b);
    let now = run(b, 0, 500, STILL);
    const beats: Beats = beatsAt(now, now + 1050);
    b.act('attack', beats);
    now = run(b, now, WING_EASE_MS + FRAME_MS, STILL);
    expect(openOf(left, 1)).toBeCloseTo(FLAPS.folded.open, 9);
    now = run(b, beats.readyAt, WING_EASE_MS + FRAME_MS, STILL); // the action done, and the wings eased open
    expect(openOf(left, 1)).toBeGreaterThan(FLAPS.standing.open - FLAPS.standing.swing - 0.01);
    b.act('skill', beatsAt(now, now + 1000));
    now = run(b, now, WING_EASE_MS + FRAME_MS, RUN);
    expect(openOf(left, 1)).toBeCloseTo(FLAPS.folded.open, 9);
    now = run(b, now, 1000, STILL);
    b.pose('dead', now);
    now = run(b, now, 2000, STILL);
    expect(openOf(left, 1)).toBeCloseTo(FLAPS.folded.open, 9);
    const chest = nodesOf(b).get('chest')!;
    chest.computeWorldMatrix(true);
    for (const name of ['Test_Wing_L', 'Test_Wing_R']) {
      expect(tip(b, name).z, name).toBeLessThan(chest.getAbsolutePosition().z - 0.3 * SPAN); // folded straight back
    }
    b.dispose();
  });

  it('fold through a block, a flinch, a knockdown and the rise from it, and open again after each', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const { left } = hinges(b);
    let now = run(b, 0, 500, STILL);
    const open = () => openOf(left, 1) > FLAPS.standing.open - FLAPS.standing.swing - 0.01;
    const block = beatsAt(now, now + 1000);
    b.act('block', block);
    now = run(b, now, WING_EASE_MS + FRAME_MS, STILL);
    expect(openOf(left, 1), 'block').toBeCloseTo(FLAPS.folded.open, 9);
    now = run(b, block.readyAt, WING_EASE_MS + FRAME_MS, STILL);
    expect(open(), 'after the block').toBe(true);
    b.pose('flinch', now);
    now = run(b, now, WING_EASE_MS + FRAME_MS, STILL);
    expect(openOf(left, 1), 'flinch').toBeCloseTo(FLAPS.folded.open, 9);
    now = run(b, now, 2000, STILL);
    expect(open(), 'after the flinch').toBe(true);
    b.pose('down', now);
    now = run(b, now, 2000, STILL);
    expect(openOf(left, 1), 'down').toBeCloseTo(FLAPS.folded.open, 9);
    b.pose('rising', now);
    now = run(b, now, FRAME_MS, STILL);
    expect(openOf(left, 1), 'rising').toBeCloseTo(FLAPS.folded.open, 9);
    now = run(b, now, 2000, STILL);
    expect(open(), 'risen').toBe(true);
    b.dispose();
  });

  it('ease from one state to the next over WING_EASE_MS', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const { left } = hinges(b);
    let now = run(b, 0, 400, JUMP);
    b.act('attack', beatsAt(now, now + 1050));
    b.update(now, 0, JUMP, true);
    const half = WING_EASE_MS / 2;
    b.update(now + half, half, JUMP, true);
    expect(
      Math.abs(openOf(left, 1) - (FLAPS.air.open + FLAPS.folded.open) / 2),
    ).toBeLessThanOrEqual(FLAPS.air.swing / 2 + 1e-9); // halfway, beating half as wide
    b.update(now + WING_EASE_MS, half, JUMP, true);
    expect(openOf(left, 1)).toBeCloseTo(FLAPS.folded.open, 9);
    now += WING_EASE_MS;
    b.dispose();
  });

  it('read the state from the gait, folded whenever the body acts', () => {
    expect(wingState(STILL, false)).toBe('standing');
    expect(wingState({ pace: 'walk', onGround: true }, false)).toBe('moving');
    expect(wingState(RUN, false)).toBe('moving');
    expect(wingState(SPRINT, false)).toBe('sprinting');
    expect(wingState({ pace: 'sprint', onGround: false }, false)).toBe('air');
    expect(wingState(SPRINT, true)).toBe('folded');
  });

  it('carry their pose into the afterimage', async () => {
    const b = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    run(b, 0, 700, SPRINT);
    const still = b.afterimage(new StandardMaterial('after', scene));
    const copy = still.root
      .getDescendants(false)
      .find((n) => n.name === 'Test_Wing_L hinge') as TransformNode;
    expect(copy.rotation.y).toBe(hinges(b).left.rotation.y);
    expect(still.meshes.map((m) => m.name)).toContain('Test_Wing_R worn');
    b.dispose();
  });

  it('glow when their piece does: the lit band whatever the light, with a rim', async () => {
    const b = await make({ ...classLook('warrior'), wear: [{ ...TEST_WINGS, glow: true }] });
    const plain = await make({ ...classLook('warrior'), wear: [TEST_WINGS] });
    const glowing = mesh(b, 'Test_Wing_L worn')!.material as NodeMaterial;
    expect(glowing.name).toContain('glow');
    expect(glowing.getBlockByName('rim')).not.toBeNull();
    const flat = mesh(plain, 'Test_Wing_L worn')!.material as NodeMaterial;
    expect(flat.getBlockByName('rim')).toBeNull();
    expect(mesh(b, 'Knight_Body')!.material).toBe(mesh(plain, 'Knight_Body')!.material);
    b.dispose();
    plain.dispose();
  });
});
