import { describe, expect, it } from 'vitest';

import { actionFor, actions, heldInput, isMovement } from './actions';
import { keyLabel, presets } from './presets';
import type { Preset } from './presets';

// The table from Screens and controls.
const table: Record<Preset, Record<string, string>> = {
  classic: {
    left: 'ArrowLeft',
    right: 'ArrowRight',
    up: 'ArrowUp',
    down: 'ArrowDown',
    jump: 'Space',
    attack: 'KeyX',
    pickup: 'KeyZ',
    inventory: 'KeyI',
    party: 'KeyP',
    chat: 'Enter',
  },
  wasd: {
    left: 'KeyA',
    right: 'KeyD',
    up: 'KeyW',
    down: 'KeyS',
    jump: 'Space',
    attack: 'KeyJ',
    pickup: 'KeyK',
    inventory: 'KeyI',
    party: 'KeyP',
    chat: 'Enter',
  },
};

describe('key presets', () => {
  it.each(['classic', 'wasd'] as const)(
    '%s maps every action in the Screens and controls table',
    (preset) => {
      for (const action of actions) {
        expect(actionFor(table[preset][action] ?? '', preset), action).toBe(action);
      }
      expect(Object.keys(presets[preset]).sort()).toEqual([...actions].sort());
    },
  );

  it.each([
    { preset: 'classic' as const, code: 'KeyA' },
    { preset: 'wasd' as const, code: 'ArrowLeft' },
    { preset: 'classic' as const, code: 'KeyQ' },
    { preset: 'wasd' as const, code: 'Escape' },
  ])('$preset: $code maps to nothing', ({ preset, code }) => {
    expect(actionFor(code, preset)).toBeUndefined();
  });

  it('no key maps to two actions within a preset', () => {
    for (const preset of ['classic', 'wasd'] as const) {
      const codes = Object.values(presets[preset]).flat();
      expect(new Set(codes).size).toBe(codes.length);
    }
  });

  it('the numpad Enter opens chat too', () => {
    expect(actionFor('NumpadEnter', 'classic')).toBe('chat');
  });

  it('held actions become the sim input; commands are not movement', () => {
    expect(heldInput(new Set(['left', 'jump', 'attack']))).toEqual({
      left: true,
      right: false,
      up: false,
      down: false,
      jump: true,
    });
    expect(heldInput(new Set())).toEqual({
      left: false,
      right: false,
      up: false,
      down: false,
      jump: false,
    });
    expect(actions.filter(isMovement)).toEqual(['left', 'right', 'up', 'down', 'jump']);
  });

  it.each([
    { preset: 'classic' as const, action: 'attack' as const, label: 'X' },
    { preset: 'classic' as const, action: 'left' as const, label: 'Left' },
    { preset: 'wasd' as const, action: 'pickup' as const, label: 'K' },
    { preset: 'wasd' as const, action: 'jump' as const, label: 'Space' },
  ])('$preset $action is labelled $label', ({ preset, action, label }) => {
    expect(keyLabel(preset, action)).toBe(label);
  });
});
