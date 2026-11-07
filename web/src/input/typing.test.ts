import { afterEach, describe, expect, it } from 'vitest';

import { isTyping } from './typing';

describe('isTyping', () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  const focus = (el: HTMLElement) => {
    document.body.append(el);
    el.focus();
  };

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
        const div = Object.assign(document.createElement('div'), { tabIndex: 0 }); // jsdom focuses only what has a tab index
        div.setAttribute('contenteditable', 'true');
        return div;
      },
    },
  ])('is true while $name has focus', ({ make }) => {
    focus(make());
    expect(isTyping()).toBe(true);
  });

  it.each([
    { name: 'nothing (the page itself)', make: null },
    { name: 'a button', make: () => document.createElement('button') },
    {
      name: 'a checkbox',
      make: () => Object.assign(document.createElement('input'), { type: 'checkbox' }),
    },
    {
      name: 'a focusable window',
      make: () => Object.assign(document.createElement('div'), { tabIndex: -1 }),
    },
  ])('is false while $name has focus', ({ make }) => {
    if (make) {
      focus(make());
    }
    expect(isTyping()).toBe(false);
  });

  it('follows focus as it leaves, even when the field goes without a blur', () => {
    const field = document.createElement('input');
    focus(field);
    expect(isTyping()).toBe(true);
    field.remove();
    expect(isTyping()).toBe(false);
  });
});
