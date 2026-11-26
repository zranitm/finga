import { describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { npcs } from '@/gamedata/npcs';
import { outfits } from '@/gamedata/outfits';

import { classLook, drawnAs, mobLook, npcLook, playerLook } from './looks';
import { MODELS } from './model-looks';
import { PIECE_LOOKS } from './outfit-looks';

describe('looks', () => {
  it('mark every model of the pack’s Flying group as a flyer, drawn at its cylinder’s middle', () => {
    const flying = Object.entries(MODELS).filter(([, m]) => m.file.startsWith('quaternius/Flying_'));
    expect(flying.length).toBeGreaterThan(10);
    for (const [key, m] of flying) {
      expect([key, m.flies]).toEqual([key, true]);
    }
  });

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
  });

  it('wear the bare head of a hooded class under headwear, and keep its hood under none or a bare head', () => {
    const bare = { slot: 'face', file: 'kaykit/Rogue.glb', meshes: ['Rogue_Head'] };
    const wear = (cls: string, looks: string[], bareHead = false) =>
      classLook(cls, { looks, bareHead }).wear;
    expect(wear('bowman', ['knight_helmet'])).toEqual([PIECE_LOOKS.knight_helmet, bare]);
    expect(wear('bowman', ['mage_hat', 'mage_body'])).toEqual([
      PIECE_LOOKS.mage_hat,
      PIECE_LOOKS.mage_body,
      bare,
    ]);
    expect(wear('bowman', ['mage_body'])).toEqual([PIECE_LOOKS.mage_body]);
    expect(wear('bowman', ['knight_helmet'], true)).toEqual([]);
    expect(wear('thief', ['knight_helmet'])).toEqual([PIECE_LOOKS.knight_helmet]); // a head with no hood
  });

  it('draw the class’s own for a piece this client’s data lacks, as an older client sees a newer one, and say so once', () => {
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const none = { outfit: '', headwear: '', face: '', weapon: '', wings: '', hideHeadwear: false };
    const newer = { ...none, outfit: 'mage_robe', face: 'golden_mask' };
    expect(playerLook('thief', newer)).toEqual(
      classLook('thief', { looks: ['mage_body'], bareHead: false }),
    );
    expect(playerLook('bowman', newer)).toEqual(
      classLook('bowman', { looks: ['mage_body'], bareHead: false }),
    );
    expect(said.mock.calls).toEqual([
      ["finga: no piece golden_mask in this client's data; drawn as the class's own"],
    ]);
    said.mockRestore();
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

  it('fit a piece only to bodies a class is drawn as, so a misnamed file can’t leave it unfitted', () => {
    const bodies = new Set(classes.map((c) => classLook(c.id).file));
    const fitted = Object.values(PIECE_LOOKS).flatMap((p) => Object.keys(p.fit ?? {}));
    expect(fitted.length).toBeGreaterThan(0);
    expect(fitted.filter((f) => !bodies.has(f))).toEqual([]);
  });

  it('give every wardrobe piece in the data a look of its slot', () => {
    expect(outfits.pieces.length).toBeGreaterThan(0);
    for (const p of outfits.pieces) {
      expect(PIECE_LOOKS[p.look]?.slot, p.id).toBe(p.slot);
    }
  });

  it('refuse a key the client’s own data doesn’t have rather than draw something else', () => {
    expect(() => classLook('pirate')).toThrow('no class pirate');
    expect(() => npcLook('merchant')).toThrow('no model merchant');
    expect(() => npcLook('trainer_pirate')).toThrow('no class pirate');
  });

  it('draw a class or mob type the server sends and this client’s data lacks as a Beginner or the smallest mob, and say so once', () => {
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const robe = {
      outfit: 'mage_robe',
      headwear: '',
      face: '',
      weapon: '',
      wings: '',
      hideHeadwear: false,
    };
    expect(playerLook('pirate', undefined)).toEqual(classLook('beginner'));
    expect(playerLook('pirate', robe)).toEqual(
      classLook('beginner', { looks: ['mage_body'], bareHead: false }),
    );
    const smallest = [...mobs].sort((a, b) => a.radius - b.radius)[0]!;
    expect(mobLook('dragon')).toEqual(mobLook(smallest.id));
    mobLook('dragon');
    expect(said.mock.calls).toEqual([
      ["finga: no class pirate in this client's data; drawn as a Beginner"],
      [`finga: no mob dragon in this client's data; drawn as a ${smallest.name}`],
    ]);
    said.mockRestore();
  });
});
