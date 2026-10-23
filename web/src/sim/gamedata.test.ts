import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';

import { classes } from '@/gamedata/classes';
import { expCurve } from '@/gamedata/exp-curve';
import { items } from '@/gamedata/items';
import { mobs } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';

// The generated gamedata equals the YAML it came from: the server's data,
// never typed by hand on the client.
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

  it('classes equal classes.yaml with cooldowns in milliseconds', () => {
    const raw = yaml('classes.yaml').classes as Record<string, unknown>[];
    expect(classes.map((c) => c.id)).toEqual(raw.map((c) => c.id));
    const warrior = classes.find((c) => c.id === 'warrior');
    expect(warrior?.attack).toEqual({ kind: 'melee', width: 64, height: 56, cooldown_ms: 600 });
    const bowman = classes.find((c) => c.id === 'bowman');
    expect(bowman?.attack).toEqual({ kind: 'ranged', range: 420, cooldown_ms: 800 });
    expect(warrior?.base_stats).toEqual(raw[0]!.base_stats);
  });

  it('mobs equal mobs.yaml', () => {
    expect(mobs).toEqual(yaml('mobs.yaml').mobs);
  });
});
