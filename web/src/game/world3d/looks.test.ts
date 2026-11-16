import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { npcs } from '@/gamedata/npcs';

import { classLook, mobLook, npcLook } from './looks';

describe('looks', () => {
  it('give every class, mob and NPC in the data a look', () => {
    for (const c of classes) {
      expect(classLook(c.id).file).toMatch(/\.glb$/);
    }
    for (const m of mobs) {
      expect(mobLook(m.id).file).toMatch(/\.glb$/);
    }
    for (const n of npcs.npcs) {
      expect(npcLook(n.look).file).toMatch(/\.glb$/);
    }
  });

  it('refuse a key the data doesn’t have rather than draw something else', () => {
    expect(() => classLook('pirate')).toThrow('no class pirate');
    expect(() => mobLook('dragon')).toThrow('no mob dragon');
    expect(() => npcLook('merchant')).toThrow('no model merchant');
    expect(() => npcLook('trainer_pirate')).toThrow('no class pirate');
  });
});
