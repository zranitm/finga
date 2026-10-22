// The single place net and state meet (page 7): incoming messages become
// store actions, and the socket's status becomes the connection store. Each
// side is tested alone; this file is tested with the fake socket.
import type { GameSocket, StatusEvent } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { toasts } from '@/state/toasts';

/** Subscribes the stores to the socket; returns the function that undoes it. */
export function wireSocket(socket: GameSocket): () => void {
  const offs = [
    socket.on('Welcome', (m) => {
      character.getState().applyWelcome(m.d);
      inventory.getState().applyWelcome(m.d);
      party.getState().applyWelcome(m.d);
    }),
    socket.on('InventoryChanged', (m) => {
      inventory.getState().applyInventoryChanged(m.d);
    }),
    socket.on('PartyState', (m) => {
      party.getState().applyPartyState(m.d);
    }),
    socket.on('PartyInvited', (m) => {
      party.getState().applyPartyInvited(m.d);
    }),
    socket.on('ChatMessage', (m) => {
      chat.getState().applyChatMessage(m.d);
    }),
    socket.on('LevelUp', (m) => {
      character.getState().applyLevelUp(m.d);
    }),
    socket.on('ExpGained', (m) => {
      character.getState().applyExpGained(m.d);
    }),
    socket.on('Pong', () => {
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

/** The socket's status into the connection store; a close that ends the session logs out. */
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
      // Kicked, the session ended, an outdated client or a bad message: the
      // world is over; back to the login screen with the reason shown.
      c.setConnection('kicked', { ...(e.kick && { kick: e.kick }) });
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

/** The words for a refused command with no request ID (a pickup, a chat line). */
export function responseText(d: Data<'Response'>): string {
  switch (d.code) {
    case 'inventory_full':
      return 'Your inventory is full.';
    case 'unavailable':
      return 'The server is busy. Try again.';
    case 'rate_limited':
      return 'Too fast. Wait a moment.';
    default:
      return d.message ?? d.code ?? 'The server refused.';
  }
}

/** Forgets the character, inventory, party and chat, and logs out. */
export function leaveWorld(): void {
  character.getState().clear();
  inventory.getState().clear();
  party.getState().clear();
  chat.getState().clear();
  session.getState().logOut();
}
