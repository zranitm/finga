import { afterEach, describe, expect, it, vi } from 'vitest';

import { defaults } from '@/input/bindings';
import type { KeyTable } from '@/input/bindings';

import { bindKeyboard } from './keyboard';

const unbinds: (() => void)[] = [];

afterEach(() => {
  for (const unbind of unbinds.splice(0)) {
    unbind();
  }
});

function setup(typing = false) {
  const target = { press: vi.fn(), release: vi.fn(), releaseAll: vi.fn() };
  const onAction = vi.fn();
  const state: { typing: boolean; keys: KeyTable } = { typing, keys: defaults };
  const unbind = bindKeyboard({
    target,
    keys: () => state.keys,
    typing: () => state.typing,
    onAction,
  });
  unbinds.push(unbind);
  const key = (type: 'keydown' | 'keyup', code: string, init: KeyboardEventInit = {}) => {
    const e = new KeyboardEvent(type, { code, cancelable: true, ...init });
    window.dispatchEvent(e);
    return e;
  };
  return { target, onAction, state, unbind, key };
}

type Key = ReturnType<typeof setup>['key'];

describe('bindKeyboard', () => {
  it.each([
    { code: 'ArrowRight', action: 'right' },
    { code: 'ArrowUp', action: 'up' },
    { code: 'Space', action: 'jump' },
  ])('$code presses $action and releases it, and the page does not scroll', ({ code, action }) => {
    const { target, key } = setup();
    const down = key('keydown', code);
    expect(target.press).toHaveBeenCalledWith(action, false);
    expect(down.defaultPrevented).toBe(true);
    key('keyup', code);
    expect(target.release).toHaveBeenCalledWith(action);
  });

  it.each([
    { name: 'a key no action takes', code: 'KeyV' },
    { name: 'a digit past the potion’s', code: 'Digit2' },
  ])('$name does nothing, and keeps its default', ({ code }) => {
    const { target, key } = setup();
    const down = key('keydown', code);
    const up = key('keyup', code);
    expect(target.press).not.toHaveBeenCalled();
    expect(target.release).not.toHaveBeenCalled();
    expect([down.defaultPrevented, up.defaultPrevented]).toEqual([false, false]);
  });

  it.each([
    { name: 'jump (Space)', code: 'Space' },
    { name: 'chat (Enter)', code: 'Enter' },
    { name: 'attack', code: 'KeyX' },
  ])(
    '$name is the game’s alone: a focused button is not pressed by its down or its up',
    ({ code }) => {
      const { key } = setup();
      const down = key('keydown', code);
      const up = key('keyup', code);
      expect([down.defaultPrevented, up.defaultPrevented]).toEqual([true, true]);
    },
  );

  it('attack is held like movement: its down presses it once, its repeats add nothing, its up releases it', () => {
    const { target, onAction, key } = setup();
    key('keydown', 'KeyX');
    key('keydown', 'KeyX', { repeat: true });
    key('keydown', 'KeyX', { repeat: true });
    expect(target.press.mock.calls).toEqual([['attack', false]]);
    key('keyup', 'KeyX');
    expect(target.release).toHaveBeenCalledWith('attack');
    expect(onAction).not.toHaveBeenCalled();
  });

  it.each([
    { code: 'KeyI', action: 'inventory' },
    { code: 'Enter', action: 'chat' },
    { code: 'KeyQ', action: 'skill1' },
    { code: 'KeyF', action: 'skill8' },
    { code: 'Digit1', action: 'hpPotion' },
  ])('$code reports $action once, not as a held key', ({ code, action }) => {
    const { target, onAction, state, key } = setup();
    key('keydown', code);
    key('keydown', code, { repeat: true });
    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onAction).toHaveBeenCalledWith(action);
    expect(target.press).not.toHaveBeenCalled();
    state.typing = true;
    key('keydown', code);
    expect(onAction).toHaveBeenCalledTimes(1);
  });

  it.each([
    { name: 'Ctrl+P', code: 'KeyP', init: { ctrlKey: true } },
    { name: 'Cmd+P', code: 'KeyP', init: { metaKey: true } },
    { name: 'Ctrl+I', code: 'KeyI', init: { ctrlKey: true } },
    { name: 'Alt+Left', code: 'ArrowLeft', init: { altKey: true } },
    { name: 'Cmd+Right', code: 'ArrowRight', init: { metaKey: true } },
  ])(
    '$name is the browser’s: nothing is pressed or reported, and its default is kept',
    ({ code, init }) => {
      const { target, onAction, key } = setup();
      const down = key('keydown', code, init);
      const up = key('keyup', code, init);
      expect(target.press).not.toHaveBeenCalled();
      expect(onAction).not.toHaveBeenCalled();
      expect([down.defaultPrevented, up.defaultPrevented]).toEqual([false, false]);
    },
  );

  it('a key let go while Ctrl is held is still released', () => {
    const { target, key } = setup();
    key('keydown', 'ArrowRight');
    key('keyup', 'ArrowRight', { ctrlKey: true });
    expect(target.release).toHaveBeenCalledWith('right');
  });

  it('letting go of Cmd releases every key, as macOS sends no key up for one let go while Cmd was held', () => {
    const { target, key } = setup();
    key('keydown', 'ArrowRight');
    key('keydown', 'MetaLeft', { key: 'Meta', metaKey: true });
    expect(target.releaseAll).not.toHaveBeenCalled();
    key('keyup', 'MetaLeft', { key: 'Meta' });
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
  });

  it.each([
    {
      name: 'Cmd let every key go',
      letGo: (key: Key) => key('keyup', 'MetaLeft', { key: 'Meta' }),
    },
    { name: 'the page lost focus', letGo: () => window.dispatchEvent(new Event('blur')) },
  ])('a key still held after $name is held again by its repeats, once', ({ letGo }) => {
    const { target, key } = setup();
    key('keydown', 'ArrowRight');
    letGo(key);
    key('keydown', 'ArrowRight', { repeat: true });
    key('keydown', 'ArrowRight', { repeat: true });
    expect(target.press.mock.calls).toEqual([
      ['right', false],
      ['right', true],
    ]); // held again, not pressed afresh
  });

  it('a held key repeats in the browser but presses once', () => {
    const { target, key } = setup();
    key('keydown', 'ArrowLeft');
    key('keydown', 'ArrowLeft', { repeat: true });
    key('keydown', 'ArrowLeft', { repeat: true });
    expect(target.press).toHaveBeenCalledTimes(1);
  });

  it('while typing, keys belong to the text field, but key ups still release', () => {
    const { target, state, key } = setup(true);
    const down = key('keydown', 'ArrowRight');
    expect(target.press).not.toHaveBeenCalled();
    expect(down.defaultPrevented).toBe(false);
    const up = key('keyup', 'ArrowRight');
    expect(target.release).toHaveBeenCalledWith('right');
    expect(up.defaultPrevented).toBe(false);
    state.typing = false;
    key('keydown', 'ArrowRight');
    expect(target.press).toHaveBeenCalledWith('right', false);
  });

  it('focus moving into a text field releases every key; moving anywhere else does not', () => {
    const { target, state } = setup();
    window.dispatchEvent(new FocusEvent('focusin'));
    expect(target.releaseAll).not.toHaveBeenCalled();
    state.typing = true;
    window.dispatchEvent(new FocusEvent('focusin'));
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
  });

  it('reads the table at each key, so keys bound anew take effect at once', () => {
    const { onAction, state, key } = setup();
    state.keys = { ...defaults, skill1: ['KeyV'], quests: ['KeyQ'] };
    key('keydown', 'KeyV');
    key('keydown', 'KeyQ');
    expect(onAction.mock.calls).toEqual([['skill1'], ['quests']]);
  });

  it('losing focus releases every key, and unbinding stops everything', () => {
    const { target, unbind, key } = setup();
    window.dispatchEvent(new Event('blur'));
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
    unbind();
    key('keydown', 'ArrowRight');
    window.dispatchEvent(new Event('blur'));
    expect(target.press).not.toHaveBeenCalled();
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
  });
});
