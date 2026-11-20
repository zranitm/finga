import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { PvpProfile, PvpStanding } from '@/net/rest';
import { arenaBuild } from '@/state/arena-build';
import { pvpProfile } from '@/state/pvp-profile';

import { watchPvpProfile } from './watchProfile';

function standing(shown: number): PvpStanding {
  return {
    mode: 'duel',
    shown,
    rank: 'bronze',
    placement_left: 0,
    wins: 5,
    losses: 0,
    draws: 0,
    place: 3,
  };
}

function profile(shown: number, bar: string[] = ['strike']): PvpProfile {
  return { standings: [standing(shown)], build: { skill_bar: bar, traits: {} } };
}

/** A rest whose reads answer when the test says, in any order, and a socket whose handlers the test calls. */
function harness() {
  const reads: { resolve: (p: PvpProfile) => void; reject: (e: Error) => void }[] = [];
  const getPvpProfile = vi.fn(
    () =>
      new Promise<PvpProfile>((resolve, reject) => {
        reads.push({ resolve, reject });
      }),
  );
  const handlers = new Map<string, () => void>();
  const offs: string[] = [];
  const socket = {
    on: (t: string, h: () => void) => {
      handlers.set(t, h);
      return () => offs.push(t);
    },
  } as unknown as Parameters<typeof watchPvpProfile>[1];
  const stop = watchPvpProfile({ getPvpProfile }, socket, 'c-ayla');
  const settle = () => new Promise((r) => setTimeout(r, 0));
  return { reads, getPvpProfile, handlers, offs, stop, settle };
}

describe('the PvP profile', () => {
  beforeEach(() => {
    pvpProfile.reset();
    arenaBuild.reset();
  });

  it('is read at once, its standings for the cards and its build for the Arena tab', async () => {
    const h = harness();
    expect(h.getPvpProfile).toHaveBeenCalledWith('c-ayla');
    h.reads[0]!.resolve(profile(1640, ['strike', 'war_cry']));
    await h.settle();
    expect(pvpProfile.getState().standings).toEqual([standing(1640)]);
    expect(arenaBuild.getState().bar).toEqual(['strike', 'war_cry', '', '', '', '', '', '']);
    h.stop();
  });

  it('is read again after a match ends, a promotion or an ask, the newest read landing alone', async () => {
    const h = harness();
    h.handlers.get('matchEnded')!();
    h.handlers.get('promoted')!();
    pvpProfile.getState().ask();
    expect(h.getPvpProfile).toHaveBeenCalledTimes(4);
    h.reads[3]!.resolve(profile(1700));
    h.reads[0]!.resolve(profile(1000)); // the first, landing last
    await h.settle();
    expect(pvpProfile.getState().standings).toEqual([standing(1700)]);
    h.stop();
  });

  it('a read that fails keeps the standings it had', async () => {
    const h = harness();
    h.reads[0]!.resolve(profile(1640));
    await h.settle();
    h.handlers.get('matchEnded')!();
    h.reads[1]!.reject(new Error('down'));
    await h.settle();
    expect(pvpProfile.getState().standings).toEqual([standing(1640)]);
    h.stop();
  });

  it('stopping drops a read under way and every listener', async () => {
    const h = harness();
    h.stop();
    h.reads[0]!.resolve(profile(1640));
    await h.settle();
    expect(pvpProfile.getState().standings).toBeNull();
    expect(h.offs.sort()).toEqual(['matchEnded', 'promoted']);
    pvpProfile.getState().ask();
    expect(h.getPvpProfile).toHaveBeenCalledTimes(1);
  });
});
