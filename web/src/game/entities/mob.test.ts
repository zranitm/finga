import type Phaser from 'phaser';
import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';

import { ANIM, ART_SCALE, TEX, greyKey } from '../keys';
import { greyArt } from '../scenes/grey-art';
import type { MobState } from '../systems/combat-view';
import type { MobAttack, Pose } from '../systems/mob-attacks';

import { MobView, mobArt } from './mob';

describe('mob art', () => {
  it.each([
    { id: 'green_slime', stand: TEX.slimeStand, move: ANIM.slimeMove, tint: null },
    {
      id: 'orange_mushroom',
      stand: greyKey(TEX.snailStand),
      move: greyKey(ANIM.snailMove),
      tint: 0xe08a3c,
    },
    {
      id: 'frost_slime',
      stand: greyKey(TEX.slimeStand),
      move: greyKey(ANIM.slimeMove),
      tint: 0xa0d8f0,
    },
  ])(
    '$id draws its own art untinted, or the grey copy under its tint',
    ({ id, stand, move, tint }) => {
      expect(mobArt(id)).toMatchObject({ stand, move, tint });
    },
  );

  it('every mob in the data gets textures and an animation the preloader makes, and its elite mark', () => {
    const grey = greyArt();
    const textures = new Set<string>([...Object.values(TEX), ...grey.textures.map(greyKey)]);
    const anims = new Set<string>([...Object.values(ANIM), ...grey.animations.map((a) => a.key)]);
    for (const m of [...mobs.map((x) => x.id), 'from_a_newer_server']) {
      const art = mobArt(m);
      expect(textures, m).toContain(art.stand);
      expect(textures, m).toContain(art.dead);
      expect(anims, m).toContain(art.move);
    }
    for (const m of mobs) {
      expect(mobArt(m.id).elite, m.id).toBe(m.elite);
    }
  });

  it("an elite is drawn bigger, a normal mob at the art's size", () => {
    expect(mobArt('mossback_snail')).toMatchObject({ elite: true, scale: 1.5 });
    expect(mobArt('spore_slime')).toMatchObject({ elite: false, scale: 1 });
  });

  it('a boss is drawn three times as big, and only a boss is one', () => {
    expect(mobArt('king_slime')).toMatchObject({ boss: true, elite: false, scale: 3 });
    for (const m of mobs) {
      expect(mobArt(m.id).boss, m.id).toBe(m.boss);
    }
  });

  it('a boss has no HP bar over it, which the boss bar shows at the top; any other mob has one', () => {
    for (const { type, rects } of [
      { type: 'king_slime', rects: 0 },
      { type: 'green_slime', rects: 2 },
    ]) {
      const filled: unknown[] = [];
      const g = {
        clear: () => g,
        setDepth: () => g,
        fillStyle: () => g,
        lineStyle: () => g,
        strokeRect: () => g,
        fillRect: (...r: unknown[]) => (filled.push(r), g),
      };
      const sprite: unknown = new Proxy(() => undefined, {
        get: (_t, key) => (key === 'then' || key === 'filters' ? undefined : () => sprite),
      });
      const scene = { add: { sprite: () => sprite, graphics: () => g } } as unknown as Phaser.Scene;
      new MobView(scene, type).apply({
        type,
        x: 500,
        y: 608,
        facing: 1,
        hp: 3000,
        maxHp: 4260,
        pose: MobPose.UNSPECIFIED,
        poseMs: 0,
      });
      expect(filled, type).toHaveLength(rects);
    }
  });

  it('a mob the data does not know is a magenta slime', () => {
    expect(mobArt('from_a_newer_server')).toMatchObject({
      stand: greyKey(TEX.slimeStand),
      tint: 0xff00ff,
      elite: false,
    });
  });
});

/** A scene whose sprite keeps its place, scale, turn, alpha, fill and glows, and whose graphics keep the names of what they draw since their last clear. */
function recordingScene() {
  const glows: number[] = [];
  const sprite = {
    x: 0,
    y: 0,
    scaleX: 1,
    scaleY: 1,
    rotation: 0,
    alpha: 1,
    tint: null as number | null,
    tintMode: 0,
    setPosition: (x: number, y: number) => ((sprite.x = x), (sprite.y = y), sprite),
    setScale: (x: number, y = x) => ((sprite.scaleX = x), (sprite.scaleY = y), sprite),
    setRotation: (r: number) => ((sprite.rotation = r), sprite),
    setAlpha: (a: number) => ((sprite.alpha = a), sprite),
    setTint: (c: number) => ((sprite.tint = c), sprite),
    setTintMode: (m: number) => ((sprite.tintMode = m), sprite),
    clearTint: () => ((sprite.tint = null), sprite),
    enableFilters: () => sprite,
    filters: { external: { addGlow: (color: number) => glows.push(color) } },
    setOrigin: () => sprite,
    setDepth: () => sprite,
    setFlipX: () => sprite,
    setTexture: () => sprite,
    stop: () => sprite,
    play: () => sprite,
  };
  const layers: string[][] = [];
  const graphics = () => {
    const drawn: string[] = [];
    layers.push(drawn);
    const g: unknown = new Proxy(
      {},
      {
        get: (_t, key) =>
          key === 'then'
            ? undefined
            : () => (key === 'clear' ? drawn.splice(0) : drawn.push(String(key)), g),
      },
    );
    return g;
  };
  const scene = { add: { sprite: () => sprite, graphics } } as unknown as Phaser.Scene;
  return { scene, sprite, glows, fx: () => layers[1] ?? [] };
}

