import { describe, expect, it, vi } from 'vitest';

import type { Input } from '@/sim/input';

import { InputSender } from './input-sender';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

function sender() {
  const sent: { input: Input; tick: number }[] = [];
  const send = vi.fn((input: Input, tick: number) => {
    sent.push({ input, tick });
    return true;
  });
  return { s: new InputSender({ send }), sent, send };
}

describe('InputSender', () => {
  it('sends exactly one Input per tick with consecutive tick numbers, including ticks where no key changed', () => {
    const { s, sent } = sender();
    s.press('right');
    for (let i = 0; i < 5; i++) {
      s.tickStart();
    }
    expect(sent.map((x) => x.tick)).toEqual([1, 2, 3, 4, 5]);
    expect(sent.every((x) => x.input.right && !x.input.left)).toBe(true);
    s.release('right');
    s.tickStart();
    expect(sent[5]).toEqual({ input: noInput, tick: 6 });
  });

  it.each([
    {
      name: 'a jump pressed and released between two tick boundaries goes out as jump: true for that tick, once',
      act: (s: InputSender) => {
        s.press('jump');
        s.release('jump');
      },
      first: { ...noInput, jump: true },
      second: noInput,
    },
    {
      name: 'a jump still held at the boundary is sent for every tick it is held',
      act: (s: InputSender) => {
        s.press('jump');
      },
      first: { ...noInput, jump: true },
      second: { ...noInput, jump: true },
    },
    {
      name: 'left and right together go out as both; the sim decides',
      act: (s: InputSender) => {
        s.press('left');
        s.press('right');
      },
      first: { ...noInput, left: true, right: true },
      second: { ...noInput, left: true, right: true },
    },
  ])('$name', ({ act, first, second }) => {
    const { s } = sender();
    act(s);
    expect(s.tickStart()).toEqual(first);
    expect(s.tickStart()).toEqual(second);
  });

  it('releaseAll drops every held key and a jump tapped this tick', () => {
    const { s } = sender();
    s.press('left');
    s.press('up');
    s.press('jump');
    s.release('jump');
    s.releaseAll();
    expect(s.tickStart()).toEqual(noInput);
  });

  it('a refused send (socket closed) still advances the tick, so numbers stay consecutive when it reopens', () => {
    const sent: number[] = [];
    const s = new InputSender({
      send: (_input, tick) => {
        sent.push(tick);
        return tick !== 2;
      },
    });
    s.tickStart();
    s.tickStart();
    s.tickStart();
    expect(sent).toEqual([1, 2, 3]);
  });
});
