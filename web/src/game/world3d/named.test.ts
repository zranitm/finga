import { describe, expect, it } from 'vitest';

import { Named } from './named';

/** n players in a row east of the target, 1 m apart from 1 m out. */
function row(n: number): Map<string, { x: number; z: number }> {
  return new Map(Array.from({ length: n }, (_, i) => [`p${i}`, { x: i + 1, z: 0 }]));
}

describe('the players named', () => {
  const target = { x: 0, z: 0 };

  it('names everyone shown while they’re no more than the limit', () => {
    const named = new Named(3);
    named.pick(row(3), target);
    expect(['p0', 'p1', 'p2', 'nobody'].map((id) => named.has(id))).toEqual([
      true,
      true,
      true,
      false,
    ]);
  });

  it('names the limit nearest past it, and a newcomer takes a name only once it comes 2 m nearer than the one it would take it from', () => {
    const named = new Named(3);
    const shown = row(4); // p3 at 4 m, the 3rd named at 3 m
    named.pick(shown, target);
    expect(['p0', 'p1', 'p2', 'p3'].map((id) => named.has(id))).toEqual([true, true, true, false]);
    shown.set('p3', { x: 2.5, z: 0 }); // nearer than p2, by less than 2 m
    named.pick(shown, target);
    expect([named.has('p2'), named.has('p3')]).toEqual([true, false]);
    shown.set('p3', { x: 0.9, z: 0 }); // 2.1 m nearer than p2
    named.pick(shown, target);
    expect([named.has('p2'), named.has('p3')]).toEqual([false, true]);
    shown.set('p2', { x: 1.5, z: 0 }); // p2 back, nearer than p1 by 0.5 m: not enough to take p1's
    named.pick(shown, target);
    expect(['p0', 'p1', 'p2', 'p3'].map((id) => named.has(id))).toEqual([true, true, false, true]);
  });

  it('crossing the limit from everyone named, holds every name as it was', () => {
    const named = new Named(3);
    const shown = row(3);
    named.pick(shown, target);
    shown.set('p3', { x: 2.5, z: 0 }); // a fourth comes in 0.5 m nearer than the 3rd
    named.pick(shown, target);
    expect(['p0', 'p1', 'p2', 'p3'].map((id) => named.has(id))).toEqual([true, true, true, false]);
  });
});
