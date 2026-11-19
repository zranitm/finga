import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FieldBossGoneSchema, FieldBossSchema } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { fieldBoss } from './field-boss';

const king = { mobId: 9, type: 'king_slime' };
const beaten = { mobId: 9, type: 'king_slime', backInMs: 180000n };

describe('field boss store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('takes a Snapshot’s boss up, or beaten with its time back counted from when it came, or neither', () => {
    fieldBoss.getState().applySnapshot(create(FieldBossSchema, king), undefined);
    expect(fieldBoss.getState()).toMatchObject({ boss: king, down: null });
    vi.advanceTimersByTime(1000);
    const came = performance.now();
    fieldBoss.getState().applySnapshot(undefined, create(FieldBossGoneSchema, beaten));
    expect(fieldBoss.getState()).toMatchObject({
      boss: null,
      down: { type: 'king_slime', backAt: came + 180000 },
    });
    fieldBoss.getState().applySnapshot(undefined, undefined);
    expect(fieldBoss.getState()).toMatchObject({ boss: null, down: null, hp: null });
  });

  it('keeps the HP the scene drew for the same boss, and forgets it for a new one', () => {
    fieldBoss.getState().applySnapshot(create(FieldBossSchema, king), undefined);
    fieldBoss.getState().setHp({ hp: 3000, maxHp: 4260 });
    fieldBoss.getState().applySnapshot(create(FieldBossSchema, king), undefined); // the same boss, as a rejoin's Snapshot tells it
    expect(fieldBoss.getState().hp).toEqual({ hp: 3000, maxHp: 4260 });
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, { ...king, mobId: 10 }));
    expect(fieldBoss.getState().hp).toBeNull();
  });

  it('a boss beaten counts down to when it’s back, and its return clears that', () => {
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, king));
    fieldBoss.getState().setHp({ hp: 3000, maxHp: 4260 });
    const fell = performance.now();
    fieldBoss.getState().applyGone(create(FieldBossGoneSchema, beaten));
    expect(fieldBoss.getState()).toMatchObject({
      boss: null,
      hp: null,
      down: { type: 'king_slime', backAt: fell + 180000 },
    });
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, { ...king, mobId: 10 }));
    expect(fieldBoss.getState()).toMatchObject({ boss: { mobId: 10 }, down: null });
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
