// The single place net and state meet.
import { itemName, starMarks, starsOf } from '@/features/shared/items';
import { article } from '@/features/shared/words';
import { RequestError } from '@/net/socket';
import type { GameSocket, StatusEvent } from '@/net/socket';
import { className } from '@/progression/classes';
import { mapName, mobName } from '@/progression/names';
import { readyQuests } from '@/progression/quests';
import { skillById, skillCooldownTicks, stageAt } from '@/progression/skills';
import { autoAssign, hasSplit } from '@/progression/stats';
import { ERR, errorText, refusalCode } from '@/protocol/errors';
import { LootKind, SkillTimerKind } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { fieldBoss } from '@/state/field-boss';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { outdates, questPlayer, quests } from '@/state/quests';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import type { ActiveBuff, Cooldown } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

export function wireSocket(socket: GameSocket): () => void {
  // The quests ready to hand in: one that becomes ready gets a line in the Combat tab.
  let ready = new Set<string>();
  const announceReady = () => {
    const now = readyQuests(questPlayer());
    for (const id of now) {
      if (!ready.has(id)) {
        combatLog.getState().questReady(id);
      }
    }
    ready = new Set(now);
  };
  const offs = [
    socket.on('welcome', (m) => {
      character.getState().applyWelcome(m.d);
      inventory.getState().applyWelcome(m.d);
      quests.getState().applyWelcome(m.d);
      ui.getState().closeWindow('npc');
      restoreSkillTimers(m.d, performance.now()); // a Welcome is a fresh join, which counts on the world's from its last checkpoint
      if (m.d.token === '') {
        // A rejoin the server made, with no token: play went on, so a quest ready since gets its line, and the party stays until the PartyState right behind.
        announceReady();
      } else {
        party.getState().applyWelcome();
        ready = new Set(readyQuests(questPlayer()));
      }
    }),
    socket.on('inventoryChanged', (m) => {
      inventory.getState().applyInventoryChanged(m.d);
      announceReady();
    }),
    socket.on('npcDialog', (m) => {
      quests.getState().applyNpcDialog(m.d);
      ui.getState().openWindow('npc');
    }),
    socket.on('questChanged', (m) => {
      if (outdates(quests.getState().dialog, m.d)) {
        ui.getState().closeWindow('npc');
      }
      quests.getState().applyQuestChanged(m.d);
      combatLog.getState().applyQuestChanged(m.d);
      announceReady();
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
    socket.on('snapshot', (m) => {
      fieldBoss.getState().applyFieldBoss(m.d.fieldBoss);
    }),
    socket.on('fieldBoss', (m) => {
      fieldBoss.getState().applyFieldBoss(m.d);
    }),
    socket.on('fieldBossGone', (m) => {
      const gone = fieldBoss.getState().applyGone(m.d);
      if (gone && !m.d.beaten) {
        toasts.getState().push(`${mobName(gone.type)} has left.`, 'info');
      }
    }),
    socket.on('announcement', (m) => {
      announce(m.d);
    }),
    socket.on('levelUp', (m) => {
      character.getState().applyLevelUp(m.d);
      combatLog.getState().applyLevelUp(m.d);
      autoAssignPoints(socket, m.d);
      announceReady();
    }),
    socket.on('promoted', (m) => {
      character.getState().applyPromoted(m.d);
      combatLog.getState().applyPromoted(m.d);
      ui.getState().closeWindow('npc'); // its offer is spent, even if the request's answer was lost
      if (m.d.character) {
        const name = className(m.d.character.class);
        toasts.getState().push(`You became ${article(name)} ${name}!`, 'info');
      }
    }),
    socket.on('statsChanged', (m) => {
      const before = character.getState().character?.sheet?.skills?.invested;
      character.getState().applyStatsChanged(m.d);
      const after = m.d.sheet?.skills?.invested;
      if (before && after) {
        combatLog.getState().applySkillPoints(before, after);
      }
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
        toasts.getState().push(errorText(refusalCode(d.code)));
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

/** The Welcome's skill timers as the skill bar times them: each cooldown drains from its full length at the character's cooldown speed, and ends at its time, as a press's does. */
function restoreSkillTimers(d: Data<'welcome'>, now: number): void {
  const sheet = d.character?.sheet;
  const tickMs = 1000 / (d.tickRateHz || 20);
  const cooldowns: Record<string, Cooldown> = {};
  const buffs: ActiveBuff[] = [];
  for (const t of d.skillTimers) {
    const until = now + Number(t.leftMs);
    if (t.kind === SkillTimerKind.COOLDOWN) {
      const skill = skillById(t.skill);
      const stage = skill?.stages[stageAt(skill, sheet?.skills?.invested[t.skill] ?? 0)];
      const full = stage
        ? skillCooldownTicks(stage.cooldown_ms, sheet?.derived?.cooldownSpeed ?? 0, tickMs) * tickMs
        : 0;
      cooldowns[t.skill] = { from: Math.min(now, until - full), until };
    } else if (t.kind === SkillTimerKind.BUFF) {
      buffs.push({ skill: t.skill, points: t.points, until });
    }
  }
  skillBar.getState().restoreTimers(cooldowns, buffs, tickMs);
  for (const [skill, c] of Object.entries(cooldowns)) {
    setTimeout(() => {
      if (skillBar.getState().cooldowns[skill] === c) {
        skillBar.getState().cool(skill, null);
      }
    }, c.until - now);
  }
}

/** With the setting on, a level-up spends every unspent point by the class's split, as the window's button does; a class with no split spends nothing. */
function autoAssignPoints(socket: GameSocket, d: Data<'levelUp'>): void {
  const c = d.character;
  const unspent = c?.sheet?.points?.unspent ?? 0;
  if (!c || unspent === 0 || !settings.getState().autoAssignPoints || !hasSplit(c.class)) {
    return;
  }
  socket
    .request('spendPoints', { expectedUnspent: unspent, points: autoAssign(c.class, unspent) })
    .catch((err: unknown) => {
      // POINTS_CHANGED: a later level-up's request spends them; without an answer, the badge still shows any left.
      if (
        err instanceof RequestError &&
        !([ERR.POINTS_CHANGED, 'timeout', 'disconnected'] as const).some((c) => c === err.code)
      ) {
        toasts.getState().push(`Could not auto-assign your points: ${err.message}`);
      }
    });
}

/** News for everyone online shows as a toast and a [Notice] line in the General tab. */
function announce(d: Data<'announcement'>): void {
  switch (d.kind.case) {
    case 'bossAppeared': {
      const { boss, mapId } = d.kind.value;
      const text = `${mobName(boss)} has appeared in the ${mapName(mapId)}!`;
      toasts.getState().push(text, 'info');
      chat.getState().notice(text);
      return;
    }
    case undefined:
      return; // a kind from a newer server
  }
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

/** Back to the character list: what this character showed, its chat and Combat tab too, goes with it. */
export function leaveCharacter(): void {
  character.getState().clear();
  inventory.getState().clear();
  party.getState().clear();
  quests.getState().clear();
  chat.getState().clear();
  combatLog.getState().clear();
  fieldBoss.getState().clear();
}

/** Logged out or ended: the character and the session go. */
export function leaveWorld(): void {
  leaveCharacter();
  session.getState().logOut();
}
