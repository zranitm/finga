import { describe, expect, it } from 'vitest';

import { actionFor, actions, heldInput, isMovement } from './actions';
import { defaults } from './bindings';

// key-bindings.md section 2.
const table: Readonly<Record<string, string>> = {
  left: 'ArrowLeft',
  right: 'ArrowRight',
  up: 'ArrowUp',
  down: 'ArrowDown',
  jump: 'Space',
  attack: 'KeyX',
  nextTarget: 'Tab',
  talk: 'KeyZ',
  hpPotion: 'Digit1',
  skill1: 'KeyQ',
  skill2: 'KeyW',
  skill3: 'KeyE',
  skill4: 'KeyR',
  skill5: 'KeyA',
  skill6: 'KeyS',
  skill7: 'KeyD',
  skill8: 'KeyF',
  inventory: 'KeyI',
  character: 'KeyC',
  skills: 'KeyK',
  party: 'KeyP',
  quests: 'KeyJ',
  map: 'KeyM',
  arena: 'KeyV',
  zoomIn: 'Equal',
  zoomOut: 'Minus',
  chat: 'Enter',
  close: 'Escape',
};

describe('the default keys', () => {
  it('map every action as key-bindings.md section 2 lists it, the skills on Q W E R A S D F', () => {
    for (const action of actions) {
      expect(actionFor(table[action] ?? '', defaults), action).toBe(action);
    }
    expect(Object.keys(defaults).sort()).toEqual([...actions].sort());
  });

  it('a key no action takes maps to nothing: the old digits past the potion, and the old WASD movement keys', () => {
    for (const code of ['Digit2', 'Digit6', 'KeyG', 'KeyB']) {
      expect(actionFor(code, defaults), code).toBeUndefined();
    }
  });

  it('no key maps to two actions', () => {
    const codes = Object.values(defaults).flat();
    expect(new Set(codes).size).toBe(codes.length);
  });

  it('the numpad Enter opens chat too', () => {
    expect(actionFor('NumpadEnter', defaults)).toBe('chat');
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
});
