import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, getToken, setToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import {
  ChatChannel,
  KickReason,
  LootKind,
  MatchEndedSchema,
  MatchOutcome,
  MatchPhase,
  PvpOfferGoneReason,
  PvpUnqueueReason,
  QuestChange,
  QuestOfferState,
  QuestStatus,
  SkillTimerKind,
  TeleportCancelReason,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combat } from '@/state/combat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { inventory } from '@/state/inventory';
import { mapView } from '@/state/map-view';
import { party } from '@/state/party';
import { pouches } from '@/state/pouches';
import { pvp } from '@/state/pvp';
import { quests } from '@/state/quests';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { applyStatus, leaveCharacter, wireSocket } from './wiring';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  maxHp: 204,
  energy: 7,
};
const potion = { itemId: 'i1', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };
const gel = {
  mapId: 'meadow',
  pouchId: 7,
  ownerId: 'c-ayla',
  ownerName: 'Ayla',
  items: [{ template: 'slime_gel', quantity: 1 }],
};
const welcome: ServerInit<'welcome'> = {
  token: 'tok-rotated',
  character: ayla,
  inventory: [potion],
  mapId: 'town',
  tick: 1,
};

async function connectedSocket(): Promise<{ socket: GameSocket; ws: FakeWebSocket }> {
  setToken('tok-login');
  const socket = new GameSocket({ url: 'ws://game.test/ws' });
  wireSocket(socket);
  const p = socket.connect('c-ayla');
  const ws = FakeWebSocket.latest();
  ws.open();
  ws.push('welcome', welcome);
  await p;
  return { socket, ws };
}

