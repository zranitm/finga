import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';

import { ANIM, TEX } from '../keys';

import { mobArt } from './mob';

describe('mob art', () => {
  it.each([
    { id: 'green_slime', stand: TEX.slimeStand, move: ANIM.slimeMove, tint: null },
    { id: 'orange_mushroom', stand: TEX.snailStand, move: ANIM.snailMove, tint: 0xe08a3c },
    { id: 'wild_boar', stand: TEX.mouseStand, move: ANIM.mouseMove, tint: 0x8a5a3c },
    { id: 'cave_bat', stand: TEX.flyStand, move: ANIM.flyMove, tint: 0x9a9ab0 },
  ])('$id looks as it did before its look moved into mobs.yaml', ({ id, stand, move, tint }) => {
    expect(mobArt(id)).toMatchObject({ stand, move, tint });
  });

  it('every mob in the data gets textures and its elite mark', () => {
    for (const m of mobs) {
      const art = mobArt(m.id);
      expect(Object.values(TEX), m.id).toContain(art.stand);
      expect(art.elite, m.id).toBe(m.elite);
    }
  });

  it('a mob the data does not know is a magenta slime', () => {
    expect(mobArt('from_a_newer_server')).toMatchObject({
      stand: TEX.slimeStand,
      tint: 0xff00ff,
      elite: false,
    });
  });
});
