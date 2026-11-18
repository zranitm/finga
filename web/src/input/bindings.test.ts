import { describe, expect, it } from 'vitest';

import { defaults, keyName, keyOf } from './bindings';

describe('key names', () => {
  it.each([
    ['KeyQ', 'Q'],
    ['Digit1', '1'],
    ['Numpad1', 'Num 1'],
    ['NumpadEnter', 'Num Enter'],
    ['ArrowUp', '↑'],
    ['ShiftLeft', 'Left Shift'],
    ['Space', 'Space'],
    ['Escape', 'Esc'],
    ['Semicolon', ';'],
    ['F7', 'F7'],
  ])('%s is %s', (code, name) => {
    expect(keyName(code)).toBe(name);
  });

  it('an action is named by its first key', () => {
    expect([
      keyOf(defaults, 'skill1'),
      keyOf(defaults, 'hpPotion'),
      keyOf(defaults, 'chat'),
    ]).toEqual(['Q', '1', 'Enter']);
  });
});
