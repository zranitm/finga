import { describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import type { Cooldown } from '@/state/skill-bar';

import { SkillKeys } from './skill-keys';
import type { Caster } from './skill-keys';

/** A bar of Strike, Spark, Second Wind and Throw, at 50 ms ticks; each use waits until its answer is settled by hand. A skill may start until action.free says otherwise, and then its press is held in action.held. spend counts the costs taken off and given back. */
function keys(caster: Partial<Caster> = {}, town = false) {
  let now = 1000;
  const action = { free: true, held: null as (() => void) | null, begun: 0, givenBack: 0 };
  const spend = { spent: [] as number[], refunded: 0 };
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
    slots: () => ['strike', 'spark', 'second_wind', 'throw', '', 'heal'],
    caster: () => ({
      invested: { strike: 1, spark: 10, second_wind: 2, heal: 1 },
      energy: 10,
      cooldownSpeed: 0,
      dead: false,
      hurt: true,
      feet: { x: 300, y: 608 },
      hurtAllies: [],
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
    spend: (cost) => {
      spend.spent.push(cost);
      return () => {
        spend.refunded++;
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
    spend,
    wait: (ms: number) => {
      now += ms;
    },
  };
}

const settled = () => new Promise((r) => setTimeout(r, 0));

describe('SkillKeys', () => {
  it('uses the skill on the key, taking off its energy and starting its cooldown at once: 4.5 s of 50 ms ticks for Strike', () => {
    const { k, use, cooldowns, spend } = keys();
    k.press(0);
    expect(use).toHaveBeenCalledWith('strike');
    expect(spend.spent).toEqual([2]);
    expect(cooldowns.strike).toEqual({ from: 1000, until: 5500 });
  });

  it('cooldown speed cuts the cooldown before it is counted in whole ticks', () => {
    const { k, cooldowns } = keys({ cooldownSpeed: 0.21 });
    k.press(1); // Spark's 5.625 s × 0.79 is 4443.75 ms: 89 ticks
    expect(cooldowns.spark).toEqual({ from: 1000, until: 5450 });
  });

  it('a key pressed again during the cooldown sends nothing and says so once, and once it ends it sends again', () => {
    const { k, use, notice, wait } = keys();
    k.press(0);
    wait(1000);
    k.press(0);
    wait(3499);
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

  it('says why a key did nothing: no points, a passive, damage in a town, or energy short', () => {
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
    const short = keys({ energy: 1 }); // Bolt costs 2
    short.k.press(1);
    expect(short.notice).toHaveBeenCalledWith('Not enough energy.');
    for (const r of [none, town, short]) {
      expect(r.use).not.toHaveBeenCalled();
      expect(r.cooldowns).toEqual({});
      expect(r.spend.spent).toEqual([]);
    }
  });

  it('a key short of energy says so once, until a press finds enough', () => {
    let energy = 1; // Bolt costs 2
    const { k, notice, use, wait } = keys({
      get energy() {
        return energy;
      },
    });
    k.press(1);
    k.press(1);
    expect(notice.mock.calls).toEqual([['Not enough energy.']]);
    energy = 2;
    k.press(1);
    expect(use).toHaveBeenCalledWith('spark');
    wait(5650); // past Spark's cooldown
    energy = 0;
    k.press(1);
    expect(notice.mock.calls).toEqual([['Not enough energy.'], ['Not enough energy.']]);
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

  it('a refusal gives back the cooldown, the action and the energy and says the world’s words; no answer keeps them, as it may have landed', async () => {
    const refused = keys();
    refused.k.press(0);
    refused.answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_ENERGY, 'Not enough energy.'));
    await settled();
    expect(refused.cooldowns).toEqual({});
    expect(refused.action).toMatchObject({ begun: 1, givenBack: 1 });
    expect(refused.spend).toEqual({ spent: [2], refunded: 1 });
    expect(refused.notice).toHaveBeenCalledWith('Not enough energy.');
    const lost = keys();
    lost.k.press(0);
    lost.answers[0]!.reject(new RequestError('timeout', 'no response'));
    await settled();
    expect(lost.cooldowns.strike).toEqual({ from: 1000, until: 5500 });
    expect(lost.action).toMatchObject({ begun: 1, givenBack: 0 });
    expect(lost.spend).toEqual({ spent: [2], refunded: 0 });
    expect(lost.notice).not.toHaveBeenCalled();
  });

  it('the world saying a skill isn’t ready keeps its cooldown, so the key doesn’t look ready, and gives the energy back', async () => {
    const { k, answers, cooldowns, notice, action, spend } = keys();
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.SKILL_COOLING_DOWN, 'That skill isn’t ready yet.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 1000, until: 5500 });
    expect(action.givenBack).toBe(1);
    expect(spend.refunded).toBe(1);
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
    let energy = 10;
    const { k, use, cooldowns, notice, action, wait } = keys({
      get energy() {
        return energy;
      },
    });
    action.free = false;
    k.press(0);
    expect([use.mock.calls.length, action.begun]).toEqual([0, 0]);
    action.free = true;
    wait(300);
    action.held!();
    expect(use.mock.calls).toEqual([['strike']]);
    expect(cooldowns.strike).toEqual({ from: 1300, until: 5800 });
    action.free = false;
    k.press(1);
    energy = 0;
    action.free = true;
    action.held!();
    expect(notice).toHaveBeenCalledWith('Not enough energy.');
    expect(use).toHaveBeenCalledTimes(1);
  });

  it('a key held for its moment counts the energy then: an attack’s gain that came meanwhile pays for it', () => {
    let energy = 0;
    const { k, use, notice, action } = keys({
      get energy() {
        return energy;
      },
    });
    action.free = false;
    k.press(0);
    expect(notice).not.toHaveBeenCalled();
    energy = 2; // the attack it waits on reached a mob
    action.free = true;
    action.held!();
    expect(use).toHaveBeenCalledWith('strike');
    expect(notice).not.toHaveBeenCalled();
  });

  it('a cooldown ends at its time', () => {
    vi.useFakeTimers();
    try {
      const { k, cooldowns } = keys();
      k.press(0);
      vi.advanceTimersByTime(4499);
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
    wait(4500);
    k.press(0);
    answers[0]!.reject(new RequestError(ERR.NOT_ENOUGH_ENERGY, 'Not enough energy.'));
    await settled();
    expect(cooldowns.strike).toEqual({ from: 5500, until: 10000 });
  });

  // Heal reaches 150 px from the caster's middle to the nearest point of a 24 px wide box: a member's feet up to 162 px away on level ground.
  it.each([
    ['no one hurt', { hurt: false }, false],
    ['the caster hurt', {}, true],
    ['a party member hurt beside them', { hurt: false, hurtAllies: [{ x: 400, y: 608 }] }, true],
    [
      'a party member hurt at its edge',
      { hurt: false, hurtAllies: [{ x: 300 + 162, y: 608 }] },
      true,
    ],
    [
      'the hurt party members out of reach',
      {
        hurt: false,
        hurtAllies: [
          { x: 300 + 162 + 1, y: 608 },
          { x: 0, y: 300 },
        ],
      },
      false,
    ],
    ['a hurt party member not drawn yet', { hurt: false, hurtAllies: [null] }, true],
  ] as const)(
    'a heal with %s: goes %s, else says why so no casting starts to be taken back',
    (_, caster, goes) => {
      const k = keys(caster);
      k.k.press(5);
      if (goes) {
        expect(k.use).toHaveBeenCalledWith('heal');
      } else {
        expect([k.use.mock.calls.length, k.action.begun]).toEqual([0, 0]);
        expect(k.notice).toHaveBeenCalledWith('Your HP is already full.');
      }
    },
  );
});
