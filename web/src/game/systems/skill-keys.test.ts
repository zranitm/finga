import { describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import type { Cooldown } from '@/state/skill-bar';

import { SkillKeys } from './skill-keys';
import type { Caster } from './skill-keys';

/** A bar of Strike, Spark, Second Wind and Throw, at 50 ms ticks; each use waits until its answer is settled by hand. A skill may start until action.free says otherwise, and then its press is held in action.held. */
function keys(caster: Partial<Caster> = {}, town = false) {
  let now = 1000;
  const action = { free: true, held: null as (() => void) | null, begun: 0, givenBack: 0 };
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
    free: (again) => {
      action.held = action.free ? null : again;
      return action.free;
    },
    begin: () => {
      action.begun++;
      return () => {
        action.givenBack++;
      };
    },
  });
  return {
    k,
    use,
    notice,
    answers,
    cooldowns,
    action,
    wait: (ms: number) => {
      now += ms;
    },
  };
}

const settled = () => new Promise((r) => setTimeout(r, 0));

describe('SkillKeys', () => {
  it('uses the skill on the key and starts its cooldown at once: 3 s of 50 ms ticks for Strike', () => {
    const { k, use, cooldowns } = keys();
    k.press(0);
    expect(use).toHaveBeenCalledWith('strike');
    expect(cooldowns.strike).toEqual({ from: 1000, until: 4000 });
  });

  it('cooldown speed cuts the cooldown before it is counted in whole ticks', () => {
    const { k, cooldowns } = keys({ cooldownSpeed: 0.21 });
    k.press(1); // Spark's 3.75 s × 0.79 is 2962.5 ms: 60 ticks
    expect(cooldowns.spark).toEqual({ from: 1000, until: 4000 });
  });

  it('a key pressed again during the cooldown sends nothing and says so once, and once it ends it sends again', () => {
    const { k, use, notice, wait } = keys();
    k.press(0);
    wait(1000);
    k.press(0);
    wait(1999);
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
    wait(4000); // past Spark's cooldown
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

  it('a refusal gives back the cooldown and the action and says the world’s words; no answer keeps both, as it may have landed', async () => {
    const refused = keys();
    refused.k.press(0);
    refused.answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_MP, 'Not enough MP.'));
    await settled();
    expect(refused.cooldowns).toEqual({});
    expect(refused.action).toMatchObject({ begun: 1, givenBack: 1 });
    expect(refused.notice).toHaveBeenCalledWith('Not enough MP.');
    const lost = keys();
    lost.k.press(0);
    lost.answers[0]!.reject(new RequestError('timeout', 'no response'));
    await settled();
    expect(lost.cooldowns.strike).toEqual({ from: 1000, until: 4000 });
    expect(lost.action).toMatchObject({ begun: 1, givenBack: 0 });
    expect(lost.notice).not.toHaveBeenCalled();
  });

  it('the world saying a skill isn’t ready keeps its cooldown, so the key doesn’t look ready', async () => {
    const { k, answers, cooldowns, notice, action } = keys();
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.SKILL_COOLING_DOWN, 'That skill isn’t ready yet.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 1000, until: 4000 });
    expect(action.givenBack).toBe(1);
    expect(notice).toHaveBeenCalledWith('That skill isn’t ready yet.');
  });

  it('another skill already waiting gives the action back and undoes the cooldown, as this one never went', async () => {
    const { k, answers, cooldowns, action } = keys();
    k.press(0);
    answers[0]!.reject(
      new RequestError(ERR.SKILL_WAITING, 'Another skill is already waiting for its turn.'),
    );
    await settled();
    expect(cooldowns).toEqual({});
    expect(action.givenBack).toBe(1);
  });

  it('a key the action clock holds sends nothing, and pressed again at its moment is checked and timed then', () => {
    let mp = 50;
    const { k, use, cooldowns, notice, action, wait } = keys({
      get mp() {
        return mp;
      },
    });
    action.free = false;
    k.press(0);
    expect([use.mock.calls.length, action.begun]).toEqual([0, 0]);
    action.free = true;
    wait(300);
    action.held!();
    expect(use.mock.calls).toEqual([['strike']]);
    expect(cooldowns.strike).toEqual({ from: 1300, until: 4300 });
    action.free = false;
    k.press(1);
    mp = 0;
    action.free = true;
    action.held!();
    expect(notice).toHaveBeenCalledWith('Not enough MP.');
    expect(use).toHaveBeenCalledTimes(1);
  });

  it('a cooldown ends at its time', () => {
    vi.useFakeTimers();
    try {
      const { k, cooldowns } = keys();
      k.press(0);
      vi.advanceTimersByTime(2999);
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
    wait(3000);
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_MP, 'Not enough MP.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 4000, until: 7000 });
  });
});
