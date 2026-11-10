// The quest log, from Welcome and QuestChanged, and the dialog of the NPC talked to last.
import type { Log, LogEntry, Player } from '@/progression/quests';
import { QuestStatus } from '@/protocol/messages';
import type { Data, NpcDialog, QuestState } from '@/protocol/messages';

import { character } from './character';
import { heldOf, inventory } from './inventory';
import { defineStore } from './store';

export interface QuestsState {
  readonly log: Log;
  /** What the NPC offered when last talked to; the window shows it while open. */
  readonly dialog: NpcDialog | null;
  applyWelcome(d: Data<'welcome'>): void;
  /** The whole log again, after the server joined the character again on its own. */
  applyQuestLog(d: Data<'questLog'>): void;
  applyQuestChanged(d: Data<'questChanged'>): void;
  applyNpcDialog(d: Data<'npcDialog'>): void;
  clear(): void;
}

/** The log's entry for a quest's state; none when it is out of the log. */
export function entryOf(q: QuestState): LogEntry | null {
  switch (q.status) {
    case QuestStatus.ACTIVE:
      return { status: 'active', progress: q.progress, timesFinished: q.timesFinished };
    case QuestStatus.FINISHED:
      return { status: 'finished', progress: [], timesFinished: q.timesFinished };
    default:
      return null;
  }
}

function logOf(states: readonly QuestState[]): Log {
  const log: Record<string, LogEntry> = {};
  for (const q of states) {
    const entry = entryOf(q);
    if (entry) {
      log[q.questId] = entry;
    }
  }
  return log;
}

export const quests = defineStore<QuestsState>((set, get) => ({
  log: {},
  dialog: null,
  applyWelcome: (d) => {
    set({ log: logOf(d.quests), dialog: null });
  },
  applyQuestLog: (d) => {
    set({ log: logOf(d.quests) });
  },
  applyQuestChanged: (d) => {
    const q = d.quest;
    if (!q) {
      return;
    }
    const log: Record<string, LogEntry> = { ...get().log };
    const entry = entryOf(q);
    if (entry) {
      log[q.questId] = entry;
    } else {
      delete log[q.questId];
    }
    set({ log });
  },
  applyNpcDialog: (d) => {
    set({ dialog: d });
  },
  clear: () => {
    set({ log: {}, dialog: null });
  },
}));

/** The player as the quest rules read it, from the stores now. */
export function questPlayer(): Player {
  const c = character.getState().character;
  const { slots } = inventory.getState();
  return {
    level: c?.level ?? 1,
    cls: c?.class ?? '',
    log: quests.getState().log,
    held: (template) => heldOf(slots, template),
  };
}
