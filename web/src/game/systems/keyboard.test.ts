import { describe, expect, it, vi } from 'vitest';

import type { Preset } from '@/state/settings';

import { bindKeyboard } from './keyboard';

function setup(preset: Preset = 'classic', captured = false) {
  const source = document.createElement('div');
  const target = { press: vi.fn(), release: vi.fn(), releaseAll: vi.fn() };
  const state = { captured };
  const unbind = bindKeyboard({
    target,
    preset: () => preset,
    captured: () => state.captured,
    source,
    focusSource: source,
  });
  const key = (type: 'keydown' | 'keyup', code: string, repeat = false) => {
    const e = new KeyboardEvent(type, { code, repeat, cancelable: true });
    source.dispatchEvent(e);
    return e;
  };
  return { source, target, state, unbind, key };
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
    { name: 'a non-movement action (attack)', code: 'KeyX' },
    { name: 'the other preset’s key', code: 'KeyD' },
  ])('$name does nothing', ({ code }) => {
    const { target, key } = setup('classic');
    const down = key('keydown', code);
    key('keyup', code);
    expect(target.press).not.toHaveBeenCalled();
    expect(target.release).not.toHaveBeenCalled();
    expect(down.defaultPrevented).toBe(false);
  });

  it('a held key repeats in the browser but presses once', () => {
    const { target, key } = setup();
    key('keydown', 'ArrowLeft');
    key('keydown', 'ArrowLeft', true);
    key('keydown', 'ArrowLeft', true);
    expect(target.press).toHaveBeenCalledTimes(1);
  });

  it('while captured, key downs belong to the text field but key ups still release', () => {
    const { target, state, key } = setup('classic', true);
    const down = key('keydown', 'ArrowRight');
    expect(target.press).not.toHaveBeenCalled();
    expect(down.defaultPrevented).toBe(false);
    key('keyup', 'ArrowRight');
    expect(target.release).toHaveBeenCalledWith('right');
    state.captured = false;
    key('keydown', 'ArrowRight');
    expect(target.press).toHaveBeenCalledWith('right');
  });

  it('losing focus releases every key, and unbinding stops everything', () => {
    const { source, target, unbind, key } = setup();
    source.dispatchEvent(new Event('blur'));
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
    unbind();
    key('keydown', 'ArrowRight');
    source.dispatchEvent(new Event('blur'));
    expect(target.press).not.toHaveBeenCalled();
    expect(target.releaseAll).toHaveBeenCalledTimes(1);
  });
});
