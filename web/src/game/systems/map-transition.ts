import type { Data } from '@/protocol/messages';

export interface TransitionState {
  /** Nothing is sent or predicted until Respawned. */
  readonly dead: boolean;
  /** Respawned on another map; MapChange and a Snapshot follow. */
  readonly awaitingMap: boolean;
}

export const initialTransition: TransitionState = { dead: false, awaitingMap: false };

export type TransitionMsg =
  | { readonly t: 'died'; readonly d: Data<'died'> }
  | { readonly t: 'respawned'; readonly d: Data<'respawned'> }
  | { readonly t: 'mapChange'; readonly d: Data<'mapChange'> };

export type TransitionAction =
  | { readonly type: 'die' }
  | { readonly type: 'respawn-here'; readonly x: number; readonly y: number; readonly hp: number }
  | { readonly type: 'change-map'; readonly mapId: string; readonly x: number; readonly y: number };

export interface Transitioned {
  readonly state: TransitionState;
  readonly actions: readonly TransitionAction[];
}

/** Other players' Died and Respawned change nothing here; their views follow the Deltas. */
export function transition(
  state: TransitionState,
  msg: TransitionMsg,
  me: string,
  currentMap: string,
): Transitioned {
  switch (msg.t) {
    case 'died':
      if (msg.d.characterId !== me) {
        return { state, actions: [] };
      }
      return { state: { ...state, dead: true }, actions: [{ type: 'die' }] };
    case 'respawned': {
      if (msg.d.characterId !== me) {
        return { state, actions: [] };
      }
      if (msg.d.mapId === currentMap) {
        return {
          state: { dead: false, awaitingMap: false },
          actions: [{ type: 'respawn-here', x: msg.d.x, y: msg.d.y, hp: msg.d.hp }],
        };
      }
      return { state: { dead: false, awaitingMap: true }, actions: [] };
    }
    case 'mapChange':
      return {
        state: { dead: false, awaitingMap: false },
        actions: [{ type: 'change-map', mapId: msg.d.mapId, x: msg.d.x, y: msg.d.y }],
      };
  }
}
