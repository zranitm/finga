import { beforeEach, describe, expect, it } from 'vitest';

import { MAX_TOASTS, toasts } from './toasts';

describe('toasts store', () => {
  beforeEach(() => {
    toasts.reset();
  });

  it('pushes with new ids, errors by default, and dismisses by id', () => {
    const a = toasts.getState().push('Your inventory is full.');
    const b = toasts.getState().push('Picked up', 'info');
    expect(toasts.getState().toasts).toEqual([
      { id: a, text: 'Your inventory is full.', kind: 'error' },
      { id: b, text: 'Picked up', kind: 'info' },
    ]);
    toasts.getState().dismiss(a);
    expect(toasts.getState().toasts.map((t) => t.id)).toEqual([b]);
    toasts.getState().dismiss(999); // unknown: nothing
    expect(toasts.getState().toasts).toHaveLength(1);
  });

  it('keeps the newest MAX_TOASTS, and clear empties the list', () => {
    for (let i = 0; i < MAX_TOASTS + 2; i++) {
      toasts.getState().push(`t${i}`);
    }
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['t2', 't3', 't4', 't5']);
    toasts.getState().clear();
    expect(toasts.getState().toasts).toEqual([]);
  });
});
