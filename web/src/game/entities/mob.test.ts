import type Phaser from 'phaser';
import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';

import { ANIM, TEX, greyKey } from '../keys';
import { greyArt } from '../scenes/grey-art';

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
        get: (_t, key) => (key === 'then' ? undefined : () => sprite),
      });
      const scene = { add: { sprite: () => sprite, graphics: () => g } } as unknown as Phaser.Scene;
      new MobView(scene, type).apply({ type, x: 500, y: 608, facing: 1, hp: 3000, maxHp: 4260 }, 0);
      expect(filled, type).toHaveLength(rects);
    }
  });

  it("holds its animation's frame for a hit-stop, keeping its place, then walks on", () => {
    const calls: string[] = [];
    const anims = { pause: () => calls.push('pause'), resume: () => calls.push('resume') };
    const sprite: unknown = new Proxy(() => undefined, {
      get: (_t, key) =>
        key === 'then'
          ? undefined
          : key === 'anims'
            ? anims
            : (...a: unknown[]) => (calls.push(`${String(key)} ${a.join(' ')}`.trim()), sprite),
    });
    const g: unknown = new Proxy(() => undefined, {
      get: (_t, key) => (key === 'then' ? undefined : () => g),
    });
    const scene = { add: { sprite: () => sprite, graphics: () => g } } as unknown as Phaser.Scene;
    const v = new MobView(scene, 'green_slime');
    const at = (x: number) => ({
      type: 'green_slime',
      x,
      y: 608,
      facing: 1 as const,
      hp: 30,
      maxHp: 34,
    });
    v.apply(at(500), 1000);
    v.hold(1050);
    calls.length = 0;
    v.apply(at(502), 1049);
    expect(calls).toEqual(['setPosition 502 608', 'setFlipX true', 'pause']);
    calls.length = 0;
    v.apply(at(504), 1050);
    expect(calls.slice(0, 4)).toEqual([
      'setPosition 504 608',
      'setFlipX true',
      'resume',
      `play ${mobArt('green_slime').move} true`,
    ]);
  });

  it('a mob the data does not know is a magenta slime', () => {
    expect(mobArt('from_a_newer_server')).toMatchObject({
      stand: greyKey(TEX.slimeStand),
      tint: 0xff00ff,
      elite: false,
    });
  });
});
