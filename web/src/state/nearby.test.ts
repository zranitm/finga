import { beforeEach, describe, expect, it } from 'vitest';

import { nearby } from './nearby';

describe('nearby store', () => {
  beforeEach(() => {
    nearby.reset();
  });

  it('keeps the players sorted by name and notifies only when the set changes', () => {
    let changes = 0;
    const off = nearby.subscribe(() => changes++);
    nearby.getState().setNearby([
      { id: 'b', name: 'Robin' },
      { id: 'a', name: 'Ayla' },
    ]);
    expect(nearby.getState().players.map((p) => p.name)).toEqual(['Ayla', 'Robin']);
    nearby.getState().setNearby([
      { id: 'a', name: 'Ayla' },
      { id: 'b', name: 'Robin' },
    ]);
    expect(changes).toBe(1);
    nearby.getState().setNearby([{ id: 'a', name: 'Ayla' }]);
    expect(changes).toBe(2);
    nearby.getState().clear();
    nearby.getState().clear();
    expect(changes).toBe(3);
    expect(nearby.getState().players).toEqual([]);
    off();
  });
});
