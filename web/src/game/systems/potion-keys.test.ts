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
const blue = create(ItemViewSchema, {
  itemId: 'i-blue',
  template: 'blue_potion',
  tab: 'use',
  slot: 1,
  quantity: 3,
});

/** Each use waits until its answer is settled by hand. */
function keys(hp = 20, mp = 10, useTab = [red, blue]) {
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
    vitals: () => ({ hp, maxHp: 200, mp, maxMp: 100 }),
    useTab: () => useTab,
    use,
    drank,
    notice,
  });
  return { k, use, drank, notice, answers };
}

const settled = () => new Promise((r) => setTimeout(r, 0));

describe('PotionKeys', () => {
  it('drinks the potion for the key, and once the world applied it says what it restored', async () => {
    const { k, use, drank, answers } = keys(170);
    k.press('hp');
    expect(use).toHaveBeenCalledWith(red);
    expect(drank).not.toHaveBeenCalled();
    answers[0]!.resolve();
    await settled();
    expect(drank).toHaveBeenCalledWith('red_potion', { hp: 30, mp: 0 });
  });

  it('a key waits for its answer before it drinks again; the other key does not wait', async () => {
    const { k, use, answers } = keys();
    k.press('hp');
    k.press('hp');
    k.press('mp');
    expect(use.mock.calls).toEqual([[red], [blue]]);
    answers[0]!.resolve();
    await settled();
    k.press('hp');
    expect(use).toHaveBeenCalledTimes(3);
  });

  it('a full vital or no potion for it sends nothing and says why', () => {
    const full = keys(200, 100);
    full.k.press('hp');
    full.k.press('mp');
    expect(full.use).not.toHaveBeenCalled();
    expect(full.notice.mock.calls).toEqual([
      ['Your HP is already full.'],
      ['Your MP is already full.'],
    ]);
    const none = keys(20, 10, [red]);
    none.k.press('mp');
    expect(none.use).not.toHaveBeenCalled();
    expect(none.notice).toHaveBeenCalledWith('You have no MP potions.');
  });

  it('a refusal says why; no answer says nothing; either way the key works again and nothing is drunk', async () => {
    const { k, use, drank, notice, answers } = keys();
    k.press('hp');
    answers[0]!.reject(new RequestError(330001, 'You can’t do that while dead.'));
    await settled();
    expect(notice).toHaveBeenCalledWith('You can’t do that while dead.');
    k.press('hp');
    answers[1]!.reject(new RequestError('timeout', 'No answer yet.'));
    await settled();
    expect(notice).toHaveBeenCalledTimes(1);
    k.press('hp');
    expect(use).toHaveBeenCalledTimes(3);
    expect(drank).not.toHaveBeenCalled();
  });
});
