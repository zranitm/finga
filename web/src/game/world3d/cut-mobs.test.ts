import { describe, expect, it } from 'vitest';

import type { Mark } from '../systems/aim';

import { CUT_MOBS, cutMobs } from './cut-mobs';

const mob = (id: number, x: number, z: number, hp = 10): Mark => ({
  id,
  x,
  z,
  radius: 0.5,
  hp,
  inView: true,
  inFrame: true,
});
const none = () => false;
const ids = (ms: readonly Mark[]) => ms.map((m) => m.id);

describe('the mobs the place opens round', () => {
  it('takes those within 4 m, nearest first, a tie to the lower id, and none further', () => {
    const mobs = [mob(1, 3, 0), mob(2, 0, 1), mob(3, 0, -3), mob(4, 4.5, 0)];
    expect(ids(cutMobs(0, 0, null, none, mobs))).toEqual([2, 1, 3]);
  });

  it('takes the locked mob first, however far', () => {
    const mobs = [mob(1, 1, 0), mob(2, 20, 0)];
    expect(ids(cutMobs(0, 0, 2, none, mobs))).toEqual([2, 1]);
  });

  it('takes the mobs attacking within 8 m before the idle ones nearer', () => {
    const mobs = [mob(1, 1, 0), mob(2, 7, 0), mob(3, 9, 0)];
    const attacking = (id: number) => id !== 1;
    expect(ids(cutMobs(0, 0, null, attacking, mobs))).toEqual([2, 1]);
  });

  it(`takes ${CUT_MOBS} at most, and no dead mob`, () => {
    const mobs = [mob(1, 1, 0, 0), ...[2, 3, 4, 5, 6].map((id) => mob(id, id / 2, 0))];
    expect(ids(cutMobs(0, 0, 1, none, mobs))).toEqual([2, 3, 4, 5]);
  });
});
