// Mirrors server/modules/world/internal/application/quests for the dialog, the log and the markers; the server's answer wins.
import { items } from '@/gamedata/items';
import { movement } from '@/gamedata/movement';
import { npcs } from '@/gamedata/npcs';
import { quests } from '@/gamedata/quests';

import { mobName } from './names';

export type ObjectiveKind = 'kill' | 'collect' | 'talk' | 'level';

export interface Objective {
  readonly kind: ObjectiveKind;
  readonly target: string | null; // the mob, item or NPC; none for a level
  readonly count: number; // for a level, the level
}

export interface Quest {
  readonly id: string;
  readonly name: string;
  readonly giver: string;
  readonly turn_in: string;
  readonly level: number;
  readonly class: string | null;
  readonly quests: readonly string[];
  readonly objectives: readonly Objective[];
  readonly rewards: {
    readonly exp: number;
    readonly items: readonly { readonly item: string; readonly quantity: number }[];
  };
  readonly repeatable: boolean;
}

export interface Npc {
  readonly id: string;
  readonly name: string;
  readonly look: string;
  readonly map: string;
  readonly x: number;
  readonly y: number;
  readonly gives: readonly string[];
  readonly takes: readonly string[];
  readonly lines: readonly string[]; // what it says when talked to
}

const allQuests: readonly Quest[] = quests;
const allNpcs: readonly Npc[] = npcs.npcs;

export const TALK_REACH = npcs.talk_reach;

export function questById(id: string): Quest | undefined {
  return allQuests.find((q) => q.id === id);
}

export function npcById(id: string): Npc | undefined {
  return allNpcs.find((n) => n.id === id);
}

export function npcsOn(mapId: string): readonly Npc[] {
  return allNpcs.filter((n) => n.map === mapId);
}

/** One quest of the log; a quest out of it has none. */
export interface LogEntry {
  readonly status: 'active' | 'finished';
  /** One count per objective while active; a collect objective's stays 0. */
  readonly progress: readonly number[];
  readonly timesFinished: number;
}

export type Log = Readonly<Record<string, LogEntry>>;

/** What the rules read of the character: its level, class, log, and how many of an item it holds. */
export interface Player {
  readonly level: number;
  readonly cls: string;
  readonly log: Log;
  readonly held: (template: string) => number;
}

export type OfferState = 'available' | 'active' | 'finished' | 'unmet';

/** Where a quest an NPC gives stands for the player, as the server's NpcDialog says it. */
export function offerState(q: Quest, p: Player): OfferState {
  const entry = p.log[q.id];
  if (entry?.status === 'active') {
    return 'active';
  }
  if (entry && !q.repeatable) {
    return 'finished';
  }
  return meets(q, p) ? 'available' : 'unmet';
}

function meets(q: Quest, p: Player): boolean {
  return (
    p.level >= q.level &&
    (q.class === null || q.class === p.cls) &&
    q.quests.every((id) => (p.log[id]?.timesFinished ?? 0) > 0)
  );
}

/** The kth objective's count so far: the log's for kills and talks, the inventory's for items, the level for a level. */
export function objectiveCount(q: Quest, entry: LogEntry, k: number, p: Player): number {
  const o = q.objectives[k]!;
  switch (o.kind) {
    case 'collect':
      return p.held(o.target ?? '');
    case 'level':
      return p.level;
    default:
      return entry.progress[k] ?? 0;
  }
}

/** What an objective asks, as the log and the combat channel name it. */
export function objectiveLabel(o: Objective): string {
  const target = o.target ?? '';
  switch (o.kind) {
    case 'kill':
      return mobName(target);
    case 'collect':
      return items.find((i) => i.id === target)?.name ?? target;
    case 'talk':
      return `Talk to ${npcById(target)?.name ?? target}`;
    case 'level':
      return `Reach level ${o.count}`;
  }
}

/** A quest's name, or its ID for one this client does not know. */
export function questName(id: string): string {
  return questById(id)?.name ?? id;
}

/** An active quest with every objective done, its items held too. */
export function isReady(q: Quest, p: Player): boolean {
  const entry = p.log[q.id];
  return (
    entry?.status === 'active' &&
    q.objectives.every((o, k) => objectiveCount(q, entry, k, p) >= o.count)
  );
}

/** The active quests that are ready to hand in, by ID. */
export function readyQuests(p: Player): string[] {
  return Object.keys(p.log).filter((id) => {
    const q = questById(id);
    return q !== undefined && isReady(q, p);
  });
}

/** What floats over an NPC: a quest to hand in, one to take, or one under way. */
export type Marker = 'ready' | 'available' | 'active';

export function markerFor(npc: Npc, p: Player): Marker | null {
  const takes = npc.takes.flatMap((id) => questById(id) ?? []);
  if (takes.some((q) => isReady(q, p))) {
    return 'ready';
  }
  const gives = npc.gives.flatMap((id) => questById(id) ?? []);
  if (gives.some((q) => offerState(q, p) === 'available')) {
    return 'available';
  }
  return takes.some((q) => p.log[q.id]?.status === 'active') ? 'active' : null;
}

/** As the server measures it: from the middle of a character with feet at x, y to the NPC's feet. */
function distance(npc: Npc, x: number, y: number): number {
  return Math.hypot(npc.x - x, npc.y - (y - movement.height / 2));
}

export function inReach(npc: Npc, x: number, y: number): boolean {
  return distance(npc, x, y) <= TALK_REACH;
}

/** The nearest NPC on the map within reach of feet at x, y. */
export function nearestInReach(mapId: string, x: number, y: number): Npc | undefined {
  let best: Npc | undefined;
  for (const n of npcsOn(mapId)) {
    if (inReach(n, x, y) && (!best || distance(n, x, y) < distance(best, x, y))) {
      best = n;
    }
  }
  return best;
}
