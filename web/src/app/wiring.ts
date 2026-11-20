// The single place net and state meet.
import { membersNamed } from '@/features/pvp/mates';
import { leaverLine } from '@/features/pvp/outcome';
import { itemName, starMarks, starsOf } from '@/features/shared/items';
import { article } from '@/features/shared/words';
import { RequestError } from '@/net/socket';
import type { GameSocket, StatusEvent } from '@/net/socket';
import { className } from '@/progression/classes';
import { isMatchMap, mobName } from '@/progression/names';
import { readyQuests } from '@/progression/quests';
import { blockOf, skillById, skillCooldownTicks, specAt } from '@/progression/skills';
import { autoAssign, hasSplit } from '@/progression/stats';
import { ERR, errorText, refusalCode } from '@/protocol/errors';
import {
  LootKind,
  PvpOfferGoneReason,
  PvpUnqueueReason,
  SkillTimerKind,
  TeleportCancelReason,
} from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
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
import { pvp, runsOn } from '@/state/pvp';
import { outdates, questPlayer, quests } from '@/state/quests';
import { session } from '@/state/session';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import type { ActiveBuff, Cooldown } from '@/state/skill-bar';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

/** Why a teleport ended early, as its notice says it; a death's screen says it already. */
const CANCELLED: Partial<Record<TeleportCancelReason, string>> = {
  [TeleportCancelReason.MOVED]: 'you moved',
  [TeleportCancelReason.ACTED]: 'you attacked',
  [TeleportCancelReason.HIT]: 'you were hit',
};

/** Why the queue let the player go, as its notice says it: leaving or declining is their own doing, and says nothing; a party member's answer names them; a lapse no one caused is a match that couldn't open. */
function unqueued(d: Data<'pvpUnqueued'>): string | null {
  const me = character.getState().character?.id ?? '';
  const others = d.by.filter((id) => id !== me);
  switch (d.reason) {
    case PvpUnqueueReason.PARTY_CHANGED:
      return 'Your party changed, so it left the queue.';
    case PvpUnqueueReason.LAPSED:
      if (d.by.includes(me)) {
        return 'You didn’t answer the match offer, so you left the queue.';
      }
      return others.length > 0
        ? `${membersNamed(others)} didn’t answer the match offer, so your party left the queue.`
        : 'The match couldn’t open, so you left the queue.';
    case PvpUnqueueReason.DECLINED:
      return others.length > 0
        ? `${membersNamed(others)} declined the match, so your party left the queue.`
        : null;
    default:
      return null;
  }
}

const QUIT = 'You left the match: it counts as a loss. Your rating updates when the match ends.';

/** The player is out of the match's map: one who left it under way, in a match that runs on, hears at once what it costs them. */
function leaveMatch(): void {
  if (pvp.getState().leftMatch()) {
    toasts.getState().push(QUIT, 'info');
  }
}

/** Why a match offer the player accepted fell through. */
const FELL_THROUGH: Partial<Record<PvpOfferGoneReason, string>> = {
  [PvpOfferGoneReason.DECLINED]: 'Someone declined the match.',
  [PvpOfferGoneReason.LAPSED]: 'Someone didn’t answer the match offer.',
};

