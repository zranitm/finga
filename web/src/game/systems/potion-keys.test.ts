import { create } from '@bufbuild/protobuf';
import { describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ItemViewSchema } from '@/protocol/messages';

import { PotionKeys } from './potion-keys';

const red = create(ItemViewSchema, {
  itemId: 'i-red',
  template: 'red_potion',
  tab: 'use',
  quantity: 3,
});
const gel = create(ItemViewSchema, {
  itemId: 'i-gel',
  template: 'slime_gel',
  tab: 'etc',
  quantity: 3,
});

/** Each use waits until its answer is settled by hand. */
function keys(hp = 20, useTab = [red]) {
  const answers: { resolve: () => void; reject: (err: Error) => void }[] = [];
  const use = vi.fn(
    () =>
      new Promise<unknown>((resolve, reject) =>
        answers.push({ resolve: () => resolve({}), reject }),
      ),
  );
  const drank = vi.fn();
  const notice = vi.fn();
  const k = new PotionKeys({
    vitals: () => ({ hp, maxHp: 200 }),
    useTab: () => useTab,
    use,
    drank,
    notice,
  });
  return { k, use, drank, notice, answers };
}

const settled = () => new Promise((r) => setTimeout(r, 0));

describe('PotionKeys', () => {
  it('drinks the potion, and once the world applied it says the HP it restored', async () => {
    const { k, use, drank, answers } = keys(170);
    k.press();
    expect(use).toHaveBeenCalledWith(red);
    expect(drank).not.toHaveBeenCalled();
    answers[0]!.resolve();
    await settled();
    expect(drank).toHaveBeenCalledWith('red_potion', 30);
  });

  it('waits for its answer before it drinks again', async () => {
    const { k, use, answers } = keys();
    k.press();
    k.press();
    expect(use).toHaveBeenCalledTimes(1);
    answers[0]!.resolve();
    await settled();
    k.press();
    expect(use).toHaveBeenCalledTimes(2);
  });

  it('full HP or no potion sends nothing and says why', () => {
    const full = keys(200);
    full.k.press();
    expect(full.use).not.toHaveBeenCalled();
    expect(full.notice).toHaveBeenCalledWith('Your HP is already full.');
    const none = keys(20, [gel]);
    none.k.press();
    expect(none.use).not.toHaveBeenCalled();
    expect(none.notice).toHaveBeenCalledWith('You have no HP potions.');
  });

  it('a refusal says why; no answer says nothing; either way the key works again and nothing is drunk', async () => {
    const { k, use, drank, notice, answers } = keys();
    k.press();
    answers[0]!.reject(new RequestError(330001, 'You can’t do that while dead.'));
    await settled();
    expect(notice).toHaveBeenCalledWith('You can’t do that while dead.');
    k.press();
    answers[1]!.reject(new RequestError('timeout', 'No answer yet.'));
    await settled();
    expect(notice).toHaveBeenCalledTimes(1);
    k.press();
    expect(use).toHaveBeenCalledTimes(3);
    expect(drank).not.toHaveBeenCalled();
  });
});
