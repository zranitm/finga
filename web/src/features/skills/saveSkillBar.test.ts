import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RestError } from '@/net/rest';
import { skillBar } from '@/state/skill-bar';

import { SAVE_DELAY_MS, watchSkillBar } from './saveSkillBar';

function watched(
  answer: () => Promise<{ slots: string[] }> = () => Promise.resolve({ slots: [] }),
) {
  const putSkillBar = vi.fn<(id: string, slots: readonly string[]) => Promise<{ slots: string[] }>>(
    () => answer(),
  );
  const notice = vi.fn();
  const stop = watchSkillBar({ putSkillBar }, 'c-ayla', notice);
  return { putSkillBar, notice, stop };
}

describe('saving the skill bar', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    skillBar.reset();
  });
  afterEach(() => vi.useRealTimers());

  it('saves once, a moment after the last of a run of changes', () => {
    const { putSkillBar, stop } = watched();
    skillBar.getState().put(0, 'strike');
    vi.advanceTimersByTime(SAVE_DELAY_MS - 1);
    skillBar.getState().put(1, 'spark');
    vi.advanceTimersByTime(SAVE_DELAY_MS - 1);
    expect(putSkillBar).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(putSkillBar.mock.calls).toEqual([['c-ayla', ['strike', 'spark', '', '', '', '']]]);
    stop();
  });

  it('a load, a cooldown or a buff is not a change to save', () => {
    const { putSkillBar, stop } = watched();
    skillBar.getState().load(['strike']);
    skillBar.getState().cool('strike', { from: 0, until: 1 });
    skillBar.getState().buff({ skill: 'rally', points: 1, until: 1 });
    vi.advanceTimersByTime(SAVE_DELAY_MS);
    expect(putSkillBar).not.toHaveBeenCalled();
    stop();
  });

  it('stopping saves a change still waiting, and nothing after', () => {
    const { putSkillBar, stop } = watched();
    skillBar.getState().put(2, 'throw');
    stop();
    expect(putSkillBar).toHaveBeenCalledTimes(1);
    skillBar.getState().clear(2);
    vi.advanceTimersByTime(SAVE_DELAY_MS);
    expect(putSkillBar).toHaveBeenCalledTimes(1);
  });

  it('a refusal or no answer keeps the bar and says so', async () => {
    const { notice, stop } = watched(() =>
      Promise.reject(new RestError(400, 120007, 'That can’t go on the skill bar.')),
    );
    skillBar.getState().put(0, 'second_wind');
    await vi.advanceTimersByTimeAsync(SAVE_DELAY_MS);
    expect(notice).toHaveBeenCalledWith('That can’t go on the skill bar.');
    expect(skillBar.getState().slots[0]).toBe('second_wind');
    stop();
  });
});
