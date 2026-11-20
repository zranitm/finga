import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RestError } from '@/net/rest';
import type { ArenaBuild } from '@/net/rest';
import { arenaBuild } from '@/state/arena-build';

import { watchArenaBuild } from './saveArenaBuild';
import { SAVE_DELAY_MS } from './saveSkillBar';

function watched(
  answer: () => Promise<ArenaBuild> = () => Promise.resolve({ skill_bar: [], traits: {} }),
) {
  const putArenaBuild = vi.fn<(id: string, b: ArenaBuild) => Promise<ArenaBuild>>(() => answer());
  const notice = vi.fn();
  const stop = watchArenaBuild({ putArenaBuild }, 'c-ayla', notice);
  return { putArenaBuild, notice, stop };
}

describe('saving the arena build', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    arenaBuild.reset();
  });
  afterEach(() => vi.useRealTimers());

  it('saves once, the skill bar’s moment after the last of a run of changes, with every trait chosen, on the bar or not', () => {
    const { putArenaBuild, stop } = watched();
    arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} });
    arenaBuild.getState().choose('taunt', 'mock');
    vi.advanceTimersByTime(SAVE_DELAY_MS - 1);
    arenaBuild.getState().choose('strike', 'concussion');
    arenaBuild.getState().put(1, 'war_cry');
    vi.advanceTimersByTime(SAVE_DELAY_MS - 1);
    expect(putArenaBuild).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(putArenaBuild.mock.calls).toEqual([
      [
        'c-ayla',
        {
          skill_bar: ['strike', 'war_cry', '', '', '', '', '', ''],
          traits: { taunt: 'mock', strike: 'concussion' },
        },
      ],
    ]);
    void stop();
  });

  it('a load is not a change to save; stopping or closing the tab saves one still waiting at once, and nothing after', async () => {
    const { putArenaBuild, stop } = watched();
    arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} });
    vi.advanceTimersByTime(SAVE_DELAY_MS);
    expect(putArenaBuild).not.toHaveBeenCalled();
    arenaBuild.getState().put(0, 'taunt');
    window.dispatchEvent(new Event('pagehide'));
    expect(putArenaBuild).toHaveBeenCalledTimes(1);
    arenaBuild.getState().put(1, 'strike');
    void stop();
    await vi.advanceTimersByTimeAsync(0); // behind the save before it
    expect(putArenaBuild).toHaveBeenCalledTimes(2);
    arenaBuild.getState().clear(1);
    window.dispatchEvent(new Event('pagehide'));
    vi.advanceTimersByTime(SAVE_DELAY_MS);
    expect(putArenaBuild).toHaveBeenCalledTimes(2);
  });

  it('an ask to save now, as the skill window closes, saves a change still waiting at once, and nothing when none is', () => {
    const { putArenaBuild, stop } = watched();
    arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} });
    arenaBuild.getState().saveNow();
    expect(putArenaBuild).not.toHaveBeenCalled();
    arenaBuild.getState().put(0, 'taunt');
    arenaBuild.getState().saveNow();
    expect(putArenaBuild).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(SAVE_DELAY_MS);
    expect(putArenaBuild).toHaveBeenCalledTimes(1);
    void stop();
  });

  it('a refusal says the server’s words, no answer says it couldn’t save, and the build stays as the player left it', async () => {
    const answers = [
      () =>
        Promise.reject(new RestError(409, 550001, 'only a promoted character can fight in PvP')),
      () => Promise.reject(new Error('offline')),
    ];
    const { notice, stop } = watched(() => answers.shift()!());
    arenaBuild.getState().put(0, 'taunt');
    await vi.advanceTimersByTimeAsync(SAVE_DELAY_MS);
    arenaBuild.getState().put(1, 'strike');
    await vi.advanceTimersByTimeAsync(SAVE_DELAY_MS);
    expect(notice.mock.calls).toEqual([
      ['only a promoted character can fight in PvP'],
      ['Could not save the arena build.'],
    ]);
    expect(arenaBuild.getState().bar?.slice(0, 2)).toEqual(['taunt', 'strike']);
    void stop();
  });
});
