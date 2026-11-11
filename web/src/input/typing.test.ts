import { describe, expect, it } from 'vitest';

import { isTyping } from './typing';

describe('isTyping', () => {
  it.each([
    { name: 'a text input', make: () => document.createElement('input') },
    {
      name: 'a password input',
      make: () => Object.assign(document.createElement('input'), { type: 'password' }),
    },
    {
      name: 'a number input',
      make: () => Object.assign(document.createElement('input'), { type: 'number' }),
    },
    { name: 'a textarea', make: () => document.createElement('textarea') },
    { name: 'a select', make: () => document.createElement('select') },
    {
      name: 'an editable element',
      make: () => {
        const div = document.createElement('div');
        div.setAttribute('contenteditable', 'true');
        return div;
      },
    },
  ])('is true for $name', ({ make }) => {
    expect(isTyping(make())).toBe(true);
  });

  it.each([
    { name: 'no element', make: null },
    { name: 'a button', make: () => document.createElement('button') },
    {
      name: 'a checkbox',
      make: () => Object.assign(document.createElement('input'), { type: 'checkbox' }),
    },
    {
      name: 'a focusable window',
      make: () => Object.assign(document.createElement('div'), { tabIndex: -1 }),
    },
  ])('is false for $name', ({ make }) => {
    expect(isTyping(make?.() ?? null)).toBe(false);
  });

  it('is false for the page and the window, where a key goes with no field focused', () => {
    expect(isTyping(document.body)).toBe(false);
    expect(isTyping(window)).toBe(false);
  });
});
