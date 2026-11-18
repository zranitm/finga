// The skill bar of the character in play: the skill on each of its eight keys, as the server keeps it, a chain's root or a skill in no chain, what each key offers now, and the cooldowns and buffs this client times for it.
import { defineStore } from './store';

export const SKILL_KEYS = 8; // the server's SkillBarKeys, and the input's skill keys

/** What a skill dragged from the skill window carries: its ID. */
export const SKILL_DRAG_TYPE = 'application/x-finga-skill';

/** A skill's cooldown, in performance.now() milliseconds. */
export interface Cooldown {
  readonly from: number;
  readonly until: number;
}

/** One of the player's own buffs: its skill at the points and trait it was used with, "" the default, until a time in performance.now() milliseconds. */
export interface ActiveBuff {
  readonly skill: string;
  readonly points: number;
  readonly trait: string;
  readonly until: number;
}

export interface SkillBarState {
  readonly slots: readonly string[];
  /** What each key offers now: its slot's skill, or a follow-up its chain gives now (skill-chains.md section 5). */
  readonly live: readonly string[];
  /** Counts the player's own changes, which are saved; a load is not one. */
  readonly edits: number;
  readonly cooldowns: Readonly<Record<string, Cooldown>>;
  readonly buffs: readonly ActiveBuff[];
  /** The world's tick, which every cooldown is rounded up to whole ones of. */
  readonly tickMs: number;
  load(slots: readonly string[]): void;
  /** What each key offers now, as the world's frame loop judges it; the same again changes nothing. */
  showLive(live: readonly string[]): void;
  /** Puts the skill on the key; on another key already, it leaves that one. */
  put(key: number, skill: string): void;
  swap(a: number, b: number): void;
  clear(key: number): void;
  cool(skill: string, cooldown: Cooldown | null): void;
  /** A buff used again starts over. */
  buff(b: ActiveBuff): void;
  /** Ends the buffs whose time is up by now, or every buff. */
  endBuffs(now?: number): void;
  /** A join brings the world's cooldowns and buffs, as its last checkpoint left them, and the client's follow; a map change carries both. */
  restoreTimers(
    cooldowns: Readonly<Record<string, Cooldown>>,
    buffs: readonly ActiveBuff[],
    tickMs: number,
  ): void;
}

function barOf(slots: readonly string[]): string[] {
  return Array.from({ length: SKILL_KEYS }, (_, k) => slots[k] ?? '');
}

export const skillBar = defineStore<SkillBarState>((set, get) => ({
  slots: barOf([]),
  live: barOf([]),
  edits: 0,
  cooldowns: {},
  buffs: [],
  tickMs: 50,
  load: (slots) => {
    set({ slots: barOf(slots), live: barOf(slots), cooldowns: {}, buffs: [] });
  },
  showLive: (live) => {
    const bar = barOf(live);
    if (bar.some((s, k) => s !== get().live[k])) {
      set({ live: bar });
    }
  },
  put: (key, skill) => {
    const slots = get().slots.map((s) => (s === skill ? '' : s));
    slots[key] = skill;
    set({ slots, live: slots, edits: get().edits + 1 });
  },
  swap: (a, b) => {
    const slots = [...get().slots];
    [slots[a], slots[b]] = [slots[b] ?? '', slots[a] ?? ''];
    set({ slots, live: slots, edits: get().edits + 1 });
  },
  clear: (key) => {
    const slots = [...get().slots];
    slots[key] = '';
    set({ slots, live: slots, edits: get().edits + 1 });
  },
  cool: (skill, cooldown) => {
    const rest = Object.fromEntries(Object.entries(get().cooldowns).filter(([k]) => k !== skill));
    set({ cooldowns: cooldown ? { ...rest, [skill]: cooldown } : rest });
  },
  buff: (b) => {
    set({ buffs: [...get().buffs.filter((x) => x.skill !== b.skill), b] });
  },
  endBuffs: (now) => {
    const buffs = now === undefined ? [] : get().buffs.filter((b) => b.until > now);
    if (buffs.length !== get().buffs.length) {
      set({ buffs });
    }
  },
  restoreTimers: (cooldowns, buffs, tickMs) => {
    set({ cooldowns, buffs, tickMs });
  },
}));
