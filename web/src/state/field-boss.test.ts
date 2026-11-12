import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FieldBossGoneSchema, FieldBossSchema } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { fieldBoss } from './field-boss';

const king = { mobId: 9, type: 'king_slime', leavesInMs: 600000n };

describe('field boss store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts the time left from when the boss came, and a Snapshot with none clears it', () => {
    vi.advanceTimersByTime(1000);
    const came = performance.now();
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, king));
    expect(fieldBoss.getState().boss).toEqual({
      mobId: 9,
      type: 'king_slime',
      leavesAt: came + 600000,
    });
    fieldBoss.getState().setHp({ hp: 3000, maxHp: 4260 });
    fieldBoss.getState().applyFieldBoss(undefined);
    expect(fieldBoss.getState()).toMatchObject({ boss: null, hp: null });
  });

  it('keeps the HP the scene drew for the same boss, and forgets it for a new one', () => {
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, king));
    fieldBoss.getState().setHp({ hp: 3000, maxHp: 4260 });
    vi.advanceTimersByTime(300000);
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, { ...king, leavesInMs: 300000n })); // the same boss, as a rejoin's Snapshot tells it
    expect(fieldBoss.getState().hp).toEqual({ hp: 3000, maxHp: 4260 });
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, { ...king, mobId: 10 }));
    expect(fieldBoss.getState().hp).toBeNull();
  });

  it('a boss gone is the one it names, if it is this map’s', () => {
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, king));
    expect(
      fieldBoss.getState().applyGone(create(FieldBossGoneSchema, { mobId: 8, beaten: false })),
    ).toBeNull();
    expect(fieldBoss.getState().boss?.mobId).toBe(9);
    expect(
      fieldBoss.getState().applyGone(create(FieldBossGoneSchema, { mobId: 9, beaten: false }))
        ?.type,
    ).toBe('king_slime');
    expect(fieldBoss.getState().boss).toBeNull();
  });

  it('sets the HP only when it changes, so the bar redraws only then', () => {
    const seen: unknown[] = [];
    const off = fieldBoss.subscribe((s) => seen.push(s.hp));
    fieldBoss.getState().setHp({ hp: 10, maxHp: 20 });
    fieldBoss.getState().setHp({ hp: 10, maxHp: 20 });
    fieldBoss.getState().setHp(null);
    fieldBoss.getState().setHp(null);
    off();
    expect(seen).toEqual([{ hp: 10, maxHp: 20 }, null]);
  });
});
