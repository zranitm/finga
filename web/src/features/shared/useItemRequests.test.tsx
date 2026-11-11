import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { ERR } from '@/protocol/errors';
import { ui } from '@/state/ui';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { useItemRequests } from './useItemRequests';

const refused = (code: Parameters<typeof refusal>[0]) => () => Promise.reject(refusal(code));

/** Sends, then lets the request settle. */
async function sent(run: () => void): Promise<void> {
  act(run);
  await act(() => new Promise((resolve) => setTimeout(resolve, 0)));
}

describe('useItemRequests', () => {
  beforeEach(() => {
    resetStores();
  });

  it('a refusal shows in its own window until a request in another window replaces it', async () => {
    const inventory = renderHook(() => useItemRequests('inventory'));
    const character = renderHook(() => useItemRequests('character'));
    await sent(() => inventory.result.current.run('i-1', refused(ERR.LEVEL_TOO_LOW)));
    expect(inventory.result.current.notice).toBe('Your level is too low for that item.');
    expect(character.result.current.notice).toBeNull();

    await sent(() => character.result.current.run('weapon', refused(ERR.EQUIP_TAB_FULL)));
    expect(inventory.result.current.notice).toBeNull();
    expect(character.result.current.notice).toBe('Your Equip tab is full.');

    await sent(() => inventory.result.current.run('i-2', () => Promise.resolve()));
    expect(character.result.current.notice).toBeNull(); // a newer request makes it stale
  });

  it('a window closed with a notice opens again without it', async () => {
    act(() => ui.getState().openWindow('inventory'));
    const inventory = renderHook(() => useItemRequests('inventory'));
    await sent(() => inventory.result.current.run('i-1', refused(ERR.ITEM_WORN)));
    expect(inventory.result.current.notice).toBe('Unequip it first.');
    act(() => ui.getState().toggleWindow('inventory'));
    expect(inventory.result.current.notice).toBeNull();
  });
});
