import { beforeEach, describe, expect, it } from 'vitest';

import { arenaBuild } from './arena-build';

const s = () => arenaBuild.getState();

describe('arena build', () => {
  beforeEach(() => arenaBuild.reset());

  it('loads the server’s build, padding a short bar, till the player changes theirs', () => {
    expect(s().bar).toBeNull();
    s().load({ skill_bar: ['strike'], traits: { strike: 'concussion' } });
    expect(s()).toMatchObject({
      bar: ['strike', '', '', '', '', '', '', ''],
      traits: { strike: 'concussion' },
      edits: 0,
    });
    s().put(1, 'war_cry');
    s().load({ skill_bar: ['taunt'], traits: {} }); // a read from before the change
    expect(s().bar).toEqual(['strike', 'war_cry', '', '', '', '', '', '']);
  });

  it('a skill put on a key leaves the key it was on; keys swap and clear; traits change and go back to the default; each is a change', () => {
    s().load({ skill_bar: ['strike', '', 'taunt'], traits: {} });
    s().put(4, 'strike');
    expect(s().bar).toEqual(['', '', 'taunt', '', 'strike', '', '', '']);
    s().swap(2, 4);
    expect(s().bar).toEqual(['', '', 'strike', '', 'taunt', '', '', '']);
    s().clear(2);
    expect(s().bar).toEqual(['', '', '', '', 'taunt', '', '', '']);
    s().choose('strike', 'concussion');
    s().choose('taunt', 'mock');
    s().choose('taunt', '');
    expect(s().traits).toEqual({ strike: 'concussion' });
    expect(s().edits).toBe(6);
  });

  it('a change before the profile comes starts from empty keys', () => {
    s().put(0, 'strike');
    expect(s().bar).toEqual(['strike', '', '', '', '', '', '', '']);
  });
});