describe('a mob attacking', () => {
  const attackOf = (kind: MobAttack['kind']) => {
    const m = mobs.find((x) => x.attacks[0]?.kind === kind && !x.boss && !x.elite);
    if (!m) {
      throw new Error(`no normal mob attacks first with a ${kind}`);
    }
    return { type: m.id, attack: m.attacks[0] };
  };
  const state = (type: string, hp = 10, pose = MobPose.UNSPECIFIED, poseMs = 0): MobState => ({
    type,
    x: 500,
    y: 608,
    facing: 1,
    hp,
    maxHp: 10,
    pose,
    poseMs,
  });
  const posed = (kind: MobAttack['kind'], pose: Pose, facing: 1 | -1 = 1, hp = 10) => {
    const { type, attack } = attackOf(kind);
    const r = recordingScene();
    const v = new MobView(r.scene, type);
    v.apply(state(type, hp), { swing: { attack, facing, from: 0 }, pose });
    return { ...r, v, type };
  };

  it('crouches and is ringed in its windup, and stands back up on the next frame without one', () => {
    const { sprite, fx, v, type } = posed('leap', { phase: 'windup', progress: 0.5 });
    expect(sprite.scaleY).toBeLessThan(ART_SCALE);
    expect(fx()).toContain('strokeEllipse');
    v.apply(state(type));
    expect(sprite).toMatchObject({
      x: 500,
      y: 608,
      scaleX: ART_SCALE,
      scaleY: ART_SCALE,
      rotation: 0,
    });
    expect(fx()).toEqual([]);
  });

  it('rises through a hop, over where the server has it', () => {
    const { sprite } = posed('leap', { phase: 'travel', progress: 0.5 });
    expect(sprite.x).toBe(500);
    expect(sprite.y).toBeLessThan(608);
  });

  it.each([1, -1] as const)(
    'leans forward through a charge facing %d, with a trail behind',
    (facing) => {
      const { sprite, fx } = posed('rush', { phase: 'travel', progress: 0.5 }, facing);
      expect(Math.sign(sprite.rotation)).toBe(facing);
      expect(fx()).toContain('fillCircle');
    },
  );

  it.each([1, -1] as const)(
    'lunges the way it faces, %d, with an arc as a bite lands',
    (facing) => {
      const { sprite, fx } = posed('front', { phase: 'impact', progress: 0 }, facing);
      expect(Math.sign(sprite.x - 500)).toBe(facing);
      expect(fx()).toContain('arc');
    },
  );

  it('draws no attack once dead', () => {
    const { sprite, fx } = posed('front', { phase: 'impact', progress: 0 }, 1, 0);
    expect(sprite.x).toBe(500);
    expect(fx()).toEqual([]);
  });
});

describe('a mob taking blows', () => {
  const normal = 'green_slime';
  const drawn = (pose: MobPose, poseMs: number, type = normal, facing: 1 | -1 = 1) => {
    const r = recordingScene();
    new MobView(r.scene, type).apply({
      type,
      x: 500,
      y: 608,
      facing,
      hp: 10,
      maxHp: 10,
      pose,
      poseMs,
    });
    return r;
  };

  it('flashes white as it flinches, then shows its own colour, jolted back from where it faces', () => {
    const early = drawn(MobPose.FLINCH, 0);
    expect(early.sprite).toMatchObject({ tint: 0xffffff, tintMode: 1 });
    expect(early.sprite.x).toBeLessThan(500);
    expect(drawn(MobPose.FLINCH, 0, normal, -1).sprite.x).toBeGreaterThan(500);
    const late = drawn(MobPose.FLINCH, 200);
    expect(late.sprite).toMatchObject({ tint: null, tintMode: 0, x: 500 });
  });

  it('lies on its side, raised to the ground, in a puff of dust that settles', () => {
    const { sprite, fx } = drawn(MobPose.DOWN, 0);
    expect(Math.abs(sprite.rotation)).toBeCloseTo(Math.PI / 2);
    expect(sprite.y).toBeLessThan(608);
    expect(fx()).toContain('fillEllipse');
    expect(drawn(MobPose.DOWN, 400).fx()).toEqual([]);
  });

  it('blinks as it rises', () => {
    expect(drawn(MobPose.RISING, 0).sprite.alpha).toBeLessThan(1);
    expect(drawn(MobPose.RISING, 150).sprite.alpha).toBe(1);
  });

  it('stands as usual otherwise', () => {
    expect(drawn(MobPose.UNSPECIFIED, 0).sprite).toMatchObject({
      x: 500,
      y: 608,
      rotation: 0,
      alpha: 1,
      tint: null,
    });
  });

  it('an elite or a boss carries a warm outline, and only they do', () => {
    for (const m of mobs) {
      expect(recordingScene().glows, m.id).toEqual([]);
      const r = recordingScene();
      new MobView(r.scene, m.id);
      expect(r.glows.length, m.id).toBe(m.elite || m.boss ? 1 : 0);
    }
  });
});
