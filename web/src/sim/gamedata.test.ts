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

  it('items equal items.yaml with the stack size the server applies, then an item for each wardrobe piece as the server makes it', () => {
    const raw = yaml('items.yaml').items as Record<string, unknown>[];
    const pieces = yaml('outfits.yaml').pieces as { id: string; name: string; stars: number }[];
    expect(items).toHaveLength(raw.length + pieces.length);
    raw.forEach((source, k) => {
      const item = items[k]!;
      expect(item).toMatchObject(source);
      expect(item.max_stack).toBe(source.max_stack ?? (item.tab === 'equip' ? 1 : 100));
    });
    expect(items.slice(raw.length)).toEqual(
      pieces.map((p) => ({
        id: `piece.${p.id}`,
        name: p.name,
        tab: 'etc',
        stars: p.stars,
        description: `Adds ${p.name} to your wardrobe.`,
        max_stack: 100,
      })),
    );
  });

  it('the equip slots equal items.yaml’s, in order, each naming its slot kind', () => {
    expect(equipSlots).toEqual(yaml('items.yaml').equip_slots);
    expect(equipSlots.map((s) => s.id)).toContain('ring_2');
  });

  it('actions equal actions.yaml: the cancel point, energy, a block’s arc and time in combat', () => {
    const { combat, ...rest } = yaml('actions.yaml');
    expect(combat).toBe('5s');
    expect(actions).toEqual({ ...rest, combat_ms: 5000 });
    expect([actions.energy.per_block, actions.block.arc]).toEqual([2, 180]);
  });

  it('a block skill carries its guard’s time, and its counter its window after a block (C21)', () => {
    const guard = skills.find((s) => s.id === 'guard')!;
    expect([
      guard.effect,
      guard.granted,
      guard.stages[0].block_ms,
      guard.stages[0].length_ms,
    ]).toEqual(['block', true, 600, 600]);
    expect(skills.find((s) => s.id === 'strike')!.stages[0].block_ms).toBe(0);
    const chain = classes.find((c) => c.id === 'beginner')!.chains.find((c) => c.root === 'guard')!;
    expect(chain.follow).toEqual([
      {
        skill: 'strike',
        when: {
          after_ms: null,
          controlled: null,
          far: null,
          dodged_ms: null,
          blocked_ms: 1500,
          controlled_self: false,
        },
      },
    ]);
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

  it('each active skill that takes points has a default that changes nothing and an extra resolved as the server resolves it', () => {
    const golden = JSON.parse(
      readFileSync(
        path.resolve(process.cwd(), '..', 'server', 'testdata', 'traits', 'extras.json'),
        'utf8',
      ),
    ) as unknown;
    const nonZero = (o: object) =>
      Object.fromEntries(
        Object.entries(o).filter(([, v]) =>
          typeof v === 'number' ? v !== 0 : JSON.stringify(v) !== '{"base":0,"per_point":0}',
        ),
      );
    const extras: Record<string, unknown> = {};
    for (const s of skills) {
      const takes = s.kind === 'active' && !s.granted;
      expect(s.traits, s.id).toHaveLength(takes ? 2 : 0);
      if (!takes) continue;
      const [first, extra] = s.traits;
      expect(first.stage, s.id).toBeNull();
      const stage = extra.stage;
      extras[s.id] = {
        [extra.id]: {
          ...stage,
          shape: stage.shape && nonZero(stage.shape),
          bonus: nonZero(stage.bonus),
        },
      };
    }
    expect(extras).toEqual(golden);
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

  it("a mob's attacks are the table's with its changes, times in milliseconds, values its kind doesn't use 0, a status in milliseconds or none, and whether a block can't stop it", () => {
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
      unblockable: false,
      status: null,
    };
    expect(mobs.find((m) => m.id === 'green_slime')!.attacks).toEqual([hop]);
    expect(mobs.find((m) => m.id === 'king_slime')!.attacks).toEqual([
      { ...hop, distance: 5, spread: 1, cooldown_ms: 4750 },
      {
        name: 'slam',
        kind: 'around',
        windup_ms: 1000,
        travel_ms: 0,
        recover_ms: 600,
        cooldown_ms: 9250,
        power: 1.5,
        reach: 0,
        distance: 0,
        spread: 1.5,
        range: 0,
        speed: 0,
        size: 0,
        unblockable: true, // a boss's slam: red
        status: { status: 'stun', time_ms: 1000, power: 0 },
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

  it('maps carry their spawns, portals, boss spot and world chart place, a match’s map none, and no areas', () => {
    const places = yaml('world.yaml').places as Record<string, [number, number]>;
    for (const m of maps.filter((x) => x.kind !== 'region')) {
      const raw = yaml(`maps/${m.id}.yaml`) as {
        spawns?: { mob: string; at: [number, number]; radius: number }[];
        portals?: { name: string; at: [number, number]; radius: number; to: { map: string } }[];
        boss?: { mob: string; at: [number, number]; radius: number };
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
        boss: raw.boss
          ? { mob: raw.boss.mob, x: raw.boss.at[0], z: raw.boss.at[1], radius: raw.boss.radius }
          : null,
        place: places[m.id] ?? null,
        areas: [],
      });
    }
  });

  it('a region is a map of its plan: its own town, its camps as spawns and gates as portals, its areas with their corners, levels and bosses, no boss of its own, on no chart', () => {
    const regions = maps.filter((m) => m.kind === 'region');
    expect(regions.map((m) => m.id)).toContain('test_region');
    for (const m of regions) {
      const raw = yaml(`regions/${m.id}.yaml`) as {
        name: string;
        look: { sky: string };
        camps?: { mob: string; at: [number, number]; radius: number }[];
        gates?: { name: string; at: [number, number]; radius: number; to: { region: string } }[];
        areas: {
          id: string;
          name: string;
          kind: string;
          levels?: [number, number];
          area: [number, number][];
          boss?: { mob: string; at: [number, number]; radius: number };
        }[];
      };
      expect(m).toEqual({
        id: m.id,
        name: raw.name,
        kind: 'region',
        town: m.id,
        start: false,
        ground: null,
        sky: raw.look.sky,
        min_level: null,
        max_level: null,
        spawns: (raw.camps ?? []).map((c) => ({
          mob: c.mob,
          x: c.at[0],
          z: c.at[1],
          radius: c.radius,
        })),
        portals: (raw.gates ?? []).map((g) => ({
          name: g.name,
          x: g.at[0],
          z: g.at[1],
          radius: g.radius,
          to: g.to.region,
        })),
        boss: null,
        place: null,
        areas: raw.areas.map((a) => ({
          id: a.id,
          name: a.name,
          kind: a.kind,
          levels: a.levels ?? null,
          area: a.area,
          boss: a.boss
            ? { mob: a.boss.mob, x: a.boss.at[0], z: a.boss.at[1], radius: a.boss.radius }
            : null,
        })),
        points: [],
        bases: [],
      });
    }
    expect(maps.find((m) => m.id === 'test_region')!.areas.map((a) => [a.id, a.levels])).toEqual([
      ['town', null],
      ['west', [1, 4]],
      ['south', [10, 14]],
      ['north', [15, 18]],
      ['woods', [19, 20]],
    ]);
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
