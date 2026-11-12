import type Phaser from 'phaser';
import { describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';

import { ANIM, TEX, greyKey } from '../keys';
import { greyArt } from '../scenes/grey-art';

import { MobView } from './mob';
import { PlayerView, classArt } from './player';

describe('class art', () => {
  it.each([
    { cls: 'warrior', stand: TEX.alienBlueStand, walk: ANIM.alienBlueWalk, tint: null },
    { cls: 'bowman', stand: TEX.alienGreenStand, walk: ANIM.alienGreenWalk, tint: null },
    {
      cls: 'beginner',
      stand: greyKey(TEX.alienBlueStand),
      walk: greyKey(ANIM.alienBlueWalk),
      tint: 0xff6640,
    },
    {
      cls: 'thief',
      stand: greyKey(TEX.alienGreenStand),
      walk: greyKey(ANIM.alienGreenWalk),
      tint: 0x9a70d8,
    },
  ])(
    '$cls draws its own art untinted, or the grey copy under its tint',
    ({ cls, stand, walk, tint }) => {
      expect(classArt(cls)).toMatchObject({ stand, walk, tint });
    },
  );

  it('a class from a newer server is magenta', () => {
    expect(classArt('paladin').tint).toBe(0xff00ff);
  });

  it('every class in the data gets textures and animations the preloader makes', () => {
    const grey = greyArt();
    const textures = new Set<string>([...Object.values(TEX), ...grey.textures.map(greyKey)]);
    const anims = new Set<string>([...Object.values(ANIM), ...grey.animations.map((a) => a.key)]);
    for (const cls of [...classes.map((c) => c.id), 'paladin']) {
      const art = classArt(cls);
      for (const t of [art.stand, art.jump, art.climb, art.hit]) {
        expect(textures, cls).toContain(t);
      }
      expect(anims, cls).toContain(art.walk);
      expect(anims, cls).toContain(art.climbAnim);
    }
  });
});

describe('PlayerView', () => {
  const scene = () => {
    const sprite = {
      x: 300,
      y: 608,
      setOrigin: vi.fn(() => sprite),
      setScale: vi.fn(() => sprite),
      setTint: vi.fn(() => sprite),
      clearTint: vi.fn(() => sprite),
      setTexture: vi.fn(() => sprite),
      stop: vi.fn(() => sprite),
      setDepth: vi.fn(() => sprite),
    };
    const label = {
      visible: true,
      width: 40,
      height: 14,
      setOrigin: () => label,
      setDepth: () => label,
    };
    return {
      sprite,
      scene: { add: { sprite: () => sprite, text: () => label } } as unknown as Phaser.Scene,
    };
  };

  it('a new class swaps the art and its tint; the same class changes nothing', () => {
    const { sprite, scene: s } = scene();
    const view = new PlayerView(s, 'beginner', 'Ayla');
    expect(sprite.setTint).toHaveBeenLastCalledWith(0xff6640);
    view.setClass('warrior');
    expect(sprite.setTexture).toHaveBeenLastCalledWith(TEX.alienBlueStand);
    expect(sprite.clearTint).toHaveBeenCalledOnce();
    view.setClass('thief');
    expect(sprite.setTexture).toHaveBeenLastCalledWith(greyKey(TEX.alienGreenStand));
    expect(sprite.setTint).toHaveBeenLastCalledWith(0x9a70d8);
    sprite.setTexture.mockClear();
    view.setClass('thief');
    expect(sprite.setTexture).not.toHaveBeenCalled();
  });

  it("a promotion's new look leaves the name tag where it was", () => {
    const view = new PlayerView(scene().scene, 'beginner', 'Ayla');
    const before = view.nameTag();
    view.setClass('warrior');
    expect(before).not.toBeNull();
    expect(view.nameTag()).toEqual(before);
  });

  it('draws over a boss made after it, name tag and all, so one fighting inside it still shows', () => {
    const made: { kind: string; depth: number }[] = [];
    const add = new Proxy(
      {},
      {
        get: (_t, kind) => () => {
          const o = { kind: String(kind), depth: 0 };
          made.push(o);
          const chain: unknown = new Proxy(() => undefined, {
            get: (_c, key) =>
              key === 'then'
                ? undefined
                : key === 'setDepth'
                  ? (d: number) => ((o.depth = d), chain)
                  : () => chain,
          });
          return chain;
        },
      },
    );
    const s = { add } as unknown as Phaser.Scene;
    new PlayerView(s, 'warrior', 'Ayla');
    new MobView(s, 'king_slime');
    const [sprite, tag, boss] = made;
    expect([sprite?.kind, tag?.kind, boss?.kind]).toEqual(['sprite', 'text', 'sprite']);
    expect(sprite?.depth).toBeGreaterThan(boss?.depth ?? 0);
    expect(tag?.depth).toBeGreaterThan(boss?.depth ?? 0);
  });
});
