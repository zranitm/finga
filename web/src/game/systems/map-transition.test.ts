import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { DiedSchema, MapChangeSchema, RespawnedSchema } from '@/protocol/messages';

import { initialTransition, transition } from './map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from './map-transition';

const me = 'c-ayla';

const died = (characterId: string, x: number, y: number): TransitionMsg => ({
  t: 'died',
  d: create(DiedSchema, { characterId, x, y }),
});
const respawned = (
  characterId: string,
  mapId: string,
  x: number,
  y: number,
  hp: number,
): TransitionMsg => ({
  t: 'respawned',
  d: create(RespawnedSchema, { characterId, mapId, x, y, hp }),
});
const mapChange = (mapId: string, x: number, y: number): TransitionMsg => ({
  t: 'mapChange',
  d: create(MapChangeSchema, { mapId, x, y }),
});

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
      died(me, 500, 608),
      respawned(me, 'town', 416, 608, 102),
      mapChange('town', 416, 608),
    ]);
    expect(actions).toEqual([
      { type: 'die' },
      { type: 'change-map', mapId: 'town', x: 416, y: 608 },
    ]);
    expect(state).toEqual({ dead: false, awaitingMap: false });
  });

  it('between the Respawned and the MapChange the player is no longer dead but waits for the map', () => {
    const { state } = run([died(me, 500, 608), respawned(me, 'town', 416, 608, 102)]);
    expect(state).toEqual({ dead: false, awaitingMap: true });
  });

  it('Respawned on the same map with no MapChange: respawn here (position and prediction only)', () => {
    const { state, actions } = run(
      [died(me, 500, 608), respawned(me, 'arena', 100, 608, 102)],
      'arena',
    );
    expect(actions).toEqual([{ type: 'die' }, { type: 'respawn-here', x: 100, y: 608, hp: 102 }]);
    expect(state).toEqual(initialTransition);
  });

  it.each([
    { name: 'another player dying', msg: died('c-robin', 1, 2) },
    { name: 'another player respawning', msg: respawned('c-robin', 'town', 1, 2, 50) },
  ])('$name changes nothing', ({ msg }) => {
    const { state, actions } = run([msg]);
    expect(actions).toEqual([]);
    expect(state).toBe(initialTransition);
  });

  it('a MapChange alone (a portal, C12) changes the map', () => {
    const { actions } = run([mapChange('cave', 64, 608)]);
    expect(actions).toEqual([{ type: 'change-map', mapId: 'cave', x: 64, y: 608 }]);
  });
});
