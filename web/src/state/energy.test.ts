import { beforeEach, describe, expect, it } from 'vitest';

import { energy } from './energy';

const s = () => energy.getState();

describe('energy', () => {
  beforeEach(() => {
    energy.reset();
  });

  it('shows the Welcome’s count, then each Delta’s, and a Delta without one keeps the last', () => {
    s().welcome(6);
    expect(s().shown).toBe(6);
    s().delta(3, 8);
    expect(s()).toMatchObject({ told: 8, ackSeq: 3, shown: 8 });
    s().delta(4, undefined);
    expect(s()).toMatchObject({ told: 8, ackSeq: 4, shown: 8 });
  });

  it('a press shows at once, and stays taken off until a Delta acks the input after its own, whose count has it', () => {
    s().welcome(6);
    s().spend(10, 2);
    expect(s().shown).toBe(4);
    s().delta(9, undefined); // before the press: the world's 6 can't refill it
    s().delta(10, undefined); // its own input, which the world may apply before it has the press
    expect(s().shown).toBe(4);
    s().delta(11, 4); // acks the next, with the spend counted
    expect(s()).toMatchObject({ pending: [], shown: 4 });
  });

  it('a press the world takes in a tick after its own input never blinks back, however the frames acking that input fall', () => {
    s().welcome(4);
    s().delta(5, undefined);
    const shown = new Set<number>();
    const off = energy.subscribe((now) => shown.add(now.shown));
    s().spend(5, 2); // pressed during input 5, which the world has applied
    s().delta(5, undefined); // the next frame, before the world has the press
    s().delta(5, undefined); // the press goes, still told as of input 5
    s().delta(6, 2); // the input after it, with the spend counted
    off();
    expect([...shown]).toEqual([2]);
    expect(s()).toMatchObject({ pending: [], shown: 2 });
  });

  it('of two presses, a Delta acking the first keeps the second taken off', () => {
    s().welcome(8);
    s().spend(10, 2);
    s().spend(12, 3);
    expect(s().shown).toBe(3);
    s().delta(11, 6); // the world counts the first spend only
    expect(s()).toMatchObject({ pending: [{ seq: 12, cost: 3 }], shown: 3 });
    s().delta(13, 3);
    expect(s()).toMatchObject({ pending: [], shown: 3 });
  });

  it('a Welcome, as on a reconnect, drops the presses before it and shows its count', () => {
    s().welcome(8);
    s().spend(10, 2);
    s().delta(9, 8);
    s().welcome(4);
    expect(s()).toMatchObject({ told: 4, ackSeq: 0, pending: [], shown: 4 });
    s().delta(1, undefined); // the new connection's inputs count from 1
    expect(s().shown).toBe(4);
  });

  it('a press the world refused comes back once it is acked, or at once when the refusal arrives first', () => {
    s().welcome(6);
    s().spend(10, 2);
    s().delta(11, undefined); // acked, and the world's count never moved
    expect(s().shown).toBe(6);
    const refund = s().spend(11, 3);
    expect(s().shown).toBe(3);
    refund();
    expect(s().shown).toBe(6);
    refund(); // twice does nothing more
    expect(s().shown).toBe(6);
  });

  it('a refund after its Delta acked it changes nothing', () => {
    s().welcome(6);
    const refund = s().spend(10, 2);
    s().delta(11, 4);
    refund();
    expect(s().shown).toBe(4);
  });

  it('never shows less than none', () => {
    s().welcome(2);
    s().spend(10, 2);
    s().spend(11, 3); // a second press the world will refuse
    expect(s().shown).toBe(0);
  });

  it('a new map’s join drops the presses before it, which its frames count already', () => {
    s().welcome(6);
    s().spend(10, 2);
    s().joined();
    expect(s()).toMatchObject({ pending: [], shown: 6 });
    s().delta(0, 4);
    expect(s().shown).toBe(4);
  });

  it('clear empties it', () => {
    s().welcome(6);
    s().spend(10, 2);
    s().clear();
    expect(s()).toMatchObject({ told: 0, ackSeq: 0, pending: [], shown: 0 });
  });
});
