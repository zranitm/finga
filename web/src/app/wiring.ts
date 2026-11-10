// The single place net and state meet.
import { itemName, starMarks, starsOf } from '@/features/shared/items';
import { RequestError } from '@/net/socket';
import type { GameSocket, StatusEvent } from '@/net/socket';
import { autoAssign } from '@/progression/stats';
import { LootKind } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
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
      combatLog.getState().applyLevelUp(m.d);
      autoAssignPoints(socket, m.d);
    }),
    socket.on('statsChanged', (m) => {
      character.getState().applyStatsChanged(m.d);
    }),
    socket.on('expGained', (m) => {
      character.getState().applyExpGained(m.d);
      combatLog.getState().applyExpGained(m.d);
    }),
    socket.on('loot', (m) => {
      combatLog.getState().applyLoot(m.d);
      toastRareFind(m.d);
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

/** For a refused command with no req: an attack, a portal, a chat line. */
export function responseText(d: Data<'response'>): string {
  switch (d.code) {
    case 'unavailable':
      return 'The server is busy. Try again.';
    case 'rate_limited':
    case 'chat_rate_limited':
      return 'Too fast. Wait a moment.';
    default:
      return d.message || d.code || 'The server refused.';
  }
}

/** With the setting on, a level-up spends every unspent point by the class's split, as the window's button does. */
function autoAssignPoints(socket: GameSocket, d: Data<'levelUp'>): void {
  const c = d.character;
  const unspent = c?.sheet?.points?.unspent ?? 0;
  if (!c || unspent === 0 || !settings.getState().autoAssignPoints) {
    return;
  }
  socket
    .request('spendPoints', { expectedUnspent: unspent, points: autoAssign(c.class, unspent) })
    .catch((err: unknown) => {
      // points_changed: a later level-up's request spends them; without an answer, the badge still shows any left.
      if (
        err instanceof RequestError &&
        !['points_changed', 'timeout', 'disconnected'].includes(err.code)
      ) {
        toasts.getState().push(`Could not auto-assign your points: ${err.message}`);
      }
    });
}

const RARE_STARS = 4;

/** A collected item of 4★ and up, this player's or a party member's, also shows as a toast. */
function toastRareFind(d: Data<'loot'>): void {
  if (d.kind !== LootKind.COLLECTED || starsOf(d.template) < RARE_STARS) {
    return;
  }
  const who = d.ownerId === character.getState().character?.id ? 'You' : d.ownerName;
  toasts.getState().push(`${who} got ${itemName(d.template)} ${starMarks(d.template)}`, 'info');
}

export function leaveWorld(): void {
  character.getState().clear();
  inventory.getState().clear();
  party.getState().clear();
  chat.getState().clear();
  combatLog.getState().clear();
  session.getState().logOut();
}
