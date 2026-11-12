import { describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import type { Cooldown } from '@/state/skill-bar';

import { SkillKeys } from './skill-keys';
import type { Caster } from './skill-keys';

/** A bar of Strike, Spark, Second Wind and Throw, at 50 ms ticks; each use waits until its answer is settled by hand. The action turn is free until turnAt says otherwise. */
function keys(caster: Partial<Caster> = {}, town = false) {
  let now = 1000;
  const turn = { at: 0, taken: 0, givenBack: 0 };
  const cooldowns: Record<string, Cooldown> = {};
  const answers: { resolve: () => void; reject: (err: Error) => void }[] = [];
  const use = vi.fn(
    () =>
      new Promise<unknown>((resolve, reject) =>
        answers.push({ resolve: () => resolve({}), reject }),
      ),
  );
  const notice = vi.fn();
  const k = new SkillKeys({
    slots: () => ['strike', 'spark', 'second_wind', 'throw', '', ''],
    caster: () => ({
      invested: { strike: 1, spark: 10, second_wind: 2 },
      mp: 50,
      cooldownSpeed: 0,
      dead: false,
      ...caster,
    }),
    inTown: () => town,
    tickMs: 50,
    now: () => now,
    cooldowns: () => cooldowns,
    cool: (skill, c) => {
      if (c) cooldowns[skill] = c;
      else delete cooldowns[skill];
    },
    use,
    notice,
    turnIn: () => Math.max(0, turn.at - now),
    takeTurn: () => {
      turn.taken++;
      return () => {
        turn.givenBack++;
      };
    },
  });
  return {
    k,
    use,
    notice,
    answers,
    cooldowns,
    turn,
    wait: (ms: number) => {
      now += ms;
    },
  };
}

const settled = () => new Promise((r) => setTimeout(r, 0));

describe('SkillKeys', () => {
  it('uses the skill on the key and starts its cooldown at once: 2 s of 50 ms ticks for Strike', () => {
    const { k, use, cooldowns } = keys();
    k.press(0);
    expect(use).toHaveBeenCalledWith('strike');
    expect(cooldowns.strike).toEqual({ from: 1000, until: 3000 });
  });

  it('cooldown speed cuts the cooldown before it is counted in whole ticks', () => {
    const { k, cooldowns } = keys({ cooldownSpeed: 0.21 });
    k.press(1); // Spark's 2.5 s × 0.79 is 1975 ms: 40 ticks
    expect(cooldowns.spark).toEqual({ from: 1000, until: 3000 });
  });

  it('a key pressed again during the cooldown sends nothing and says so once, and once it ends it sends again', () => {
    const { k, use, notice, wait } = keys();
    k.press(0);
    wait(1000);
    k.press(0);
    wait(999);
    k.press(0);
    expect(use).toHaveBeenCalledTimes(1);
    expect(notice.mock.calls).toEqual([['That skill isn’t ready yet.']]);
    wait(1);
    k.press(0);
    expect(use).toHaveBeenCalledTimes(2);
    wait(1);
    k.press(0); // the next cooldown says it again
    expect(notice).toHaveBeenCalledTimes(2);
  });

  it('says why a key did nothing: no points, a passive, damage in a town, or MP short', () => {
    const none = keys();
    none.k.press(3); // Throw has no points
    none.k.press(2); // Second Wind is passive
    expect(none.notice.mock.calls).toEqual([
      ['Put a point in that skill first.'],
      ['A passive skill is always on.'],
    ]);
    const town = keys({}, true);
    town.k.press(0);
    expect(town.notice).toHaveBeenCalledWith('You can’t use that in town.');
    const short = keys({ mp: 8 }); // Bolt at 10 points costs 6 + 3
    short.k.press(1);
    expect(short.notice).toHaveBeenCalledWith('Not enough MP.');
    for (const r of [none, town, short]) {
      expect(r.use).not.toHaveBeenCalled();
      expect(r.cooldowns).toEqual({});
    }
  });

  it('a key short of MP says so once, until a press finds enough', () => {
    let mp = 8; // Bolt costs 9
    const { k, notice, use, wait } = keys({
      get mp() {
        return mp;
      },
    });
    k.press(1);
    k.press(1);
    expect(notice.mock.calls).toEqual([['Not enough MP.']]);
    mp = 9;
    k.press(1);
    expect(use).toHaveBeenCalledWith('spark');
    wait(3000); // past Spark's cooldown
    mp = 0;
    k.press(1);
    expect(notice.mock.calls).toEqual([['Not enough MP.'], ['Not enough MP.']]);
  });

  it('an empty key, a dead player or no character yet does nothing', () => {
    for (const { k, use, notice } of [keys(), keys({ dead: true })]) {
      k.press(4);
      expect(use).not.toHaveBeenCalled();
      expect(notice).not.toHaveBeenCalled();
    }
    const dead = keys({ dead: true });
    dead.k.press(0);
    expect(dead.use).not.toHaveBeenCalled();
  });

  it('a refusal gives back the cooldown and the turn and says the world’s words; no answer keeps both, as it may have landed', async () => {
    const refused = keys();
    refused.k.press(0);
    refused.answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_MP, 'Not enough MP.'));
    await settled();
    expect(refused.cooldowns).toEqual({});
    expect(refused.turn).toMatchObject({ taken: 1, givenBack: 1 });
    expect(refused.notice).toHaveBeenCalledWith('Not enough MP.');
    const lost = keys();
    lost.k.press(0);
    lost.answers[0]!.reject(new RequestError('timeout', 'no response'));
    await settled();
    expect(lost.cooldowns.strike).toEqual({ from: 1000, until: 3000 });
    expect(lost.turn).toMatchObject({ taken: 1, givenBack: 0 });
    expect(lost.notice).not.toHaveBeenCalled();
  });

  it('the world saying a skill isn’t ready keeps its cooldown, so the key doesn’t look ready', async () => {
    const { k, answers, cooldowns, notice, turn } = keys();
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.SKILL_COOLING_DOWN, 'That skill isn’t ready yet.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 1000, until: 3000 });
    expect(turn.givenBack).toBe(1);
    expect(notice).toHaveBeenCalledWith('That skill isn’t ready yet.');
  });

  it('a key pressed before the action turn waits for it, checked again then; a newer press takes its place, and stop forgets it', () => {
    vi.useFakeTimers();
    try {
      const { k, use, turn, wait } = keys();
      turn.at = 1300;
      k.press(0);
      expect([use.mock.calls.length, k.waiting()]).toEqual([0, true]);
      k.press(1); // Spark instead
      wait(300);
      vi.advanceTimersByTime(300);
      expect(use.mock.calls).toEqual([['spark']]);
      expect([turn.taken, k.waiting()]).toEqual([1, false]);
      turn.at = 1400;
      k.press(0);
      k.stop();
      wait(100);
      vi.advanceTimersByTime(100);
      expect(use).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a cooldown ends at its time', () => {
    vi.useFakeTimers();
    try {
      const { k, cooldowns } = keys();
      k.press(0);
      vi.advanceTimersByTime(1999);
      expect(cooldowns.strike).toBeDefined();
      vi.advanceTimersByTime(1);
      expect(cooldowns).toEqual({});
    } finally {
      vi.useRealTimers();
    }
  });

  it('a late refusal leaves a newer cooldown alone', async () => {
    const { k, answers, cooldowns, wait } = keys();
    k.press(0);
    wait(2000);
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_MP, 'Not enough MP.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 3000, until: 5000 });
  });
});
