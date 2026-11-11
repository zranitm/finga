import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';

import { classes } from '@/gamedata/classes';
import { equipSlots } from '@/gamedata/equip-slots';
import { expCurve } from '@/gamedata/exp-curve';
import { items } from '@/gamedata/items';
import { maps } from '@/gamedata/maps';
import { mobs } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';
import { npcs } from '@/gamedata/npcs';
import { quests } from '@/gamedata/quests';
import { skills } from '@/gamedata/skills';
import { stats } from '@/gamedata/stats';

const dataDir = path.resolve(process.cwd(), '..', 'server', 'data');

function yaml(name: string): Record<string, unknown> {
  return parse(readFileSync(path.join(dataDir, name), 'utf8')) as Record<string, unknown>;
}

describe('generated gamedata', () => {
  it('movement equals movement.yaml', () => {
    expect(movement).toEqual(yaml('movement.yaml'));
  });

  it('the EXP curve equals exp_curve.yaml, one entry per level below the cap', () => {
    const raw = yaml('exp_curve.yaml');
    expect(expCurve).toEqual(raw);
    expect(expCurve.exp_to_next).toHaveLength(expCurve.level_cap - 1);
  });

  it('items equal items.yaml with the stack size the server applies', () => {
    const raw = yaml('items.yaml').items as Record<string, unknown>[];
    expect(items).toHaveLength(raw.length);
    items.forEach((item, k) => {
      const source = raw[k]!;
      expect(item).toMatchObject(source);
      expect(item.max_stack).toBe(source.max_stack ?? (item.tab === 'equip' ? 1 : 100));
    });
  });

  it('the equip slots equal items.yaml’s, in order, each naming its slot kind', () => {
    expect(equipSlots).toEqual(yaml('items.yaml').equip_slots);
    expect(equipSlots.map((s) => s.id)).toContain('ring_2');
  });

  it('classes equal classes.yaml, root first, with cooldowns in milliseconds and what a class leaves out as null', () => {
    const raw = yaml('classes.yaml').classes as Record<string, unknown>[];
    expect(classes.map((c) => c.id)).toEqual(raw.map((c) => c.id));
    expect(classes.map((c) => c.parent)).toEqual(raw.map((c) => c.parent ?? null));
    const beginner = classes[0];
    expect(beginner.id).toBe('beginner');
    expect(beginner.base_stats).toEqual(raw[0]!.base_stats);
    expect(beginner.auto_assign).toBeNull();
    expect(beginner.look).toEqual(raw[0]!.look);
    const warrior = classes.find((c) => c.id === 'warrior');
    expect(warrior?.attack).toEqual({
      kind: 'melee',
      damage: 'physical',
      width: 64,
      height: 56,
      cooldown_ms: 600,
    });
    const bowman = classes.find((c) => c.id === 'bowman');
    expect(bowman?.attack).toEqual({
      kind: 'ranged',
      damage: 'physical',
      range: 420,
      cooldown_ms: 800,
    });
    expect(warrior?.base_stats).toBeNull();
    expect(warrior?.auto_assign).toEqual(raw[1]!.auto_assign);
    expect(warrior?.look).toEqual({ sprite: 'alien_blue', tint: null });
    expect(classes.map((c) => c.skills)).toEqual(raw.map((c) => c.skills ?? []));
  });

  it('skills equal skills.yaml, each with the class that teaches it and every stage resolved', () => {
    const raw = yaml('skills.yaml').skills as Record<string, unknown>[];
    expect(skills.map((s) => s.id)).toEqual(raw.map((s) => s.id));
    for (const s of skills) {
      expect(classes.find((c) => c.id === s.class)?.skills, s.id).toContain(s.id);
    }
    const strike = skills.find((s) => s.id === 'strike')!;
    expect(strike.stages[0]).toMatchObject({
      at: 0,
      name: 'Strike',
      damage: 'physical',
      shape: { kind: 'melee', width: 64, height: 56 },
      mp: { base: 4, per_point: 0.2 },
      cooldown_ms: 2000,
      power: { base: 1.5, per_point: 0.05 },
      targets: 1,
    });
    // A stage changes only what it names: Heavy Strike's base power and shape, the rest carried on.
    expect(strike.stages[1]).toMatchObject({
      name: 'Heavy Strike',
      shape: { kind: 'melee', width: 80, height: 56 },
      mp: { base: 4, per_point: 0.2 },
      power: { base: 2.2, per_point: 0.05 },
    });
    expect(strike.stages[2]).toMatchObject({ shape: { width: 80 }, targets: 3 });
    const wind = skills.find((s) => s.id === 'second_wind')!;
    expect(wind.stages[0]).toMatchObject({ damage: null, shape: null, cooldown_ms: 0 });
    expect(wind.stages[1].bonus).toEqual({
      hp_regen: { base: 10, per_point: 1 },
      mp_regen: { base: 10, per_point: 1 },
    });
  });

  it('the stat rules equal stats.yaml', () => {
    expect(stats).toEqual(yaml('stats.yaml'));
    expect(stats.per_point.str.physical_attack).toBe(1.5);
  });

  it('mobs equal mobs.yaml, a mob not marked elite as normal, an untinted one as null and an unscaled one as 1', () => {
    const raw = yaml('mobs.yaml').mobs as {
      elite?: boolean;
      look: { tint?: number; scale?: number };
    }[];
    expect(mobs).toEqual(
      raw.map((m) => ({
        ...m,
        elite: m.elite ?? false,
        look: { ...m.look, tint: m.look.tint ?? null, scale: m.look.scale ?? 1 },
      })),
    );
  });

  it('maps carry their Tiled properties, a hunting map its town and level range', () => {
    expect(maps.find((m) => m.id === 'town')).toMatchObject({
      kind: 'town',
      town: 'town',
      start: true,
      min_level: null,
    });
    expect(maps.find((m) => m.id === 'cave')).toMatchObject({
      kind: 'hunting',
      town: 'town',
      start: false,
      min_level: 10,
      max_level: 14,
    });
  });

  it('NPCs equal npcs.yaml, each at the point its map places it', () => {
    const raw = yaml('npcs.yaml');
    expect(npcs.talk_reach).toBe(raw.talk_reach);
    expect(npcs.npcs.map((n) => n.id)).toEqual(
      (raw.npcs as Record<string, unknown>[]).map((n) => n.id),
    );
    const town = JSON.parse(readFileSync(path.join(dataDir, 'maps', 'town.tmj'), 'utf8')) as {
      layers: { name: string; objects: { name: string; x: number; y: number }[] }[];
    };
    const placed = town.layers.find((l) => l.name === 'npcs')!.objects[0]!;
    const npc = npcs.npcs.find((n) => n.id === placed.name);
    expect(npc).toMatchObject({ map: 'town', x: placed.x, y: placed.y, trainer: null });
  });

  it('quests equal quests.yaml, each objective as kind, target and count', () => {
    const raw = yaml('quests.yaml').quests as Record<string, unknown>[];
    expect(quests.map((q) => q.id)).toEqual(raw.map((q) => q.id));
    const first = quests[0];
    expect(first.objectives[0]).toEqual({ kind: 'kill', target: 'green_slime', count: 10 });
    expect(first).toMatchObject({ quests: [], class: null, repeatable: false });
    expect(quests[1].objectives[0]).toEqual({ kind: 'collect', target: 'slime_gel', count: 5 });
    expect(quests[1].quests).toEqual(['slime_cleanup']);
  });
});
