import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';

import { actions } from '@/gamedata/actions';
import { classes } from '@/gamedata/classes';
import { equipSlots } from '@/gamedata/equip-slots';
import { expCurve } from '@/gamedata/exp-curve';
import { items } from '@/gamedata/items';
import { mapLinks } from '@/gamedata/map-links';
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

  it('actions equal actions.yaml: the cancel point and energy', () => {
    expect(actions).toEqual(yaml('actions.yaml'));
  });

  it('classes equal classes.yaml, root first, with durations in milliseconds, distances in metres and what a class leaves out as null', () => {
    const raw = yaml('classes.yaml').classes as Record<string, unknown>[];
    expect(classes.map((c) => c.id)).toEqual(raw.map((c) => c.id));
    expect(classes.map((c) => c.parent)).toEqual(raw.map((c) => c.parent ?? null));
    const beginner = classes[0];
    expect(beginner.id).toBe('beginner');
    expect(beginner.base_stats).toEqual(raw[0]!.base_stats);
    expect(beginner.auto_assign).toBeNull();
    expect(beginner.look).toEqual({ model: 'barbarian', tint: null }); // untinted, its cape hidden
    const warrior = classes.find((c) => c.id === 'warrior');
    expect(warrior?.attack).toEqual({
      kind: 'arc',
      damage: 'physical',
      radius: 2.375,
      angle: 120,
      height: 1.75,
      length_ms: 900,
      hit: 0,
      cancel: actions.cancel,
      ready: 0.85, // its own, ahead of actions.yaml's 1
    });
    const bowman = classes.find((c) => c.id === 'bowman');
    expect(bowman?.attack).toEqual({
      kind: 'shot',
      damage: 'physical',
      range: 13.125,
      width: 0.6,
      length_ms: 1050,
      hit: 0,
      cancel: actions.cancel,
      ready: 0.85,
    });
    expect(warrior?.base_stats).toBeNull();
    expect(warrior?.auto_assign).toEqual(raw[1]!.auto_assign);
    expect(warrior?.look).toEqual({ model: 'knight', tint: null });
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
      shape: { kind: 'arc', radius: 2.375, angle: 120, height: 1.75 },
      energy: 2,
      cooldown_ms: 4500,
      power: { base: 1.5, per_point: 0.05 },
      targets: 1,
      length_ms: 1000,
      hit: 0,
      cancel: 0.45, // its own, so an attack cuts its second at 450 ms
      ready: actions.ready,
      cancelable: true,
    });
    // A stage changes only what it names: Heavy Strike's base power and shape, the rest carried on.
    expect(strike.stages[1]).toMatchObject({
      name: 'Heavy Strike',
      shape: { kind: 'arc', radius: 2.875, angle: 120, height: 1.75 },
      energy: 2,
      power: { base: 2.2, per_point: 0.05 },
    });
    expect(strike.stages[2]).toMatchObject({ shape: { radius: 2.875 }, targets: 3 });
    const wind = skills.find((s) => s.id === 'second_wind')!;
    expect(wind.stages[0]).toMatchObject({
      damage: null,
      shape: null,
      cooldown_ms: 0,
      length_ms: 0,
      hit: 0,
      cancel: 0,
      ready: 0,
      cancelable: false,
    });
    expect(wind.stages[1].bonus).toEqual({ hp_regen: { base: 10, per_point: 1 } });
    const heal = skills.find((s) => s.id === 'heal')!;
    expect(heal.stages[0]).toMatchObject({
      cancel: actions.cancel,
      ready: actions.ready,
      cancelable: false,
    });
  });

  it('the stat rules equal stats.yaml', () => {
    expect(stats).toEqual(yaml('stats.yaml'));
    expect(stats.per_point.str.physical_attack).toBe(1.5);
  });

  it('mobs equal mobs.yaml, a mob not marked elite, boss or super armor as none, an untinted one as null and an unscaled one as 1', () => {
    const raw = yaml('mobs.yaml').mobs as {
      elite?: boolean;
      boss?: boolean;
      super_armor?: boolean;
      attacks: unknown[];
      look: { model: string; tint?: number; scale?: number };
    }[];
    expect(mobs.map((m) => ({ ...m, attacks: m.attacks.length }))).toEqual(
      raw.map((m) => ({
        ...m,
        attacks: m.attacks.length,
        elite: m.elite ?? false,
        boss: m.boss ?? false,
        super_armor: m.super_armor ?? false,
        look: {
          ...m.look,
          tint: m.look.tint ?? null,
          scale: m.look.scale ?? 1,
        },
      })),
    );
  });

  it("a mob's attacks are the table's with its changes, times in milliseconds and values its kind doesn't use 0", () => {
    const hop = {
      name: 'hop',
      kind: 'leap',
      windup_ms: 600,
      travel_ms: 400,
      recover_ms: 300,
      cooldown_ms: 2500,
      power: 1,
      reach: 0,
      distance: 2.25,
      spread: 0.375,
      range: 0,
      speed: 0,
      size: 0,
    };
    expect(mobs.find((m) => m.id === 'green_slime')!.attacks).toEqual([hop]);
    expect(mobs.find((m) => m.id === 'king_slime')!.attacks).toEqual([
      { ...hop, distance: 5, spread: 1, cooldown_ms: 4250 },
      {
        name: 'slam',
        kind: 'around',
        windup_ms: 1000,
        travel_ms: 0,
        recover_ms: 600,
        cooldown_ms: 8250,
        power: 1.5,
        reach: 0,
        distance: 0,
        spread: 1.5,
        range: 0,
        speed: 0,
        size: 0,
      },
    ]);
  });

  it('maps carry their kind and art keys, a hunting map its town and level range', () => {
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

  it('maps carry their spawns, portals and world chart place, but not the boss’s area', () => {
    const places = yaml('world.yaml').places as Record<string, [number, number]>;
    for (const m of maps) {
      const raw = yaml(`maps/${m.id}.yaml`) as {
        spawns?: { mob: string; at: [number, number]; radius: number }[];
        portals?: { name: string; at: [number, number]; radius: number; to: { map: string } }[];
      };
      expect(m).toMatchObject({
        id: m.id,
        spawns: (raw.spawns ?? []).map((sp) => ({
          mob: sp.mob,
          x: sp.at[0],
          z: sp.at[1],
          radius: sp.radius,
        })),
        portals: (raw.portals ?? []).map((pt) => ({
          name: pt.name,
          x: pt.at[0],
          z: pt.at[1],
          radius: pt.radius,
          to: pt.to.map,
        })),
        place: places[m.id],
      });
      expect(m).not.toHaveProperty('boss');
    }
  });

  it('links each pair of maps a portal joins once, both ways', () => {
    const joined = new Set(
      maps.flatMap((m) => m.portals.map((p) => [m.id, p.to].sort().join(' '))),
    );
    expect(mapLinks.map((l) => l.join(' '))).toEqual([...joined].sort());
    expect(mapLinks.every(([a, b]) => a < b)).toBe(true);
  });

  it('NPCs equal npcs.yaml, each at the point its map places it', () => {
    const raw = yaml('npcs.yaml');
    expect(npcs.talk_reach).toBe(raw.talk_reach);
    expect(npcs.npcs.map((n) => n.id)).toEqual(
      (raw.npcs as Record<string, unknown>[]).map((n) => n.id),
    );
    const placed = (yaml('maps/town.yaml').npcs as { id: string; at: number[]; yaw: number }[])[0]!;
    const npc = npcs.npcs.find((n) => n.id === placed.id);
    expect(npc).toMatchObject({
      map: 'town',
      x: placed.at[0],
      z: placed.at[1],
      yaw: (placed.yaw * Math.PI) / 180,
      trainer: null,
    });
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
