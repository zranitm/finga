import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Wardrobe } from '@/net/rest';
import { wardrobe } from '@/state/wardrobe';
import { resetStores } from '@/testing/reset-stores';

import { loadWardrobe } from './load';

const answer: Wardrobe = {
  pieces: [
    { id: 'knight_plate', cause: 'item', item_id: 'i-1', added_at: '2026-11-25T01:00:00Z' },
    { id: 'knight_helm', cause: 'rank', added_at: '2026-11-25T02:00:00Z' },
  ],
};

describe('loading the wardrobe (outfits.md section 7)', () => {
  beforeEach(() => {
    resetStores();
  });

  it('puts the account’s pieces in the store, in place of any before, and an add after it joins them', async () => {
    wardrobe.getState().collect(['old']);
    loadWardrobe({ getWardrobe: () => Promise.resolve(answer) }, vi.fn());
    await vi.waitFor(() =>
      expect(wardrobe.getState().collected).toEqual(new Set(['knight_plate', 'knight_helm'])),
    );
    wardrobe.getState().add('greataxe');
    expect(wardrobe.getState().collected).toEqual(
      new Set(['knight_plate', 'knight_helm', 'greataxe']),
    );
  });

  it('tells of a failed ask, and an add before the list is in waits for the list, which holds it', async () => {
    const onFail = vi.fn();
    loadWardrobe({ getWardrobe: () => Promise.reject(new Error('down')) }, onFail);
    await vi.waitFor(() => expect(onFail).toHaveBeenCalledOnce());
    wardrobe.getState().add('knight_plate');
    expect(wardrobe.getState().collected).toBeNull();
  });

  it('drops an answer or a failure that comes after it’s stopped', async () => {
    const onFail = vi.fn();
    let settle: { ok: (w: Wardrobe) => void; fail: () => void } | null = null;
    const ask = () =>
      new Promise<Wardrobe>((ok, fail) => {
        settle = { ok, fail: () => fail(new Error('down')) };
      });
    loadWardrobe({ getWardrobe: ask }, onFail)();
    settle!.ok(answer);
    loadWardrobe({ getWardrobe: ask }, onFail)();
    settle!.fail();
    await new Promise((r) => setTimeout(r, 0));
    expect([wardrobe.getState().collected, onFail.mock.calls.length]).toEqual([null, 0]);
  });
});
