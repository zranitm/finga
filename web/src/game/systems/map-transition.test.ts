import { describe, expect, it } from 'vitest';

import { initialTransition, transition } from './map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from './map-transition';

const me = 'c-ayla';

function run(msgs: readonly TransitionMsg[], map = 'forest') {
  let state: TransitionState = initialTransition;
  const actions: TransitionAction[] = [];
  for (const m of msgs) {
    const r = transition(state, m, me, map);
    state = r.state;
    actions.push(...r.actions);
  }
  return { state, actions };
}

describe('transition', () => {
  it('Died followed by Respawned on another map and MapChange: die, then change the map (prediction and buffers reset there)', () => {
    const { state, actions } = run([
      { t: 'Died', d: { character_id: me, x: 500, y: 608 } },
      { t: 'Respawned', d: { character_id: me, map_id: 'town', x: 416, y: 608, hp: 102 } },
      { t: 'MapChange', d: { map_id: 'town', x: 416, y: 608 } },
    ]);
    expect(actions).toEqual([
      { type: 'die' },
      { type: 'change-map', mapId: 'town', x: 416, y: 608 },
    ]);
    expect(state).toEqual({ dead: false, awaitingMap: false });
  });

  it('between the Respawned and the MapChange the player is no longer dead but waits for the map', () => {
    const { state } = run([
      { t: 'Died', d: { character_id: me, x: 500, y: 608 } },
      { t: 'Respawned', d: { character_id: me, map_id: 'town', x: 416, y: 608, hp: 102 } },
    ]);
    expect(state).toEqual({ dead: false, awaitingMap: true });
  });

  it('Respawned on the same map with no MapChange: respawn here (position and prediction only)', () => {
    const { state, actions } = run(
      [
        { t: 'Died', d: { character_id: me, x: 500, y: 608 } },
        { t: 'Respawned', d: { character_id: me, map_id: 'arena', x: 100, y: 608, hp: 102 } },
      ],
      'arena',
    );
    expect(actions).toEqual([{ type: 'die' }, { type: 'respawn-here', x: 100, y: 608, hp: 102 }]);
    expect(state).toEqual(initialTransition);
  });

  it.each([
    {
      name: 'another player dying',
      msg: { t: 'Died', d: { character_id: 'c-robin', x: 1, y: 2 } } as const,
    },
    {
      name: 'another player respawning',
      msg: {
        t: 'Respawned',
        d: { character_id: 'c-robin', map_id: 'town', x: 1, y: 2, hp: 50 },
      } as const,
    },
  ])('$name changes nothing', ({ msg }) => {
    const { state, actions } = run([msg]);
    expect(actions).toEqual([]);
    expect(state).toBe(initialTransition);
  });

  it('a MapChange alone (a portal, C12) changes the map', () => {
    const { actions } = run([{ t: 'MapChange', d: { map_id: 'cave', x: 64, y: 608 } }]);
    expect(actions).toEqual([{ type: 'change-map', mapId: 'cave', x: 64, y: 608 }]);
  });
});
