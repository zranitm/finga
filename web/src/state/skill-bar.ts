// The skill bar of the character in play: the skill on each of keys 1 to 6, as the server keeps it, and the cooldowns and buffs this client times for it.
import { defineStore } from './store';

export const SKILL_KEYS = 6;

/** What a skill dragged from the skill window carries: its ID. */
export const SKILL_DRAG_TYPE = 'application/x-finga-skill';

/** A skill's cooldown, in performance.now() milliseconds. */
export interface Cooldown {
  readonly from: number;
  readonly until: number;
}

/** One of the player's own buffs: its skill at the points it was used with, until a time in performance.now() milliseconds. */
export interface ActiveBuff {
  readonly skill: string;
  readonly points: number;
  readonly until: number;
}

export interface SkillBarState {
  readonly slots: readonly string[];
  /** Counts the player's own changes, which are saved; a load is not one. */
  readonly edits: number;
  readonly cooldowns: Readonly<Record<string, Cooldown>>;
  readonly buffs: readonly ActiveBuff[];
  load(slots: readonly string[]): void;
  /** Puts the skill on the key; on another key already, it leaves that one. */
  put(key: number, skill: string): void;
  swap(a: number, b: number): void;
  clear(key: number): void;
  cool(skill: string, cooldown: Cooldown | null): void;
  /** A buff used again starts over. */
  buff(b: ActiveBuff): void;
  /** Ends the buffs whose time is up by now, or every buff. */
  endBuffs(now?: number): void;
  /** A join starts the world's cooldowns and buffs over, so the client's follow; a map change carries both. */
  clearTimers(): void;
}

function sixOf(slots: readonly string[]): string[] {
  return Array.from({ length: SKILL_KEYS }, (_, k) => slots[k] ?? '');
}

export const skillBar = defineStore<SkillBarState>((set, get) => ({
  slots: sixOf([]),
  edits: 0,
  cooldowns: {},
  buffs: [],
  load: (slots) => {
    set({ slots: sixOf(slots), cooldowns: {}, buffs: [] });
  },
  put: (key, skill) => {
    const slots = get().slots.map((s) => (s === skill ? '' : s));
    slots[key] = skill;
    set({ slots, edits: get().edits + 1 });
  },
  swap: (a, b) => {
    const slots = [...get().slots];
    [slots[a], slots[b]] = [slots[b] ?? '', slots[a] ?? ''];
    set({ slots, edits: get().edits + 1 });
  },
  clear: (key) => {
    const slots = [...get().slots];
    slots[key] = '';
    set({ slots, edits: get().edits + 1 });
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
  clearTimers: () => {
    set({ cooldowns: {}, buffs: [] });
  },
}));
