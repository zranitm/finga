import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';

import { ANIM, TEX, greyKey } from '../keys';
import { greyMobArt } from '../scenes/grey-art';

import { mobArt } from './mob';

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
    const grey = greyMobArt();
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

  it('a mob the data does not know is a magenta slime', () => {
    expect(mobArt('from_a_newer_server')).toMatchObject({
      stand: greyKey(TEX.slimeStand),
      tint: 0xff00ff,
      elite: false,
    });
  });
});
