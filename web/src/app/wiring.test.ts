import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, getToken, setToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import {
  ChatChannel,
  KickReason,
  LootKind,
  QuestChange,
  QuestOfferState,
  QuestStatus,
  SkillTimerKind,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { quests } from '@/state/quests';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { applyStatus, wireSocket } from './wiring';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  maxHp: 204,
  mp: 28,
  maxMp: 28,
};
const potion = { itemId: 'i1', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };
const gel = {
  mapId: 'meadow',
  dropId: 7,
  ownerId: 'c-ayla',
  ownerName: 'Ayla',
  template: 'slime_gel',
  quantity: 1,
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
      name: 'PartyInvited',
      push: (ws: FakeWebSocket) =>
        ws.push('partyInvited', { inviteId: 'inv-1', from: 'c-robin', fromName: 'Robin' }),
      check: () => expect(party.getState().invites[0]?.fromName).toBe('Robin'),
    },
    {
      name: 'ChatMessage',
      push: (ws: FakeWebSocket) =>
        ws.push('chatMessage', {
          channel: ChatChannel.MAP,
          sender: 'c-robin',
          senderName: 'Robin',
          text: 'hi',
        }),
      check: () => expect(chat.getState().lines[0]?.text).toBe('hi'),
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
          mp: 28,
          maxMp: 28,
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
    ws.push('loot', { ...gel, kind: LootKind.WAITING });
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'exp', amount: 5 },
      { kind: 'level', level: 2 },
      { kind: 'loot', loot: LootKind.WAITING, template: 'slime_gel' },
    ]);
    expect(combatLog.getState().waiting).toMatchObject([{ key: 'meadow/7' }]);
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

  it("a Welcome sets the skill bar's cooldowns and buffs to the world's, as its last checkpoint left them", async () => {
    const { ws } = await connectedSocket();
    skillBar.getState().cool('strike', { from: 0, until: 60_000 });
    skillBar.getState().buff({ skill: 'haste', points: 1, until: 30_000 });
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
      buffs: [{ skill: 'iron_body', points: 2, until: 120_000 }],
    });
    expect(skillBar.getState().cooldowns.strike).toBeUndefined();
    vi.advanceTimersByTime(39_999);
    expect(skillBar.getState().cooldowns.iron_body).toBeDefined();
    vi.advanceTimersByTime(1);
    expect(skillBar.getState().cooldowns.iron_body).toBeUndefined(); // it ends at its time, as a press's does
  });

  it('a StatsChanged whose skill points reach a later stage adds an evolution line', async () => {
    const { ws } = await connectedSocket();
    const vitals = { hp: 204, maxHp: 204, mp: 28, maxMp: 28 };
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
      name: 'a 4★ item this player collected',
      d: { ...gel, kind: LootKind.COLLECTED, template: 'gold_ring' },
      text: ['You got Gold Ring ★★★★'],
    },
    {
      name: "a party member's 6★ item",
      d: {
        ...gel,
        kind: LootKind.COLLECTED,
        template: 'moonstone_ring',
        ownerId: 'c-robin',
        ownerName: 'Robin',
      },
      text: ['Robin got Moonstone Ring ★★★★★★'],
    },
    {
      name: 'a 3★ item',
      d: { ...gel, kind: LootKind.COLLECTED, template: 'silver_ring' },
      text: [],
    },
    {
      name: 'a 4★ item waiting for room',
      d: { ...gel, kind: LootKind.WAITING, template: 'gold_ring' },
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
