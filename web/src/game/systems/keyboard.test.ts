import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Preset } from '@/state/settings';

import { bindKeyboard } from './keyboard';

const unbinds: (() => void)[] = [];

afterEach(() => {
  for (const unbind of unbinds.splice(0)) {
    unbind();
  }
});

function setup(preset: Preset = 'classic', typing = false) {
  const target = { press: vi.fn(), release: vi.fn(), releaseAll: vi.fn() };
  const onAction = vi.fn();
  const state = { typing };
  const unbind = bindKeyboard({
    target,
    preset: () => preset,
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

describe('bindKeyboard', () => {
  it.each([
    { preset: 'classic' as const, code: 'ArrowRight', action: 'right' },
    { preset: 'classic' as const, code: 'ArrowUp', action: 'up' },
    { preset: 'classic' as const, code: 'Space', action: 'jump' },
    { preset: 'wasd' as const, code: 'KeyA', action: 'left' },
    { preset: 'wasd' as const, code: 'KeyS', action: 'down' },
    { preset: 'wasd' as const, code: 'Space', action: 'jump' },
  ])(
    '$code under $preset presses $action and releases it, and the page does not scroll',
    ({ preset, code, action }) => {
      const { target, key } = setup(preset);
      const down = key('keydown', code);
      expect(target.press).toHaveBeenCalledWith(action);
      expect(down.defaultPrevented).toBe(true);
      key('keyup', code);
      expect(target.release).toHaveBeenCalledWith(action);
    },
  );

  it.each([
    { name: 'a key not in the preset', code: 'KeyQ' },
    { name: 'the other preset’s key', code: 'KeyD' },
  ])('$name does nothing, and keeps its default', ({ code }) => {
    const { target, key } = setup('classic');
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
      const { key } = setup('classic');
      const down = key('keydown', code);
      const up = key('keyup', code);
      expect([down.defaultPrevented, up.defaultPrevented]).toEqual([true, true]);
    },
  );

  it.each([
    { code: 'KeyX', action: 'attack' },
    { code: 'KeyZ', action: 'pickup' },
    { code: 'KeyI', action: 'inventory' },
    { code: 'Enter', action: 'chat' },
  ])('$code under classic reports $action once, not as a movement key', ({ code, action }) => {
    const { target, onAction, state, key } = setup('classic');
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
      const { target, onAction, key } = setup('classic');
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

  it('a held key repeats in the browser but presses once', () => {
    const { target, key } = setup();
    key('keydown', 'ArrowLeft');
    key('keydown', 'ArrowLeft', { repeat: true });
    key('keydown', 'ArrowLeft', { repeat: true });
    expect(target.press).toHaveBeenCalledTimes(1);
  });

  it('while typing, keys belong to the text field, but key ups still release', () => {
    const { target, state, key } = setup('classic', true);
    const down = key('keydown', 'ArrowRight');
    expect(target.press).not.toHaveBeenCalled();
    expect(down.defaultPrevented).toBe(false);
    const up = key('keyup', 'ArrowRight');
    expect(target.release).toHaveBeenCalledWith('right');
    expect(up.defaultPrevented).toBe(false);
    state.typing = false;
    key('keydown', 'ArrowRight');
    expect(target.press).toHaveBeenCalledWith('right');
  });

  it('focus moving into a text field releases every key; moving anywhere else does not', () => {
    const { target, state } = setup();
    window.dispatchEvent(new FocusEvent('focusin'));
    expect(target.releaseAll).not.toHaveBeenCalled();
    state.typing = true;
    window.dispatchEvent(new FocusEvent('focusin'));
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
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