/** A promotion gives the key holding another class's block, as the Beginner's Guard, the class's own (blocking.md section 3), saved as any change to the bar is. */
function swapBlock(cls: string): void {
  const own = blockOf(cls);
  const bar = skillBar.getState();
  const key = bar.slots.findIndex((s) => s !== own?.id && skillById(s)?.effect === 'block');
  if (own && key >= 0) {
    bar.put(key, own.id);
  }
}

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
      combat.getState().clear(); // a join starts out of combat, as the world's clock does
      energy.getState().welcome(m.d.character?.energy ?? 0);
      inventory.getState().applyWelcome(m.d);
      quests.getState().applyWelcome(m.d);
      ui.getState().closeWindow('npc');
      pvp.getState().applyUnqueued(); // a join is out of any match; the queue sends its ticket again
      leaveMatch();
      restoreSkillTimers(
        m.d.skillTimers,
        m.d.character?.sheet,
        1000 / (m.d.tickRateHz || 20),
        performance.now(),
      ); // a Welcome is a fresh join, which counts on the world's from its last checkpoint
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
    socket.on('partySpots', (m) => {
      party.getState().applyPartySpots(m.d);
    }),
    socket.on('chatMessage', (m) => {
      chat.getState().applyChatMessage(m.d, mapView.getState().channel);
    }),
    socket.on('snapshot', (m) => {
      mapView.getState().inChannel(m.d.channel);
      fieldBoss.getState().applySnapshot(m.d.fieldBoss, m.d.fieldBossGone);
      energy.getState().joined();
      pouches.getState().applySnapshot(m.d.pouches);
      party.getState().clearSpots();
    }),
    socket.on('delta', (m) => {
      energy.getState().delta(m.d.ackSeq, m.d.energy);
      pouches.getState().applyDelta(m.d.pouches, m.d.pouchesGone);
    }),
    socket.on('fieldBoss', (m) => {
      fieldBoss.getState().applyFieldBoss(m.d);
      const text = `${mobName(m.d.type)} has appeared!`; // only its map hears it
      toasts.getState().push(text, 'info');
      chat.getState().notice(text);
    }),
    socket.on('fieldBossGone', (m) => {
      fieldBoss.getState().applyGone(m.d);
    }),
    socket.on('areaEntered', (m) => {
      fieldBoss.getState().applySnapshot(m.d.fieldBoss, m.d.fieldBossGone); // a region's area's own, up, beaten, or none in a town
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
        swapBlock(m.d.character.class);
        const name = className(m.d.character.class);
        toasts.getState().push(`You became ${article(name)} ${name}!`, 'info');
      }
    }),
    socket.on('statsChanged', (m) => {
      const before = character.getState().character?.sheet?.skills?.invested;
      character.getState().applyStatsChanged(m.d);
      if (m.d.timers) {
        // into a match and out, and each round's reset: the world's timers replace every one
        restoreSkillTimers(
          m.d.timers.timers,
          character.getState().character?.sheet,
          skillBar.getState().tickMs,
          performance.now(),
        );
      }
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
    socket.on('teleportCancelled', (m) => {
      teleport.getState().end();
      const why = CANCELLED[m.d.reason];
      const last = teleport.getState().last;
      if (why) {
        toasts
          .getState()
          .push(
            `${last && 'channel' in last ? 'Channel switch' : 'Teleport'} cancelled: ${why}.`,
            'info',
          );
      }
    }),
    socket.on('mapChange', (m) => {
      teleport.getState().end(); // the bar and the glow end with the old map
      combat.getState().clear();
      pouches.getState().clear(); // the new map's Snapshot brings its own
      if (!isMatchMap(m.d.mapId)) {
        leaveMatch();
      }
    }),
    socket.on('pvpQueued', (m) => {
      pvp.getState().applyQueued(m.d);
    }),
    socket.on('pvpUnqueued', (m) => {
      pvp.getState().applyUnqueued();
      const why = unqueued(m.d);
      if (why) {
        toasts.getState().push(why, 'info');
      }
    }),
    socket.on('pvpOffer', (m) => {
      pvp.getState().applyOffer(m.d);
    }),
    socket.on('pvpOfferGone', (m) => {
      pvp.getState().applyOfferGone(m.d);
      const why = FELL_THROUGH[m.d.reason];
      if (m.d.requeued && why) {
        toasts.getState().push(`${why} You’re back in the queue.`, 'info');
      }
    }),
    socket.on('matchState', (m) => {
      pvp.getState().applyMatchState(m.d, m.tick);
    }),
    socket.on('matchEnded', (m) => {
      if (m.d.left && runsOn(m.d.mode)) {
        chat.getState().notice(leaverLine(m.d)); // the card is for those it ended with; a leaver may be anywhere by now
        return;
      }
      pvp.getState().applyMatchEnded(m.d, character.getState().character?.id ?? '');
    }),
    socket.on('damage', (m) => {
      const me = `p:${character.getState().character?.id ?? ''}`;
      if (m.d.source === me || m.d.target === me) {
        combat.getState().fought();
      }
    }),
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

/** The world's skill timers as the skill bar times them, in place of every one it held: each cooldown drains from its full length, with the trait chosen now, at the character's cooldown speed, and ends at its time, as a press's does. */
function restoreSkillTimers(
  timers: Data<'welcome'>['skillTimers'],
  sheet: Data<'statsChanged'>['sheet'],
  tickMs: number,
  now: number,
): void {
  const cooldowns: Record<string, Cooldown> = {};
  const buffs: ActiveBuff[] = [];
  for (const t of timers) {
    const until = now + Number(t.leftMs);
    if (t.kind === SkillTimerKind.COOLDOWN) {
      const skill = skillById(t.skill);
      const stage =
        skill &&
        specAt(skill, sheet?.skills?.invested[t.skill] ?? 0, sheet?.skills?.traits[t.skill]);
      const full = stage
        ? skillCooldownTicks(stage.cooldown_ms, sheet?.derived?.cooldownSpeed ?? 0, tickMs) * tickMs
        : 0;
      cooldowns[t.skill] = { from: Math.min(now, until - full), until };
    } else if (t.kind === SkillTimerKind.BUFF) {
      buffs.push({ skill: t.skill, points: t.points, trait: t.trait, until });
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

const RARE_STARS = 4;

/** An item of 4★ and up taken, by this player or a party member, also shows as a toast. */
function toastRareFind(d: Data<'loot'>): void {
  if (d.kind !== LootKind.TAKEN) {
    return;
  }
  const mine = d.ownerId === character.getState().character?.id;
  for (const { template } of d.items.filter((it) => starsOf(it.template) >= RARE_STARS)) {
    const what = `${itemName(template)} ${starMarks(template)}`;
    toasts.getState().push(mine ? `Took ${what}` : `${d.ownerName} took ${what}`, 'info');
  }
}

/** Back to the character list: what this character showed, its chat and Combat tab too, goes with it. */
export function leaveCharacter(): void {
  character.getState().clear();
  energy.getState().clear();
  inventory.getState().clear();
  party.getState().clear();
  quests.getState().clear();
  chat.getState().clear();
  combatLog.getState().clear();
  pouches.getState().clear();
  fieldBoss.getState().clear();
  teleport.getState().end();
  pvp.getState().clear();
}

/** Logged out or ended: the character and the session go. */
export function leaveWorld(): void {
  leaveCharacter();
  session.getState().logOut();
}
