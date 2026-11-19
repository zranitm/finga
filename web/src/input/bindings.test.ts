import { describe, expect, it } from 'vitest';

import { defaults, keyName, keyOf, keyTable, rebind, shortKeyName } from './bindings';

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

describe('the player’s keys', () => {
  it('lay the changes over the defaults', () => {
    const table = keyTable({ attack: 'KeyB', skill1: 'ShiftRight' });
    expect([table.attack, table.skill1, table.skill2]).toEqual([
      ['KeyB'],
      ['ShiftRight'],
      ['KeyW'],
    ]);
    expect(keyTable({})).toEqual(defaults);
  });

  it.each([
    { name: 'an action this client doesn’t know', changes: { fly: 'KeyB' } },
    { name: 'a fixed action', changes: { chat: 'KeyB', close: 'KeyG' } },
    { name: 'a reserved key', changes: { attack: 'F5' } },
    { name: 'a key another action keeps by default', changes: { attack: 'KeyQ' } },
  ])('ignore $name, which keeps its default', ({ changes }) => {
    expect(keyTable(changes)).toEqual(defaults);
  });

  it('keep a swap, each action on the other’s key', () => {
    const table = keyTable({ attack: 'KeyQ', skill1: 'KeyX' });
    expect([table.attack, table.skill1]).toEqual([['KeyQ'], ['KeyX']]);
  });

  it('never leave one key doing two things, even when a change ignored puts an action back on a key another change took', () => {
    // Skill 1's change clashes with Skill 2's default, so Skill 1 stays on Q, which Attack's change wanted.
    const table = keyTable({ skill1: 'KeyW', attack: 'KeyQ' });
    expect([table.skill1, table.attack, table.skill2]).toEqual([['KeyQ'], ['KeyX'], ['KeyW']]);
    const codes = Object.values(table).flat();
    expect(new Set(codes).size).toBe(codes.length);
  });

  it('are worked out once per set of changes', () => {
    const changes = { attack: 'KeyB' };
    expect(keyTable(changes)).toBe(keyTable(changes));
  });

  it('rebind moves the action holding a key onto the old key, and keep only what differs from the defaults', () => {
    expect(rebind({}, 'attack', 'KeyB')).toEqual({ changes: { attack: 'KeyB' }, moved: null });
    expect(rebind({}, 'attack', 'KeyQ')).toEqual({
      changes: { attack: 'KeyQ', skill1: 'KeyX' },
      moved: 'skill1',
    });
    expect(rebind({ attack: 'KeyQ', skill1: 'KeyX' }, 'attack', 'KeyX')).toEqual({
      changes: {},
      moved: 'skill1',
    });
    expect(rebind({ attack: 'KeyB' }, 'attack', 'KeyX')).toEqual({ changes: {}, moved: null });
  });

  it('name keys as the player’s keyboard prints them, where the browser says, else by US names', () => {
    const printed = new Map([
      ['KeyQ', 'a'],
      ['Semicolon', 'm'],
      ['KeyW', ' '],
    ]);
    expect([
      keyName('KeyQ', printed),
      keyName('Semicolon', printed),
      keyName('KeyW', printed),
      keyName('ArrowUp', printed),
      keyOf(defaults, 'skill1', printed),
    ]).toEqual(['A', 'M', 'W', '↑', 'A']);
  });

  it('upper-cases what a key prints only while it stays one letter: German Minus stays ß, not SS', () => {
    const printed = new Map([
      ['Minus', 'ß'],
      ['BracketLeft', 'ü'],
    ]);
    expect([keyName('Minus', printed), keyName('BracketLeft', printed)]).toEqual(['ß', 'Ü']);
  });

  it('names a key short for the skill bar, the long ones cut down, the rest as they are', () => {
    expect(
      [
        'ShiftLeft',
        'ShiftRight',
        'ControlLeft',
        'AltRight',
        'Backspace',
        'Numpad5',
        'Space',
        'KeyQ',
      ].map((c) => shortKeyName(c)),
    ).toEqual(['LShift', 'RShift', 'LCtrl', 'RAlt', 'Bksp', 'N5', 'Space', 'Q']);
    expect(shortKeyName('KeyQ', new Map([['KeyQ', 'a']]))).toBe('A'); // as the keyboard prints it
  });
});
