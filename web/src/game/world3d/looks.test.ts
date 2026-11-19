import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { npcs } from '@/gamedata/npcs';
import { outfits } from '@/gamedata/outfits';

import { classLook, drawnAs, mobLook, npcLook, playerLook } from './looks';
import { PIECE_LOOKS } from './outfit-looks';

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

  it('draw a player in each piece their look wears, by its look key, and as their class alone when it wears none', () => {
    const none = { outfit: '', headwear: '', face: '', weapon: '', wings: '', hideHeadwear: false };
    expect(playerLook('thief', undefined)).toEqual(classLook('thief'));
    expect(playerLook('thief', none)).toEqual(classLook('thief'));
    const worn = { ...none, outfit: 'mage_robe', weapon: 'greataxe' };
    expect(playerLook('thief', worn)).toEqual(
      classLook('thief', { looks: ['mage_body', 'two_handed_axe'], bareHead: false }),
    );
    expect(playerLook('thief', { ...none, hideHeadwear: true })).toEqual(
      classLook('thief', { looks: [], bareHead: true }),
    );
    expect(() => playerLook('thief', { ...none, face: 'golden_mask' })).toThrow(
      'no piece golden_mask',
    );
  });

  it('name a body by its class and what its look shows, so either changing redraws it', () => {
    const none = { outfit: '', headwear: '', face: '', weapon: '', wings: '', hideHeadwear: false };
    expect(drawnAs('thief', undefined)).toBe('thief');
    expect(drawnAs('thief', none)).toBe('thief');
    expect(drawnAs('thief', { ...none, headwear: 'horned_hat', hideHeadwear: true })).toBe(
      'thief |horned_hat||||bare',
    );
    expect(drawnAs('thief', { ...none, outfit: 'mage_robe' })).not.toBe(
      drawnAs('thief', { ...none, weapon: 'mage_robe' }),
    ); // each slot keeps its place
    expect(drawnAs('bowman', { ...none, outfit: 'mage_robe' })).not.toBe(
      drawnAs('thief', { ...none, outfit: 'mage_robe' }),
    );
  });

  it('give every wardrobe piece in the data a look of its slot', () => {
    expect(outfits.pieces.length).toBeGreaterThan(0);
    for (const p of outfits.pieces) {
      expect(PIECE_LOOKS[p.look]?.slot, p.id).toBe(p.slot);
    }
  });

  it('refuse a key the data doesn’t have rather than draw something else', () => {
    expect(() => classLook('pirate')).toThrow('no class pirate');
    expect(() => mobLook('dragon')).toThrow('no mob dragon');
    expect(() => npcLook('merchant')).toThrow('no model merchant');
    expect(() => npcLook('trainer_pirate')).toThrow('no class pirate');
  });
});
