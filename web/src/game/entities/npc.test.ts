import { describe, expect, it } from 'vitest';

import { npcs } from '@/gamedata/npcs';

import { npcArt } from './npc';

describe('npcArt', () => {
  it.each(npcs.npcs.map((n) => ({ id: n.id, look: n.look })))(
    '$id has art of its own for its look $look',
    ({ look }) => {
      expect(npcArt(look)).not.toEqual(npcArt('a look no NPC has'));
    },
  );
});
