import { describe, expect, it } from 'vitest';

import { defaults, keyName, keyOf, keyTable, rebind } from './bindings';

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
    const table = keyTable({ attack: 'KeyV', skill1: 'ShiftLeft' });
    expect([table.attack, table.skill1, table.skill2]).toEqual([['KeyV'], ['ShiftLeft'], ['KeyW']]);
    expect(keyTable({})).toEqual(defaults);
  });

  it.each([
    { name: 'an action this client doesn’t know', changes: { fly: 'KeyV' } },
    { name: 'a fixed action', changes: { chat: 'KeyV', close: 'KeyG' } },
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
    const changes = { attack: 'KeyV' };
    expect(keyTable(changes)).toBe(keyTable(changes));
  });

  it('rebind moves the action holding a key onto the old key, and keep only what differs from the defaults', () => {
    expect(rebind({}, 'attack', 'KeyV')).toEqual({ changes: { attack: 'KeyV' }, moved: null });
    expect(rebind({}, 'attack', 'KeyQ')).toEqual({
      changes: { attack: 'KeyQ', skill1: 'KeyX' },
      moved: 'skill1',
    });
    expect(rebind({ attack: 'KeyQ', skill1: 'KeyX' }, 'attack', 'KeyX')).toEqual({
      changes: {},
      moved: 'skill1',
    });
    expect(rebind({ attack: 'KeyV' }, 'attack', 'KeyX')).toEqual({ changes: {}, moved: null });
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
});