describe('wiring', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    resetStores();
    session.getState().logIn({ id: 'alice', username: 'alice' });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    clearToken();
  });

  it('Welcome lands in the character and inventory stores, leaves the party to the PartyState after it, and connects', async () => {
    await connectedSocket();
    expect(character.getState().character?.name).toBe('Ayla');
    expect(character.getState().expToNext).toBe(20);
    expect(inventory.getState().slots.use[0]).toMatchObject(potion);
    expect(party.getState().party).toBeNull();
    expect(connection.getState().status).toBe('connected');
    expect(getToken()).toBe('tok-rotated');
  });

  it('energy comes from the Welcome, then each Delta as of its ack; a Snapshot drops the presses before it, and leaving clears it', async () => {
    const { ws } = await connectedSocket();
    expect(energy.getState().shown).toBe(7);
    ws.push('delta', { ackSeq: 3, energy: 4 }, { tick: 2 });
    expect(energy.getState().shown).toBe(4);
    energy.getState().spend(5, 2);
    ws.push('delta', { ackSeq: 4 }, { tick: 3 }); // before the press: it stays taken off
    expect(energy.getState().shown).toBe(2);
    ws.push('snapshot', {});
    expect(energy.getState().shown).toBe(4);
    leaveCharacter();
    expect(energy.getState()).toMatchObject({ told: 0, pending: [], shown: 0 });
  });

  it.each([
    {
      name: 'InventoryChanged',
      push: (ws: FakeWebSocket) =>
        ws.push('inventoryChanged', {
          slots: [
            { tab: 'use', slot: 0 },
            { tab: 'use', slot: 3, item: { ...potion, slot: 3 } },
          ],
        }),
      check: () => expect(inventory.getState().slots.use[3]?.slot).toBe(3),
    },
    {
      name: 'PartyState',
      push: (ws: FakeWebSocket) => ws.push('partyState', {}),
      check: () => expect(party.getState().party).toBeNull(),
    },
    {
      name: 'PartySpots',
      push: (ws: FakeWebSocket) =>
        ws.push('partySpots', { spots: [{ characterId: 'c-robin', x: 140, z: 812 }] }),
      check: () => expect(party.getState().spots).toEqual({ 'c-robin': { x: 140, z: 812 } }),
    },
    {
      name: 'PartyInvited',
      push: (ws: FakeWebSocket) =>
        ws.push('partyInvited', { inviteId: 'inv-1', from: 'c-robin', fromName: 'Robin' }),
      check: () => expect(party.getState().invites[0]?.fromName).toBe('Robin'),
    },
    {
      name: 'ChatMessage, heard in the region channel the player is in',
      push: (ws: FakeWebSocket) => {
        mapView.getState().inChannel(2);
        ws.push('chatMessage', {
          channel: ChatChannel.MAP,
          sender: 'c-robin',
          senderName: 'Robin',
          text: 'hi',
        });
      },
      check: () => expect(chat.getState().lines[0]).toMatchObject({ text: 'hi', heardIn: 2 }),
    },
    {
      name: 'LevelUp',
      push: (ws: FakeWebSocket) =>
        ws.push('levelUp', { character: { ...ayla, level: 2, exp: 0n } }),
      check: () =>
        expect(character.getState()).toMatchObject({ character: { level: 2 }, expToNext: 65 }),
    },
    {
      name: 'StatsChanged',
      push: (ws: FakeWebSocket) =>
        ws.push('statsChanged', {
          sheet: { derivedWorn: { physicalDefense: 4 } },
          hp: 204,
          maxHp: 234,
        }),
      check: () =>
        expect(character.getState().character).toMatchObject({
          maxHp: 234,
          sheet: { derivedWorn: { physicalDefense: 4 } },
        }),
    },
    {
      name: 'InventoryChanged with equipment',
      push: (ws: FakeWebSocket) =>
        ws.push('inventoryChanged', {
          equipment: [
            { equipSlot: 'hat', item: { itemId: 'i-cap', template: 'leather_cap', quantity: 1 } },
          ],
          revision: 2n,
        }),
      check: () => expect(inventory.getState().equipment.hat?.itemId).toBe('i-cap'),
    },
    {
      name: 'ExpGained',
      push: (ws: FakeWebSocket) => ws.push('expGained', { amount: 5n, exp: 15n }),
      check: () => expect(character.getState().character?.exp).toBe(15n),
    },
    {
      name: 'Pong',
      push: (ws: FakeWebSocket) =>
        ws.push('pong', { clientTime: BigInt(Date.now() - 30), serverTime: 1n }),
      check: () => expect(connection.getState().rtt).toBeGreaterThanOrEqual(30),
    },
  ])('a pushed $name lands in its store', async ({ push, check }) => {
    const { ws } = await connectedSocket();
    push(ws);
    check();
  });

  it('a field boss comes from a Snapshot quietly, and back from its message with a toast and a [Notice] line; beaten, it counts down', async () => {
    const { ws } = await connectedSocket();
    ws.push('snapshot', { fieldBoss: { mobId: 9, type: 'king_slime' } });
    expect(fieldBoss.getState().boss).toMatchObject({ mobId: 9, type: 'king_slime' });
    ws.push('fieldBossGone', { mobId: 9, type: 'king_slime', backInMs: 300000n });
    expect(fieldBoss.getState()).toMatchObject({ boss: null, down: { type: 'king_slime' } });
    ws.push('fieldBoss', { mobId: 12, type: 'king_slime' });
    expect(fieldBoss.getState()).toMatchObject({ boss: { mobId: 12 }, down: null });
    const text = 'King Slime has appeared!';
    expect(toasts.getState().toasts).toMatchObject([{ text, kind: 'info' }]);
    expect(chat.getState().lines).toMatchObject([{ kind: 'notice', text }]);
    ws.push('snapshot', { fieldBossGone: { mobId: 12, type: 'king_slime', backInMs: 60000n } });
    expect(fieldBoss.getState()).toMatchObject({ boss: null, down: { type: 'king_slime' } });
    ws.push('snapshot', {});
    expect(fieldBoss.getState()).toMatchObject({ boss: null, down: null });
    expect(toasts.getState().toasts).toHaveLength(1);
  });

  it.each([
    [TeleportCancelReason.MOVED, ['Teleport cancelled: you moved.']],
    [TeleportCancelReason.ACTED, ['Teleport cancelled: you attacked.']],
    [TeleportCancelReason.HIT, ['Teleport cancelled: you were hit.']],
    [TeleportCancelReason.DIED, []], // the death screen says it
  ])('a teleport cancelled for reason %i ends the channel and says why', async (reason, said) => {
    const { ws } = await connectedSocket();
    teleport.getState().start({ map: 'forest' });
    ws.push('teleportCancelled', { reason });
    expect(teleport.getState().to).toBeNull();
    expect(toasts.getState().toasts.map((t) => [t.text, t.kind])).toEqual(
      said.map((text) => [text, 'info']),
    );
  });

  it('a switch of channels cancelled says so, though the bar went with the key the server cancelled it for', async () => {
    const { ws } = await connectedSocket();
    teleport.getState().start({ channel: 2 });
    teleport.getState().end(); // the movement key ends the bar at once
    ws.push('teleportCancelled', { reason: TeleportCancelReason.MOVED });
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual([
      'Channel switch cancelled: you moved.',
    ]);
  });

  it('each Snapshot sets the player’s channel: a region’s number, 0 off regions', async () => {
    const { ws } = await connectedSocket();
    ws.push('snapshot', { channel: 2 });
    expect(mapView.getState().channel).toBe(2);
    ws.push('snapshot', {});
    expect(mapView.getState().channel).toBe(0);
  });

  it('a map change, or leaving the character, ends the channel with the old map', async () => {
    const { ws } = await connectedSocket();
    teleport.getState().start({ map: 'forest' });
    ws.push('mapChange', { mapId: 'forest' });
    expect(teleport.getState().to).toBeNull();
    teleport.getState().start({ map: 'cave' });
    leaveCharacter();
    expect(teleport.getState().to).toBeNull();
  });

  it('the queue, an offer, a match and its end land in the PvP store; a lapse, a party change or an offer that fell through say so; the trip back leaves the match, its outcome kept', async () => {
    const { ws } = await connectedSocket();
    const said = () => toasts.getState().toasts.map((t) => t.text);
    ws.push('pvpQueued', { mode: 'duel', sinceMs: 2000 });
    expect(pvp.getState().queued?.mode).toBe('duel');
    ws.push('pvpOffer', { offer: 'of-1', mode: 'duel', bots: 1, untilMs: 10_000 });
    ws.push('pvpOfferGone', { offer: 'of-1', reason: PvpOfferGoneReason.DECLINED, requeued: true });
    ws.push('pvpOffer', { offer: 'of-2', mode: 'duel', bots: 1, untilMs: 10_000 });
    ws.push('pvpOfferGone', { offer: 'of-2', reason: PvpOfferGoneReason.LAPSED, requeued: true });
    expect([pvp.getState().offer, pvp.getState().queued?.mode]).toEqual([null, 'duel']);
    expect(said()).toEqual([
      'Someone declined the match. You’re back in the queue.',
      'Someone didn’t answer the match offer. You’re back in the queue.',
    ]);
    ws.push('pvpOffer', { offer: 'of-3', mode: 'duel', bots: 1, untilMs: 10_000 });
    ws.push('mapChange', { mapId: 'ring' });
    ws.push(
      'matchState',
      {
        match: 'mt-1',
        mode: 'duel',
        phase: MatchPhase.PREP,
        round: 1,
        endsInMs: 10_000,
        sides: [
          { fighters: [{ id: 'c-ayla', name: 'Ayla' }] },
          { fighters: [{ id: 'b-1', name: 'Ash', bot: true }] },
        ],
      },
      { tick: 4120 },
    );
    expect([pvp.getState().queued, pvp.getState().offer, pvp.getState().match?.id]).toEqual([
      null,
      null,
      'mt-1',
    ]);
    expect(pvp.getState().match?.sent).toEqual({ tick: 4120, endsInMs: 10_000 }); // the world's tick it came at
    ws.push('matchEnded', { result: MatchOutcome.WIN, wins: [2, 1] });
    ws.push('mapChange', { mapId: 'forest' });
    expect([pvp.getState().match, pvp.getState().outcome?.rounds]).toEqual([null, [2, 1]]);
    ws.push('pvpQueued', { mode: 'duel' });
    ws.push('pvpUnqueued', { reason: PvpUnqueueReason.LEFT });
    ws.push('pvpQueued', { mode: 'duel' });
    ws.push('pvpUnqueued', { reason: PvpUnqueueReason.LAPSED, by: ['c-ayla'] });
    ws.push('pvpQueued', { mode: 'duel' });
    ws.push('pvpUnqueued', { reason: PvpUnqueueReason.PARTY_CHANGED });
    expect(pvp.getState().queued).toBeNull();
    expect(said().slice(2)).toEqual([
      'You didn’t answer the match offer, so you left the queue.',
      'Your party changed, so it left the queue.',
    ]); // leaving is the player's own doing
    ws.push('pvpQueued', { mode: 'duel' });
    ws.push('welcome', welcome); // a join is out of any queue till the queue says again
    expect(pvp.getState().queued).toBeNull();
    pvp
      .getState()
      .applyMatchEnded(create(MatchEndedSchema, { result: MatchOutcome.LOSS }), 'c-ayla');
    leaveCharacter();
    expect(pvp.getState().outcome).toBeNull();
  });

  it('a party taken out of the queue by one member’s answer names that member to the others; the player’s own answer says its own, or nothing for a decline', async () => {
    const { ws } = await connectedSocket();
    ws.push('partyState', {
      party: {
        leader: 'c-ayla',
        members: [
          { characterId: 'c-ayla', name: 'Ayla' },
          { characterId: 'c-robin', name: 'Robin' },
          { characterId: 'c-jade', name: 'Jade' },
        ],
      },
    });
    const out = (reason: PvpUnqueueReason, by: string[]) => {
      toasts.getState().clear();
      ws.push('pvpQueued', { mode: 'team', members: ['c-ayla', 'c-robin', 'c-jade'] });
      ws.push('pvpUnqueued', { reason, by });
      expect(pvp.getState().queued).toBeNull();
      return toasts.getState().toasts.map((t) => t.text);
    };
    expect(out(PvpUnqueueReason.LAPSED, ['c-robin'])).toEqual([
      'Robin didn’t answer the match offer, so your party left the queue.',
    ]);
    expect(out(PvpUnqueueReason.LAPSED, ['c-robin', 'c-jade'])).toEqual([
      'Robin and Jade didn’t answer the match offer, so your party left the queue.',
    ]);
    expect(out(PvpUnqueueReason.LAPSED, ['c-ayla', 'c-robin'])).toEqual([
      'You didn’t answer the match offer, so you left the queue.',
    ]);
    expect(out(PvpUnqueueReason.LAPSED, ['c-gone'])).toEqual([
      'A party member didn’t answer the match offer, so your party left the queue.',
    ]);
    expect(out(PvpUnqueueReason.LAPSED, [])).toEqual([
      'The match couldn’t open, so you left the queue.',
    ]);
    expect(out(PvpUnqueueReason.DECLINED, ['c-jade'])).toEqual([
      'Jade declined the match, so your party left the queue.',
    ]);
    expect(out(PvpUnqueueReason.DECLINED, ['c-ayla'])).toEqual([]);
  });

  it('a team match’s leaver hears at once that it counts as a loss, and its end as a General line, never the card, even from inside another match; a duel’s forfeiter gets the card', async () => {
    const { ws } = await connectedSocket();
    const said = () => toasts.getState().toasts.map((t) => t.text);
    const state = (match: string, mode: string, phase: MatchPhase) => ({
      match,
      mode,
      phase,
      round: 1,
      endsInMs: 10_000,
      sides: [
        {
          fighters: [
            { id: 'c-ayla', name: 'Ayla' },
            { id: 'c-robin', name: 'Robin' },
          ],
        },
        {
          fighters: [
            { id: 'b-1', name: 'Ash', bot: true },
            { id: 'b-2', name: 'Oak', bot: true },
          ],
        },
      ],
    });
    const ended = { result: MatchOutcome.LOSS, wins: [2, 0], left: true };
    const rating = { before: 1000, after: 968, rankBefore: 'bronze', rankAfter: 'bronze' };
    ws.push('mapChange', { mapId: 'ring' });
    ws.push('matchState', state('mt-1', 'team', MatchPhase.FIGHT));
    ws.push('mapChange', { mapId: 'forest' });
    expect(said()).toEqual([
      'You left the match: it counts as a loss. Your rating updates when the match ends.',
    ]);
    ws.push('welcome', welcome); // a rejoin after is out already, and says nothing more
    expect(said()).toHaveLength(1);
    ws.push('matchState', state('mt-0', 'team', MatchPhase.PREP));
    ws.push('welcome', welcome); // a drop and rejoin mid-match leaves it too
    expect(said()).toHaveLength(2);
    toasts.getState().clear();
    ws.push('mapChange', { mapId: 'ring' });
    ws.push('matchState', state('mt-2', 'duel', MatchPhase.FIGHT));
    ws.push('matchEnded', { ...ended, mode: 'team', rating }); // mt-1's, minutes on
    expect(chat.getState().lines).toMatchObject([
      { kind: 'notice', text: 'Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32).' },
    ]);
    expect([pvp.getState().match?.id, pvp.getState().outcome]).toEqual(['mt-2', null]);
    ws.push('mapChange', { mapId: 'forest' }); // the duel's forfeit
    expect(said()).toEqual([]);
    ws.push('matchEnded', { ...ended, mode: 'duel', rating });
    expect(pvp.getState().outcome).toMatchObject({ mode: 'duel', result: MatchOutcome.LOSS });
    expect(chat.getState().lines).toHaveLength(1);
  });

  it('a Kicked frame sets the connection to kicked and returns to the login screen', async () => {
    const { ws } = await connectedSocket();
    ws.push('kicked', { reason: KickReason.LOGGED_IN_ELSEWHERE });
    ws.closeFromServer(4003);
    expect(connection.getState()).toMatchObject({
      status: 'kicked',
      kick: KickReason.LOGGED_IN_ELSEWHERE,
    });
    expect(session.getState().loggedIn).toBe(false);
    expect(character.getState().character).toBeNull();
    expect(inventory.getState().slots.use[0]).toBeNull();
    expect(party.getState().party).toBeNull();
    expect(chat.getState().lines).toEqual([]);
    expect(combatLog.getState().lines).toEqual([]);
    expect(fieldBoss.getState().boss).toBeNull();
  });

  it('a reconnect shows as reconnecting with its attempt, and a user close as disconnected', async () => {
    const { socket, ws } = await connectedSocket();
    ws.closeFromServer(4002);
    expect(connection.getState()).toMatchObject({ status: 'reconnecting', attempt: 1 });
    expect(session.getState().loggedIn).toBe(true);
    await vi.advanceTimersByTimeAsync(500); // past backoff(0)
    socket.close();
    expect(connection.getState().status).toBe('disconnected');
    expect(session.getState().loggedIn).toBe(true);
  });

  it('a session that ended logs out; the other statuses map one to one', () => {
    applyStatus({ status: 'connecting' });
    expect(connection.getState().status).toBe('connecting');
    applyStatus({ status: 'idle' });
    expect(connection.getState().status).toBe('disconnected');
    applyStatus({ status: 'closed', reason: 'no_token' });
    expect(connection.getState().status).toBe('disconnected');
    applyStatus({
      status: 'closed',
      reason: 'session_ended',
      kick: KickReason.SESSION_EXPIRED,
      code: 4003,
    });
    expect(connection.getState()).toMatchObject({
      status: 'kicked',
      kick: KickReason.SESSION_EXPIRED,
    });
    expect(session.getState().loggedIn).toBe(false);
  });

  it.each([
    {
      name: 'unavailable (a service down or busy)',
      d: { ok: false, code: ERR.UNAVAILABLE },
      text: 'The server is busy. Try again.',
    },
    {
      name: 'chat_rate_limited (a chat line too soon)',
      d: { ok: false, code: ERR.CHAT_RATE_LIMITED },
      text: 'Too fast. Wait a moment.',
    },
    {
      name: 'rate_limited (any message too soon)',
      d: { ok: false, code: ERR.RATE_LIMITED },
      text: 'Too fast. Wait a moment.',
    },
    {
      name: 'chat_no_party (a party line with no party)',
      d: { ok: false, code: ERR.CHAT_NO_PARTY },
      text: 'You’re not in a party.',
    },
    {
      name: 'another refusal with a message',
      d: { ok: false, code: ERR.TARGET_NOT_ON_MAP },
      text: 'That character isn’t on your map.',
    },
  ])('a Response with no request ID and $name shows a toast', async ({ d, text }) => {
    const { ws } = await connectedSocket();
    ws.push('response', d);
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual([text]);
  });

  it('ExpGained, LevelUp and Loot also land in the combat log', async () => {
    const { ws } = await connectedSocket();
    ws.push('expGained', { amount: 5n, exp: 15n });
    ws.push('levelUp', { character: { ...ayla, level: 2, exp: 0n } });
    ws.push('loot', { ...gel, kind: LootKind.LEFT });
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'exp', amount: 5 },
      { kind: 'level', level: 2 },
      { kind: 'loot', loot: LootKind.LEFT, items: [{ template: 'slime_gel' }] },
    ]);
  });

  it("a map's Snapshot drops the last map's party spots", async () => {
    const { ws } = await connectedSocket();
    ws.push('partySpots', { spots: [{ characterId: 'c-robin', x: 140, z: 812 }] });
    ws.push('snapshot', {});
    expect(party.getState().spots).toEqual({});
  });

  it("the player's pouches come from the Snapshot, change with each Delta, and go with the map", async () => {
    const { ws } = await connectedSocket();
    const sack = (id: number, quantity: number) => ({
      id,
      x: 1,
      items: [{ item: 1, template: 'slime_gel', quantity }],
      goneInMs: 60000n,
    });
    const ids = () => pouches.getState().pouches.map((p) => [p.id, p.items[0]?.quantity]);
    ws.push('snapshot', { pouches: [sack(4, 1), sack(2, 1)] });
    expect(ids()).toEqual([
      [2, 1],
      [4, 1],
    ]);
    ws.push('delta', { ackSeq: 1, pouches: [sack(4, 3), sack(5, 1)], pouchesGone: [2] });
    expect(ids()).toEqual([
      [4, 3],
      [5, 1],
    ]);
    ws.push('mapChange', { mapId: 'forest' });
    expect(ids()).toEqual([]);
  });

  it("Promoted changes the class, adds a combat line, says so in a toast and closes the trainer's dialog", async () => {
    const { ws } = await connectedSocket();
    ui.getState().openWindow('npc');
    ws.push('promoted', { character: { ...ayla, class: 'warrior' } });
    expect(ui.getState().windows).not.toContain('npc');
    expect(character.getState().character?.class).toBe('warrior');
    expect(combatLog.getState().lines).toMatchObject([{ kind: 'promoted', cls: 'warrior' }]);
    expect(toasts.getState().toasts).toMatchObject([
      { text: 'You became a Warrior!', kind: 'info' },
    ]);
  });

  it("Promoted gives Guard's key the class's own block, moved there if it was on another key, and saved as the player's change", async () => {
    const { ws } = await connectedSocket();
    skillBar.getState().load(['strike', 'guard', '', 'bastion', '', '', '', '']);
    ws.push('promoted', { character: { ...ayla, class: 'warrior' } });
    expect(skillBar.getState()).toMatchObject({
      slots: ['strike', 'bastion', '', '', '', '', '', ''],
      edits: 1,
    });
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    ws.push('promoted', { character: { ...ayla, class: 'thief' } });
    expect(skillBar.getState()).toMatchObject({
      slots: ['strike', '', '', '', '', '', '', ''],
      edits: 1,
    }); // no block on the bar: left as it is
  });

  it("a Welcome sets the skill bar's cooldowns and buffs to the world's, as its last checkpoint left them", async () => {
    const { ws } = await connectedSocket();
    skillBar.getState().cool('strike', { from: 0, until: 60_000 });
    skillBar.getState().buff({ skill: 'haste', points: 1, trait: '', until: 30_000 });
    vi.spyOn(performance, 'now').mockReturnValue(100_000);
    ws.push('welcome', {
      ...welcome,
      token: '', // the rejoin after a world restart, or a reload's login
      skillTimers: [
        { skill: 'iron_body', kind: SkillTimerKind.COOLDOWN, leftMs: 40_000n },
        { skill: 'iron_body', kind: SkillTimerKind.BUFF, leftMs: 20_000n, points: 2 },
      ],
    });
    // Iron Body's 60 s cooldown, 40 s of it left, drains from where it would be.
    expect(skillBar.getState()).toMatchObject({
      cooldowns: { iron_body: { from: 80_000, until: 140_000 } },
      buffs: [{ skill: 'iron_body', points: 2, trait: '', until: 120_000 }],
    });
    expect(skillBar.getState().cooldowns.strike).toBeUndefined();
    vi.advanceTimersByTime(39_999);
    expect(skillBar.getState().cooldowns.iron_body).toBeDefined();
    vi.advanceTimersByTime(1);
    expect(skillBar.getState().cooldowns.iron_body).toBeUndefined(); // it ends at its time, as a press's does
  });

  it('a Welcome times a cooldown with the trait chosen now, and a buff keeps the trait it was used with', async () => {
    const { ws } = await connectedSocket();
    vi.spyOn(performance, 'now').mockReturnValue(100_000);
    ws.push('welcome', {
      ...welcome,
      character: {
        ...ayla,
        sheet: {
          skills: { invested: { strike: 25, war_cry: 10 }, traits: { strike: 'concussion' } },
        },
      },
      skillTimers: [
        { skill: 'strike', kind: SkillTimerKind.COOLDOWN, leftMs: 3_000n },
        { skill: 'war_cry', kind: SkillTimerKind.BUFF, leftMs: 15_000n, points: 10, trait: 'fury' },
      ],
    });
    // Concussion's 7 s cooldown, 3 s of it left.
    expect(skillBar.getState()).toMatchObject({
      cooldowns: { strike: { from: 96_000, until: 103_000 } },
      buffs: [{ skill: 'war_cry', points: 10, trait: 'fury', until: 115_000 }],
    });
  });

  // pvp.md: a StatsChanged with timers, at a match's start and end and each round's reset, replaces every cooldown and buff; one without leaves them.
  it("a StatsChanged's timers replace the skill bar's after a skill was used, and one without them leaves the bar be", async () => {
    const { ws } = await connectedSocket();
    vi.spyOn(performance, 'now').mockReturnValue(100_000);
    ws.push('welcome', welcome);
    skillBar.getState().cool('strike', { from: 100_000, until: 106_000 }); // as a press of Strike sets it
    skillBar.getState().buff({ skill: 'haste', points: 1, trait: '', until: 130_000 });
    ws.push('statsChanged', { hp: 100, maxHp: 100, timers: { timers: [] } }); // the round's reset
    expect(skillBar.getState()).toMatchObject({ cooldowns: {}, buffs: [] });

    ws.push('statsChanged', {
      hp: 100,
      maxHp: 100,
      timers: { timers: [{ skill: 'iron_body', kind: SkillTimerKind.COOLDOWN, leftMs: 40_000n }] },
    });
    expect(skillBar.getState().cooldowns).toEqual({ iron_body: { from: 80_000, until: 140_000 } });
    ws.push('statsChanged', { hp: 90, maxHp: 100 }); // a hit or regen
    expect(skillBar.getState().cooldowns.iron_body).toBeDefined();
    vi.advanceTimersByTime(40_000);
    expect(skillBar.getState().cooldowns.iron_body).toBeUndefined();
  });

  // skill-traits.md section 4: a Damage naming the player, dealt or taken, keeps them in combat; a join or a map change starts out of it.
  it('a Damage naming the player either way starts the combat clock; another’s doesn’t, and a map change or a join ends it', async () => {
    const { ws } = await connectedSocket();
    const fight = (source: string, target: string) =>
      ws.push('damage', { source, target, amount: 3 });
    fight('p:c-bram', 'm:4');
    expect(combat.getState().inCombat).toBe(false);
    fight('p:c-ayla', 'm:4');
    expect(combat.getState().inCombat).toBe(true);
    vi.advanceTimersByTime(5_000);
    expect(combat.getState().inCombat).toBe(false);
    fight('m:4', 'p:c-ayla');
    expect(combat.getState().inCombat).toBe(true);
    ws.push('mapChange', { mapId: 'forest' });
    expect(combat.getState().inCombat).toBe(false);
    fight('m:4', 'p:c-ayla');
    ws.push('welcome', welcome);
    expect(combat.getState().inCombat).toBe(false);
  });

  it('a StatsChanged whose skill points reach a later stage adds an evolution line', async () => {
    const { ws } = await connectedSocket();
    const vitals = { hp: 204, maxHp: 204 };
    const invested = (points: Record<string, number>) => ({
      ...vitals,
      sheet: { skills: { unspent: 0, invested: points } },
    });
    ws.push('statsChanged', invested({ strike: 9 })); // the Welcome here has no sheet: nothing to compare
    ws.push('statsChanged', invested({ strike: 10, throw: 3 }));
    ws.push('statsChanged', vitals); // regen: no sheet
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'evolved', from: 'Strike', to: 'Heavy Strike' },
    ]);
  });

  it.each([
    {
      name: 'a 4★ item this player took, beside a gel',
      d: {
        ...gel,
        kind: LootKind.TAKEN,
        items: [...gel.items, { template: 'gold_ring', quantity: 1 }],
      },
      text: ['Took Gold Ring ★★★★'],
    },
    {
      name: "a party member's 6★ item",
      d: {
        ...gel,
        kind: LootKind.TAKEN,
        items: [{ template: 'moonstone_ring', quantity: 1 }],
        ownerId: 'c-robin',
        ownerName: 'Robin',
      },
      text: ['Robin took Moonstone Ring ★★★★★★'],
    },
    {
      name: 'a 3★ item',
      d: { ...gel, kind: LootKind.TAKEN, items: [{ template: 'silver_ring', quantity: 1 }] },
      text: [],
    },
    {
      name: 'a 4★ item left behind',
      d: { ...gel, kind: LootKind.LEFT, items: [{ template: 'gold_ring', quantity: 1 }] },
      text: [],
    },
  ])('a Loot of $name toasts: $text', async ({ d, text }) => {
    const { ws } = await connectedSocket();
    ws.push('loot', d);
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual(text);
    expect(toasts.getState().toasts.every((t) => t.kind === 'info')).toBe(true);
  });

  describe('quests', () => {
    const samples = (status: QuestStatus) => ({
      questId: 'sticky_samples',
      status,
      progress: status === QuestStatus.ACTIVE ? [0] : [],
    });
    const gels = (quantity: number, revision: bigint) => ({
      revision,
      slots: [
        {
          tab: 'etc',
          slot: 0,
          item: { itemId: 'g1', template: 'slime_gel', tab: 'etc', slot: 0, quantity },
        },
      ],
    });

    it('the log arrives in Welcome, and a quest ready then gets no line', async () => {
      FakeWebSocket.reset();
      setToken('tok-login');
      const socket = new GameSocket({ url: 'ws://game.test/ws' });
      wireSocket(socket);
      const p = socket.connect('c-ayla');
      const ws = FakeWebSocket.latest();
      ws.open();
      ws.push('welcome', {
        ...welcome,
        quests: [
          { questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [10] },
          { questId: 'first', status: QuestStatus.FINISHED, timesFinished: 2 },
        ],
      });
      await p;
      expect(quests.getState().log).toEqual({
        slime_cleanup: { status: 'active', progress: [10], timesFinished: 0 },
        first: { status: 'finished', progress: [], timesFinished: 2 },
      });
      expect(combatLog.getState().lines).toEqual([]);
    });

    it('an NpcDialog opens the dialog window', async () => {
      const { ws } = await connectedSocket();
      ws.push('npcDialog', {
        npcId: 'town_guide',
        gives: [{ questId: 'slime_cleanup', state: QuestOfferState.AVAILABLE }],
      });
      expect(quests.getState().dialog?.npcId).toBe('town_guide');
      expect(ui.getState().windows).toEqual(['npc']);
    });

    it('the dialog closes once a quest it lists is accepted, handed in or abandoned, not on progress', async () => {
      const { ws } = await connectedSocket();
      const talk = () =>
        ws.push('npcDialog', {
          npcId: 'town_guide',
          gives: [{ questId: 'sticky_samples', state: QuestOfferState.AVAILABLE }],
          takes: [{ questId: 'slime_cleanup' }],
        });
      talk();
      ws.push('questChanged', {
        change: QuestChange.PROGRESSED,
        quest: { questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [5] },
      });
      ws.push('questChanged', {
        change: QuestChange.ACCEPTED,
        quest: { questId: 'meet_the_trainers', status: QuestStatus.ACTIVE, progress: [0, 0] },
      });
      expect(ui.getState().windows).toEqual(['npc']);
      ws.push('questChanged', {
        change: QuestChange.HANDED_IN,
        quest: { questId: 'slime_cleanup', status: QuestStatus.FINISHED, timesFinished: 1 },
      });
      expect(ui.getState().windows).toEqual([]);
      talk();
      ws.push('questChanged', { change: QuestChange.ACCEPTED, quest: samples(QuestStatus.ACTIVE) });
      expect(ui.getState().windows).toEqual([]);
      talk();
      ws.push('questChanged', {
        change: QuestChange.ABANDONED,
        quest: samples(QuestStatus.UNSPECIFIED),
      });
      expect(ui.getState().windows).toEqual([]);
    });

    it('QuestChanged updates the log and adds its line; an abandoned quest leaves the log', async () => {
      const { ws } = await connectedSocket();
      ws.push('questChanged', { change: QuestChange.ACCEPTED, quest: samples(QuestStatus.ACTIVE) });
      expect(quests.getState().log.sticky_samples?.status).toBe('active');
      ws.push('questChanged', {
        change: QuestChange.ABANDONED,
        quest: samples(QuestStatus.UNSPECIFIED),
      });
      expect(quests.getState().log).toEqual({});
      expect(combatLog.getState().lines).toMatchObject([
        { kind: 'quest', change: 'accepted', quest: 'sticky_samples' },
        { kind: 'quest', change: 'abandoned', quest: 'sticky_samples' },
      ]);
    });

    it('a quest becomes ready with its last item collected, once', async () => {
      const { ws } = await connectedSocket();
      ws.push('questChanged', { change: QuestChange.ACCEPTED, quest: samples(QuestStatus.ACTIVE) });
      ws.push('inventoryChanged', gels(4, 2n));
      ws.push('inventoryChanged', gels(5, 3n));
      ws.push('inventoryChanged', gels(6, 4n));
      const ready = combatLog
        .getState()
        .lines.filter((l) => l.kind === 'quest' && l.change === 'ready');
      expect(ready).toMatchObject([{ quest: 'sticky_samples' }]);
      ws.push('questChanged', {
        change: QuestChange.HANDED_IN,
        quest: samples(QuestStatus.FINISHED),
      });
      expect(combatLog.getState().lines.at(-1)).toMatchObject({ change: 'finished' });
    });

    it('a kill that finishes a quest gets a progress line and a ready line', async () => {
      const { ws } = await connectedSocket();
      ws.push('questChanged', {
        change: QuestChange.PROGRESSED,
        quest: { questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [10] },
      });
      expect(combatLog.getState().lines).toMatchObject([
        { kind: 'quest', change: 'progressed', quest: 'slime_cleanup', progress: [10] },
        { kind: 'quest', change: 'ready', quest: 'slime_cleanup' },
      ]);
    });

    it("a rejoin's Welcome, with no token, puts back the server's state and keeps the token; a quest ready since gets its line, and the party stays until the PartyState after it", async () => {
      const { ws } = await connectedSocket();
      const token = getToken();
      ws.push('questChanged', { change: QuestChange.ACCEPTED, quest: samples(QuestStatus.ACTIVE) });
      ws.push('partyState', {
        party: { leader: 'c-ayla', members: [{ characterId: 'c-ayla', name: 'Ayla' }] },
      });
      ws.push('welcome', {
        ...welcome,
        token: '',
        quests: [{ questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [10] }],
      });
      expect(getToken()).toBe(token);
      expect(quests.getState().log).toEqual({
        slime_cleanup: { status: 'active', progress: [10], timesFinished: 0 },
      });
      expect(combatLog.getState().lines.at(-1)).toMatchObject({
        change: 'ready',
        quest: 'slime_cleanup',
      });
      expect(party.getState().party?.leader).toBe('c-ayla'); // no empty panel between the two
      ws.push('partyState', {
        party: {
          leader: 'c-ayla',
          members: [
            { characterId: 'c-ayla', name: 'Ayla' },
            { characterId: 'c-robin', name: 'Robin' },
          ],
        },
      });
      expect(party.getState().party?.members).toHaveLength(2);
    });

    it("a login's Welcome tells no quest ready: the log starts there", async () => {
      const { ws } = await connectedSocket();
      ws.push('welcome', {
        ...welcome,
        token: 'tok-again',
        quests: [{ questId: 'slime_cleanup', status: QuestStatus.ACTIVE, progress: [10] }],
      });
      expect(combatLog.getState().lines.filter((l) => l.kind === 'quest')).toEqual([]);
    });

    it('leaving the world forgets the log and the dialog', async () => {
      const { ws } = await connectedSocket();
      ws.push('questChanged', { change: QuestChange.ACCEPTED, quest: samples(QuestStatus.ACTIVE) });
      ws.push('npcDialog', { npcId: 'town_guide' });
      ws.push('kicked', { reason: KickReason.LOGGED_IN_ELSEWHERE });
      ws.closeFromServer(4003);
      expect(quests.getState().log).toEqual({});
      expect(quests.getState().dialog).toBeNull();
    });
  });

  describe('auto-assign', () => {
    const levelUp = (unspent: number) => ({
      character: { ...ayla, level: 3, exp: 0n, sheet: { points: { unspent } } },
    });
    const spends = (ws: FakeWebSocket) => ws.frames().filter((f) => f.t === 'spendPoints');

    it('with the setting on, a level-up spends every unspent point by the class’s split', async () => {
      settings.getState().setAutoAssignPoints(true);
      const { ws } = await connectedSocket();
      ws.push('levelUp', levelUp(10));
      const sent = spends(ws);
      expect(sent).toHaveLength(1);
      expect(sent[0]?.d).toMatchObject({
        expectedUnspent: 10,
        points: { str: 6, dex: 0, int: 0, agi: 0, vit: 4 },
      });
    });

    it('with the setting off, or nothing unspent, a level-up sends nothing', async () => {
      const { ws } = await connectedSocket();
      ws.push('levelUp', levelUp(5));
      settings.getState().setAutoAssignPoints(true);
      ws.push('levelUp', levelUp(0));
      expect(spends(ws)).toEqual([]);
    });

    it('points_changed is a later level-up’s to spend; another refusal says so', async () => {
      settings.getState().setAutoAssignPoints(true);
      const { ws } = await connectedSocket();
      ws.push('levelUp', levelUp(5));
      ws.push(
        'response',
        { ok: false, code: ERR.POINTS_CHANGED },
        { req: spends(ws)[0]?.req ?? '' },
      );
      ws.push('levelUp', levelUp(10));
      ws.push('response', { ok: false, code: ERR.UNAVAILABLE }, { req: spends(ws)[1]?.req ?? '' });
      await vi.runOnlyPendingTimersAsync();
      expect(toasts.getState().toasts.map((t) => t.text)).toEqual([
        'Could not auto-assign your points: The server is busy. Try again.',
      ]);
    });
  });

  it('a Response with no request ID that is ok shows nothing', async () => {
    const { ws } = await connectedSocket();
    ws.push('response', { ok: true });
    expect(toasts.getState().toasts).toEqual([]);
  });

  it('unwiring stops the flow', async () => {
    setToken('tok-login');
    const socket = new GameSocket({ url: 'ws://game.test/ws' });
    const off = wireSocket(socket);
    off();
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', welcome);
    await p;
    expect(character.getState().character).toBeNull();
    expect(connection.getState().status).toBe('disconnected');
  });
});
