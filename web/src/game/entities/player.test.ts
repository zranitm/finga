import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';

import { ANIM, TEX, greyKey } from '../keys';
import { greyArt } from '../scenes/grey-art';

import { classArt } from './player';

describe('class art', () => {
  it.each([
    { cls: 'warrior', stand: TEX.alienBlueStand, walk: ANIM.alienBlueWalk, tint: null },
    { cls: 'bowman', stand: TEX.alienGreenStand, walk: ANIM.alienGreenWalk, tint: null },
    {
      cls: 'beginner',
      stand: greyKey(TEX.alienBlueStand),
      walk: greyKey(ANIM.alienBlueWalk),
      tint: 0xd8c8a8,
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
