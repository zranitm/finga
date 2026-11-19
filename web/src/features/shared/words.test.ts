import { describe, expect, it } from 'vitest';

import { timeLeft } from './words';

describe('timeLeft', () => {
  it.each([
    { ms: 600000, text: '10:00' },
    { ms: 59001, text: '1:00' },
    { ms: 9000, text: '0:09' },
    { ms: -500, text: '0:00' },
  ])('$ms ms left reads $text', ({ ms, text }) => {
    expect(timeLeft(ms)).toBe(text);
  });
});
