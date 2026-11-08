// The single place net and state meet.
import type { GameSocket, StatusEvent } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { toasts } from '@/state/toasts';

export function wireSocket(socket: GameSocket): () => void {
  const offs = [
    socket.on('welcome', (m) => {
      character.getState().applyWelcome(m.d);
      inventory.getState().applyWelcome(m.d);
      party.getState().applyWelcome();
    }),
    socket.on('inventoryChanged', (m) => {
      inventory.getState().applyInventoryChanged(m.d);
    }),
    socket.on('partyState', (m) => {
      party.getState().applyPartyState(m.d);
    }),
    socket.on('partyInvited', (m) => {
      party.getState().applyPartyInvited(m.d);
    }),
    socket.on('chatMessage', (m) => {
      chat.getState().applyChatMessage(m.d);
    }),
    socket.on('levelUp', (m) => {
      character.getState().applyLevelUp(m.d);
    }),
    socket.on('statsChanged', (m) => {
      character.getState().applyStatsChanged(m.d);
    }),
    socket.on('expGained', (m) => {
      character.getState().applyExpGained(m.d);
    }),
    socket.on('pong', () => {
      connection.getState().setRtt(socket.rtt);
    }),
    socket.onStatus(applyStatus),
    socket.onUnmatchedResponse((d) => {
      if (!d.ok) {
        toasts.getState().push(responseText(d));
      }
    }),
  ];
  return () => {
    for (const off of offs) {
      off();
    }
  };
}

/** A close that ends the session leaves the world. */
export function applyStatus(e: StatusEvent): void {
  const c = connection.getState();
  switch (e.status) {
    case 'connecting':
      c.setConnection('connecting');
      return;
    case 'connected':
      c.setConnection('connected');
      return;
    case 'reconnecting':
      c.setConnection('reconnecting', { attempt: e.attempt ?? 0 });
      return;
    case 'closed':
      if (e.reason === 'user' || e.reason === 'no_token') {
        c.setConnection('disconnected');
        return;
      }
      c.setConnection('kicked', e.kick === undefined ? {} : { kick: e.kick });
      leaveWorld();
      if (e.reason === 'outdated_protocol' && typeof location !== 'undefined') {
        location.reload();
      }
      return;
    case 'idle':
      c.setConnection('disconnected');
      return;
  }
}

/** For a refused command with no req: a pickup, a chat line. */
export function responseText(d: Data<'response'>): string {
  switch (d.code) {
    case 'inventory_full':
      return 'Your inventory is full.';
    case 'pickup_busy':
    case 'unavailable':
      return 'The server is busy. Try again.';
    case 'rate_limited':
    case 'chat_rate_limited':
      return 'Too fast. Wait a moment.';
    default:
      return d.message || d.code || 'The server refused.';
  }
}

export function leaveWorld(): void {
  character.getState().clear();
  inventory.getState().clear();
  party.getState().clear();
  chat.getState().clear();
  session.getState().logOut();
}
