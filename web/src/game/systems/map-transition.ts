// Death, respawn and map change as a small state machine over the
// server's messages (C10), reused by portals (C12): the scene applies the
// actions it returns, which is where prediction history, interpolation
// buffers and the scene itself get reset.
import type { Data } from '@/protocol/messages';

export interface TransitionState {
  /** The local player is dead: nothing is sent or predicted until Respawned. */
  readonly dead: boolean;
  /** A Respawned on another map was seen; MapChange and a Snapshot follow. */
  readonly awaitingMap: boolean;
}

export const initialTransition: TransitionState = { dead: false, awaitingMap: false };

export type TransitionMsg =
  | { readonly t: 'Died'; readonly d: Data<'Died'> }
  | { readonly t: 'Respawned'; readonly d: Data<'Respawned'> }
  | { readonly t: 'MapChange'; readonly d: Data<'MapChange'> };

export type TransitionAction =
  | { readonly type: 'die' }
  | { readonly type: 'respawn-here'; readonly x: number; readonly y: number; readonly hp: number }
  | { readonly type: 'change-map'; readonly mapId: string; readonly x: number; readonly y: number };

export interface Transitioned {
  readonly state: TransitionState;
  readonly actions: readonly TransitionAction[];
}

/**
 * Applies one message for the local player (`me`) on `currentMap`. Other
 * players' Died and Respawned change nothing here; their views follow the
 * Deltas.
 */
export function transition(
  state: TransitionState,
  msg: TransitionMsg,
  me: string,
  currentMap: string,
): Transitioned {
  switch (msg.t) {
    case 'Died':
      if (msg.d.character_id !== me) {
        return { state, actions: [] };
      }
      return { state: { ...state, dead: true }, actions: [{ type: 'die' }] };
    case 'Respawned': {
      if (msg.d.character_id !== me) {
        return { state, actions: [] };
      }
      if (msg.d.map_id === currentMap) {
        return {
          state: { dead: false, awaitingMap: false },
          actions: [{ type: 'respawn-here', x: msg.d.x, y: msg.d.y, hp: msg.d.hp }],
        };
      }
      return { state: { dead: false, awaitingMap: true }, actions: [] };
    }
    case 'MapChange':
      return {
        state: { dead: false, awaitingMap: false },
        actions: [{ type: 'change-map', mapId: msg.d.map_id, x: msg.d.x, y: msg.d.y }],
      };
  }
}
