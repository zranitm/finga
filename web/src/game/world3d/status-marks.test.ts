import { describe, expect, it } from 'vitest';

import type { Fx } from '../effects/fx';
import type { Body, PlayOptions } from '../effects/types';

import { StatusMarks } from './status-marks';

/** Marks played on a stand-in for fx: each one's effect, whom it follows, and whether it was stopped. */
function marks() {
  const played: { id: string; o: PlayOptions; stopped: boolean }[] = [];
  const fx = {
    play: (id: string, o: PlayOptions) => {
      const m = { id, o, stopped: false };
      played.push(m);
      return () => {
        m.stopped = true;
      };
    },
  } as unknown as Fx;
  const bodies = new Map<string, Body>();
  const shown = new StatusMarks(fx, (r) => bodies.get(`${r.kind === 'mob' ? 'm' : 'p'}:${r.id}`));
  const running = () =>
    played.filter((m) => !m.stopped).map((m) => [m.id, m.o.follow?.()?.x ?? null]);
  return { shown, bodies, running };
}

const at = (x: number): Body => ({ x, y: 0, z: 0, radius: 0.5, height: 1 });

describe('StatusMarks', () => {
  it('marks a unit only once it’s drawn, then each status listed once, following its body', () => {
    const { shown, bodies, running } = marks();
    const units = new Map([
      [
        'm:1',
        [
          { id: 'poison', ends: 40 },
          { id: 'slow', ends: 30 },
        ],
      ],
    ]);
    shown.show(units);
    expect(running()).toEqual([]);
    bodies.set('m:1', at(6));
    shown.show(units);
    shown.show(
      new Map([
        [
          'm:1',
          [
            { id: 'poison', ends: 50 },
            { id: 'slow', ends: 30 },
          ],
        ],
      ]),
    );
    bodies.set('m:1', at(7));
    expect(running()).toEqual([
      ['status.poison', 7],
      ['status.slow', 7],
    ]);
  });

  it('stops a mark as its status goes from the list, all of a unit’s as it goes, and every one on clear', () => {
    const { shown, bodies, running } = marks();
    bodies.set('m:1', at(6));
    bodies.set('p:c-robin', at(9));
    shown.show(
      new Map([
        [
          'm:1',
          [
            { id: 'poison', ends: 40 },
            { id: 'slow', ends: 30 },
          ],
        ],
        ['p:c-robin', [{ id: 'stun', ends: 30 }]],
      ]),
    );
    shown.show(
      new Map([
        ['m:1', [{ id: 'poison', ends: 40 }]],
        ['p:c-robin', [{ id: 'stun', ends: 30 }]],
      ]),
    );
    expect(running()).toEqual([
      ['status.poison', 6],
      ['status.stun', 9],
    ]);
    shown.show(new Map([['p:c-robin', [{ id: 'stun', ends: 30 }]]]));
    expect(running()).toEqual([['status.stun', 9]]);
    shown.clear();
    expect(running()).toEqual([]);
  });
});
