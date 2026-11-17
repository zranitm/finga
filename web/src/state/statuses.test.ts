import { beforeEach, describe, expect, it } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { statuses } from './statuses';

const stun = { id: 'stun', from: 100, until: 1100 };
const slow = { id: 'slow', from: 100, until: 2100 };

describe('statuses', () => {
  beforeEach(() => {
    resetStores();
  });

  it('keeps each list the same as before, so a frame whose statuses didn’t change doesn’t render again', () => {
    statuses.getState().show({ me: [stun], target: [slow], party: { 'c-robin': [slow] } });
    const was = statuses.getState();
    let sets = 0;
    const off = statuses.subscribe(() => sets++);
    statuses
      .getState()
      .show({ me: [{ ...stun }], target: [{ ...slow }], party: { 'c-robin': [{ ...slow }] } });
    expect(sets).toBe(0);
    statuses
      .getState()
      .show({ me: [{ ...stun }], target: [], party: { 'c-robin': [{ ...slow }] } });
    off();
    const now = statuses.getState();
    expect([sets, now.me === was.me, now.party === was.party, now.target]).toEqual([
      1,
      true,
      true,
      [],
    ]);
  });

  it('takes a restarted status, and a member who left the map', () => {
    statuses.getState().show({ me: [stun], target: [], party: { 'c-robin': [slow] } });
    statuses.getState().show({ me: [{ ...stun, from: 600, until: 1600 }], target: [], party: {} });
    expect([statuses.getState().me, statuses.getState().party]).toEqual([
      [{ id: 'stun', from: 600, until: 1600 }],
      {},
    ]);
  });
});
